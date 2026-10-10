package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/compose"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/applog"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) ExportDiagnostics() (err error) {
	defer a.logError(&err)
	yes, err := a.askToProceed("Export diagnostics",
		"Logs may contain patient information, URLs or other sensitive details. Review the ZIP before sharing it. Nothing is uploaded automatically.",
		"Export")
	if err != nil || !yes {
		return err
	}
	path, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{
		Title: "Save CARE diagnostics", DefaultFilename: "care-diagnostics-" + time.Now().Format("20060102-150405") + ".zip",
		Filters: []wruntime.FileFilter{{DisplayName: "ZIP archive", Pattern: "*.zip"}},
	})
	if err != nil || path == "" {
		return err
	}
	dir := a.log.Folder()
	if dir == "" {
		return fmt.Errorf("the logs folder is unavailable")
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return err
	}
	if rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel) {
		return fmt.Errorf("save diagnostics outside the logs folder")
	}
	metadata := map[string]string{
		"app_version": a.pins.AppVersion, "engine_version": a.pins.CareVMVersion,
		"platform": runtime.GOOS + "/" + runtime.GOARCH, "captured_at": time.Now().UTC().Format(time.RFC3339),
		"warning": "Logs may contain sensitive information. Review before sharing.",
	}
	lock := compose.ReadLock(a.installDir())
	metadata["care_line"] = lock.Line
	metadata["backend_commit"] = lock.Get("backend").Current.Commit
	metadata["frontend_commit"] = lock.Get("frontend").Current.Commit
	if err := writeDiagnostics(dir, path, metadata); err != nil {
		return err
	}
	a.logln("Diagnostics saved to " + path)
	return nil
}

func writeDiagnostics(root, dest string, metadata map[string]string) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".care-diagnostics-*.zip")
	if err != nil {
		return err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	archive := zip.NewWriter(tmp)
	meta, err := archive.Create("versions.json")
	if err != nil {
		return err
	}
	if err := json.NewEncoder(meta).Encode(metadata); err != nil {
		return err
	}
	var total int64
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Type()&os.ModeSymlink != 0 || !applog.IsLog(d.Name()) {
			return nil
		}
		f, err := os.Open(path)
		if os.IsNotExist(err) {
			return nil // rotated between listing and opening
		}
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		info, err := f.Stat()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		size := min(info.Size(), int64(10<<20))
		if total+size > applog.LogBudget {
			return fmt.Errorf("diagnostics exceed the 500 MB limit; try again after log retention runs")
		}
		if _, err := f.Seek(info.Size()-size, io.SeekStart); err != nil {
			return err
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out, err := archive.Create(filepath.ToSlash(name))
		if err != nil {
			return err
		}
		n, err := io.Copy(out, io.LimitReader(f, size))
		total += n
		return err
	})
	if err != nil {
		_ = archive.Close()
		return err
	}
	if err := archive.Close(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dest)
}
