// Package lines reads the CARE release-line manifest published by CI.
//
// A release line is a maintained CARE version (for example "2025.1") whose
// backend and frontend branches only receive bug fixes. CI builds every new
// commit of each line into images and publishes lines.json describing the
// newest images of every line. Clinics stay on their chosen line and receive
// its fixes; moving to a newer line is an explicit, one-way upgrade.
package lines

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/atomicfile"
)

const (
	Schema    = 1
	CacheFile = "lines.json"

	// OverrideEnv points the app at another manifest (an https URL or a
	// local file path) for development and testing.
	OverrideEnv = "CARE_CLINIC_LINES_URL"

	maxManifestBytes = 1 << 20
	fetchTimeout     = 20 * time.Second
)

// Image is one published CARE component build.
type Image struct {
	Commit  string `json:"commit"`
	Image   string `json:"image"`
	Builder string `json:"builder"`
	// RuntimeKeys lists the frontend settings the image applies at container
	// start (frontend only).
	RuntimeKeys []string `json:"runtime_keys,omitempty"`
	// BuildEnv holds the build-time frontend settings the image was compiled
	// with (frontend only). Other values for these keys need a local rebuild.
	BuildEnv map[string]string `json:"build_env,omitempty"`
}

type Line struct {
	ID             string `json:"id"`
	Title          string `json:"title,omitempty"`
	Backend        Image  `json:"backend"`
	Frontend       Image  `json:"frontend"`
	SupportedUntil string `json:"supported_until,omitempty"`
	MinAppVersion  string `json:"min_app_version,omitempty"`
	MaxAppVersion  string `json:"max_app_version,omitempty"`
}

// Manifest lists maintained lines from oldest to newest.
type Manifest struct {
	Schema      int    `json:"schema"`
	Generated   string `json:"generated,omitempty"`
	Recommended string `json:"recommended,omitempty"`
	Lines       []Line `json:"lines"`
}

var (
	idPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	// Images are referenced by digest so a clinic always runs exactly what CI
	// verified, and an image already on disk never needs the network again.
	imagePattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9._/:-]*@sha256:[0-9a-f]{64}$`)
	keyPattern     = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	datePattern    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

func Parse(data []byte) (*Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("release-line manifest is malformed: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *Manifest) Validate() error {
	if m.Schema != Schema {
		return fmt.Errorf("release-line manifest schema %d is not supported (this app reads schema %d); update CARE Clinic", m.Schema, Schema)
	}
	if len(m.Lines) == 0 {
		return errors.New("release-line manifest lists no lines")
	}
	seen := map[string]bool{}
	for _, l := range m.Lines {
		if !idPattern.MatchString(l.ID) {
			return fmt.Errorf("release line id %q is invalid", l.ID)
		}
		if seen[l.ID] {
			return fmt.Errorf("release line %q is listed twice", l.ID)
		}
		seen[l.ID] = true
		if err := l.Backend.validate(); err != nil {
			return fmt.Errorf("release line %q backend: %w", l.ID, err)
		}
		if err := l.Frontend.validate(); err != nil {
			return fmt.Errorf("release line %q frontend: %w", l.ID, err)
		}
		if len(l.Frontend.RuntimeKeys) == 0 {
			return fmt.Errorf("release line %q frontend lists no runtime settings", l.ID)
		}
		if v := l.MinAppVersion; v != "" && !versionPattern.MatchString(v) {
			return fmt.Errorf("release line %q min_app_version must be X.Y.Z", l.ID)
		}
		if v := l.MaxAppVersion; v != "" && !versionPattern.MatchString(v) {
			return fmt.Errorf("release line %q max_app_version must be X.Y.Z", l.ID)
		}
		if l.SupportedUntil != "" {
			if !datePattern.MatchString(l.SupportedUntil) {
				return fmt.Errorf("release line %q supported_until must be YYYY-MM-DD", l.ID)
			}
			if _, err := time.Parse(time.DateOnly, l.SupportedUntil); err != nil {
				return fmt.Errorf("release line %q supported_until: %w", l.ID, err)
			}
		}
	}
	if m.Recommended != "" && !seen[m.Recommended] {
		return fmt.Errorf("recommended release line %q is not listed", m.Recommended)
	}
	return nil
}

func (img Image) validate() error {
	if !commitPattern.MatchString(img.Commit) {
		return errors.New("commit must be a full lowercase SHA")
	}
	if !imagePattern.MatchString(img.Image) {
		return fmt.Errorf("image %q must be pinned by digest", img.Image)
	}
	if !imagePattern.MatchString(img.Builder) {
		return fmt.Errorf("builder image %q must be pinned by digest", img.Builder)
	}
	for _, k := range img.RuntimeKeys {
		if !keyPattern.MatchString(k) {
			return fmt.Errorf("runtime setting %q is not a valid name", k)
		}
	}
	for k := range img.BuildEnv {
		if !keyPattern.MatchString(k) {
			return fmt.Errorf("build setting %q is not a valid name", k)
		}
		if slices.Contains(img.RuntimeKeys, k) {
			return fmt.Errorf("setting %q is both a runtime and a build setting", k)
		}
	}
	return nil
}

func (m *Manifest) Line(id string) (Line, bool) {
	for _, l := range m.Lines {
		if l.ID == id {
			return l, true
		}
	}
	return Line{}, false
}

// Newer returns the lines published after id, oldest first.
func (m *Manifest) Newer(id string) []Line {
	for i, l := range m.Lines {
		if l.ID == id {
			return slices.Clone(m.Lines[i+1:])
		}
	}
	return nil
}

// IsNewer reports whether candidate comes after current in the manifest.
func (m *Manifest) IsNewer(current, candidate string) bool {
	return slices.ContainsFunc(m.Newer(current), func(l Line) bool { return l.ID == candidate })
}

// SupportsApp reports whether this app version may run the line.
func (l Line) SupportsApp(version string) error {
	if l.MinAppVersion != "" && CompareVersions(version, l.MinAppVersion) < 0 {
		return fmt.Errorf("CARE line %s needs CARE Clinic %s or newer (this is %s); update CARE Clinic first", l.ID, l.MinAppVersion, version)
	}
	if l.MaxAppVersion != "" && CompareVersions(version, l.MaxAppVersion) > 0 {
		return fmt.Errorf("CARE line %s supports CARE Clinic up to %s (this is %s); upgrade to a newer CARE line", l.ID, l.MaxAppVersion, version)
	}
	return nil
}

// Expired reports whether the line is past its support window.
func (l Line) Expired(now time.Time) bool {
	if l.SupportedUntil == "" {
		return false
	}
	end, err := time.Parse(time.DateOnly, l.SupportedUntil)
	return err == nil && now.After(end.Add(24*time.Hour))
}

// CompareVersions compares X.Y.Z versions (a -dev suffix is ignored);
// unparsable versions compare as 0.0.0.
func CompareVersions(a, b string) int {
	pa, pb := versionParts(a), versionParts(b)
	for i := range pa {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func versionParts(v string) [3]int {
	var out [3]int
	for i, part := range strings.SplitN(strings.TrimSuffix(strings.TrimSpace(v), "-dev"), ".", 3) {
		n, err := strconv.Atoi(part)
		if err != nil {
			return [3]int{}
		}
		out[i] = n
	}
	return out
}

// Source returns the manifest location, honouring the development override.
func Source(pinned string) string {
	if v := strings.TrimSpace(os.Getenv(OverrideEnv)); v != "" {
		return v
	}
	return pinned
}

// Fetch downloads and validates the manifest, then caches it in dir so the
// clinic keeps working offline with the images it already has.
func Fetch(dir, source string) (*Manifest, error) {
	data, err := download(source)
	if err != nil {
		return nil, err
	}
	m, err := Parse(data)
	if err != nil {
		return nil, err
	}
	if dir != "" {
		if err := atomicfile.Write(filepath.Join(dir, CacheFile), data, 0o644); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// Cached returns the last manifest fetched into dir.
func Cached(dir string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, CacheFile))
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Load fetches the manifest, falling back to the cached copy when offline.
func Load(dir, source string) (*Manifest, error) {
	m, err := Fetch(dir, source)
	if err == nil {
		return m, nil
	}
	if cached, cerr := Cached(dir); cerr == nil {
		return cached, nil
	}
	return nil, err
}

func download(source string) ([]byte, error) {
	if source == "" {
		return nil, errors.New("no CARE release list is configured")
	}
	u, err := url.Parse(source)
	if err != nil || u.Scheme == "" || len(u.Scheme) == 1 {
		// A plain path (len 1 scheme = Windows drive letter).
		return readLimited(source)
	}
	switch u.Scheme {
	case "https":
	case "file":
		return readLimited(u.Path)
	case "http":
		if host := u.Hostname(); host != "localhost" && host != "127.0.0.1" {
			return nil, errors.New("the CARE release list must be served over https")
		}
	default:
		return nil, fmt.Errorf("CARE release list location %q is not supported", source)
	}
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "care-clinic")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't download the CARE release list: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the CARE release list answered HTTP %d", resp.StatusCode)
	}
	return readAllLimited(resp.Body)
}

func readLimited(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("couldn't read the CARE release list: %w", err)
	}
	defer func() { _ = f.Close() }()
	return readAllLimited(f)
}

func readAllLimited(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxManifestBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxManifestBytes {
		return nil, errors.New("the CARE release list is too large")
	}
	return data, nil
}
