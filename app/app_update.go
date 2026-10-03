package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/clinic"
	"github.com/ohcnetwork/care_desktop/app/internal/health"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

var checking atomic.Bool

type CareUpdate struct {
	Backend  string `json:"backend"`
	Frontend string `json:"frontend"`
}

type CareCheck struct {
	Running bool   `json:"running"`
	Found   bool   `json:"found"`
	Error   string `json:"error,omitempty"`
}

func (a *App) CareUpdateStatus() (status clinic.ChannelStatus, err error) {
	defer a.logError(&err)
	if err := a.requireSetup(); err != nil {
		return clinic.ChannelStatus{}, err
	}
	return a.engine().ChannelStatus(), nil
}

func (a *App) CheckCareUpdate() (err error) {
	defer a.logError(&err)
	if err := a.requireSetup(); err != nil {
		return err
	}
	if !checking.CompareAndSwap(false, true) {
		return nil
	}
	go func() {
		defer checking.Store(false)
		a.checkCareUpdate()
	}()
	return nil
}

func (a *App) DismissCareUpdate() (err error) {
	defer a.logError(&err)
	if err := a.requireSetup(); err != nil {
		return err
	}
	return a.engine().DeclineUpdate()
}

func (a *App) checkCareUpdate() {
	if !a.updatesAllowed() {
		return
	}
	a.emit("care-check", CareCheck{Running: true})
	update, err := a.engineForUpdate().CheckForUpdate()
	if err != nil {
		a.logln("update check: " + err.Error())
		a.emit("care-check", CareCheck{Error: "CARE updates couldn't be checked. Try again, or open the log file for support."})
		return
	}
	a.emit("care-check", CareCheck{Found: update.Any()})
	if !update.Any() || !a.updatesAllowed() {
		return
	}
	a.emit("care-update", CareUpdate{Backend: update.Backend, Frontend: update.Frontend})
}

func (a *App) isClosing() bool {
	a.jobMu.RLock()
	defer a.jobMu.RUnlock()
	return a.closing
}

func (a *App) updatesAllowed() bool {
	cfg := a.loadConfig()
	return !a.isClosing() && cfg.Role == roleServer && cfg.SetupDone && !cfg.Removing
}

func (a *App) updatesAbandoned() bool {
	cfg := a.loadConfig()
	return a.isClosing() || cfg.Role == roleClient || cfg.Removing
}

const (
	updateInterval = time.Hour
	updateWarmup   = 30 * time.Second
)

func (a *App) watchForCareUpdates() {
	for {
		if a.updatesAbandoned() {
			return
		}
		wait := updateWarmup
		if a.updatesAllowed() && health.Ping().Active {
			if checking.CompareAndSwap(false, true) {
				a.checkCareUpdate()
				checking.Store(false)
			}
			wait = updateInterval
		}
		select {
		case <-a.ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

const (
	releasesAPI = "https://api.github.com/repos/ohcnetwork/care_desktop/releases/latest"

	maxDownloadBytes = 512 << 20

	downloadTimeout = 30 * time.Minute

	updateDownloadAttempts = 2
)

type AppUpdate struct {
	Current   string `json:"current"`
	Version   string `json:"version"`
	Available bool   `json:"available"`
	NotesURL  string `json:"notes_url"`
	Asset     string `json:"asset"`
	Size      int64  `json:"size"`
}

type AppUpdateProgress struct {
	Phase string `json:"phase"`
	Done  int64  `json:"done"`
	Total int64  `json:"total"`
}

const (
	phaseDownloading = "downloading"
	phaseVerifying   = "verifying"
	phaseInstalling  = "installing"
	phaseRestarting  = "restarting"
)

func (a *App) updateProgress(phase string, done, total int64) {
	a.emit("app-update-progress", AppUpdateProgress{Phase: phase, Done: done, Total: total})
}

type ghAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

type ghRelease struct {
	Tag     string    `json:"tag_name"`
	HTMLURL string    `json:"html_url"`
	Assets  []ghAsset `json:"assets"`
}

func (a *App) CheckAppUpdate() (update AppUpdate, err error) {
	defer a.logError(&err)
	out := AppUpdate{Current: a.pins.AppVersion}
	rel, err := latestRelease()
	if err != nil {
		return out, err
	}
	out.Version = strings.TrimPrefix(strings.TrimSpace(rel.Tag), "v")
	out.NotesURL = rel.HTMLURL
	asset, ok := platformAsset(rel)
	if !ok {
		return out, fmt.Errorf("release %s has no installer for this computer", out.Version)
	}
	out.Asset, out.Size = asset.Name, asset.Size
	out.Available = newerVersion(out.Current, out.Version)
	return out, nil
}

func (a *App) InstallAppUpdate() error {
	return a.runJob(func() error {
		rel, err := latestRelease()
		if err != nil {
			return err
		}
		version := strings.TrimPrefix(strings.TrimSpace(rel.Tag), "v")
		if !newerVersion(a.pins.AppVersion, version) {
			return errors.New("this is already the newest published version of CARE Desktop")
		}
		if developmentUpdateBuild || strings.HasSuffix(a.pins.AppVersion, "-dev") {
			return errors.New("update location unavailable: development builds cannot replace themselves; install a released copy of CARE Desktop first")
		}
		if _, err := updateTarget(); err != nil {
			return err
		}
		asset, ok := platformAsset(rel)
		if !ok {
			return fmt.Errorf("release %s has no installer for this computer", version)
		}
		sums, ok := findAsset(rel, func(name string) bool { return name == "SHA256SUMS" })
		if !ok {
			return fmt.Errorf("release %s publishes no checksums, so its installer cannot be verified", version)
		}
		want, err := expectedSum(sums.URL, asset.Name)
		if err != nil {
			return err
		}
		a.updateProgress(phaseDownloading, 0, asset.Size)
		progress := func(done int64) { a.updateProgress(phaseDownloading, done, asset.Size) }
		path, err := downloadVerified(asset, want, version, a.logln, progress)
		if err != nil {
			return err
		}
		a.logln("Download verified.")
		return a.handoffAppUpdate(path, version, want)
	}, false, "app-update")
}

func (a *App) quitAfterJob() {
	go func() {
		a.jobMu.Lock()
		a.jobMu.Unlock() //nolint:staticcheck // SA2001: waiting for the lock is the point
		wruntime.Quit(a.ctx)
	}()
}

func latestRelease() (ghRelease, error) {
	var rel ghRelease
	err := fetch(releasesAPI, 20*time.Second, func(body io.Reader) error {
		if err := json.NewDecoder(io.LimitReader(body, 4<<20)).Decode(&rel); err != nil {
			return fmt.Errorf("couldn't read the release listing: %w", err)
		}
		return nil
	})
	if err != nil {
		return rel, err
	}
	if strings.TrimSpace(rel.Tag) == "" {
		return rel, errors.New("no published CARE Desktop release was found")
	}
	return rel, nil
}

func fetch(url string, timeout time.Duration, fn func(io.Reader) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "care-desktop")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach GitHub to check for updates: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub answered HTTP %d for %s", resp.StatusCode, url)
	}
	return fn(resp.Body)
}

func platformAsset(rel ghRelease) (ghAsset, bool) {
	switch runtime.GOOS {
	case "windows":
		return findAsset(rel, func(name string) bool { return strings.HasSuffix(name, "-setup.exe") })
	case "darwin":
		return findAsset(rel, func(name string) bool { return strings.HasSuffix(name, ".dmg") })
	}
	return ghAsset{}, false
}

func findAsset(rel ghRelease, match func(string) bool) (ghAsset, bool) {
	for _, asset := range rel.Assets {
		if match(asset.Name) && asset.URL != "" {
			return asset, true
		}
	}
	return ghAsset{}, false
}

type progressWriter struct {
	done     int64
	last     time.Time
	progress func(int64)
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.done += int64(len(p))
	if w.progress != nil && time.Since(w.last) >= 200*time.Millisecond {
		w.last = time.Now()
		w.progress(w.done)
	}
	return len(p), nil
}

func download(url, path string, progress func(int64)) (string, error) {
	var digest string
	err := fetch(url, downloadTimeout, func(body io.Reader) error {
		file, err := os.Create(path)
		if err != nil {
			return err
		}
		sum := sha256.New()
		counter := &progressWriter{progress: progress}
		written, err := io.Copy(io.MultiWriter(file, sum, counter), io.LimitReader(body, maxDownloadBytes+1))
		if progress != nil {
			progress(written)
		}
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return fmt.Errorf("the download did not finish: %w", err)
		}
		if written > maxDownloadBytes {
			return errors.New("the download is larger than any CARE Desktop installer should be")
		}
		digest = hex.EncodeToString(sum.Sum(nil))
		return nil
	})
	return digest, err
}

func downloadVerified(asset ghAsset, want, version string, log func(string), progress func(int64)) (string, error) {
	if filepath.Base(asset.Name) != asset.Name || strings.ContainsAny(asset.Name, `/\`) || asset.Name == "." || asset.Name == "" {
		return "", errors.New("the release has an unsafe installer filename")
	}
	if digest, err := hex.DecodeString(want); err != nil || len(digest) != sha256.Size {
		return "", errors.New("the release has an invalid SHA-256 checksum")
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	root := filepath.Join(cache, "CARE Desktop", "updates")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(root, "update-")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, asset.Name)
	var problem string
	for attempt := 1; attempt <= updateDownloadAttempts; attempt++ {
		log("Downloading CARE Desktop " + version + " (" + asset.Name + ")...")
		sum, err := download(asset.URL, path, progress)
		switch {
		case err != nil:
			problem = err.Error()
		case sum != want:
			problem = "its SHA-256 is " + sum + " but release " + version + " publishes " + want
		default:
			return path, nil
		}
		_ = os.Remove(path)
		if attempt < updateDownloadAttempts {
			log("The download was incomplete or damaged (" + problem + "). Trying once more...")
		}
	}
	_ = os.RemoveAll(dir)
	return "", fmt.Errorf("the CARE Desktop %s update didn't download properly and was deleted without being installed (%s). "+
		"Check this computer's internet connection and choose Update again", version, problem)
}

func expectedSum(url, name string) (string, error) {
	var want string
	err := fetch(url, 2*time.Minute, func(body io.Reader) error {
		data, err := io.ReadAll(io.LimitReader(body, 1<<20))
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(data), "\n") {
			sum, file, ok := strings.Cut(strings.TrimSpace(line), "  ")
			if ok && file == name {
				want = strings.ToLower(sum)
				return nil
			}
		}
		return fmt.Errorf("the published checksums do not cover %s", name)
	})
	return want, err
}

func newerVersion(current, candidate string) bool {
	cur, curDev := strings.CutSuffix(current, "-dev")
	cand := strings.TrimSuffix(candidate, "-dev")
	a, b := versionParts(cur), versionParts(cand)
	for i := range a {
		if a[i] != b[i] {
			return b[i] > a[i]
		}
	}
	return curDev
}

func versionParts(v string) [3]int {
	var out [3]int
	for i, part := range strings.SplitN(v, ".", 3) {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return [3]int{}
		}
		out[i] = n
	}
	return out
}
