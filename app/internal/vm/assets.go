package vm

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Manifest describes one engine bundle (vm/nix/bundle.nix writes it).
type Manifest struct {
	Version string            `json:"version"`
	Arch    string            `json:"arch"`
	Kernel  string            `json:"kernel"`
	Initrd  string            `json:"initrd"`
	System  string            `json:"system"`
	Cmdline string            `json:"cmdline"`
	Files   map[string]string `json:"files"`

	dir string
}

func (m Manifest) path(name string) string { return filepath.Join(m.dir, name) }

// commandLine is the kernel command line. init= points at the NixOS system
// inside the read-only store disk.
func (m Manifest) commandLine(console string) string {
	cl := strings.TrimSpace(m.Cmdline)
	if cl == "" {
		cl = "reboot=t panic=1"
	}
	// Each hypervisor exposes a different console device; drop the others.
	var keep []string
	for _, f := range strings.Fields(cl) {
		if strings.HasPrefix(f, "console=") || strings.HasPrefix(f, "init=") {
			continue
		}
		keep = append(keep, f)
	}
	keep = append([]string{"console=" + console}, keep...)
	return strings.Join(append(keep, "init="+m.System+"/init"), " ")
}

func currentFile() string { return filepath.Join(engineDir(), "current") }

// Installed returns the active engine bundle.
func Installed() (Manifest, error) {
	b, err := os.ReadFile(currentFile())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Manifest{}, errNotInstalled
		}
		return Manifest{}, err
	}
	return readManifest(filepath.Join(engineDir(), strings.TrimSpace(string(b))))
}

func readManifest(dir string) (Manifest, error) {
	var m Manifest
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf("engine manifest: %w", err)
	}
	if m.Version == "" || m.Kernel == "" || m.System == "" {
		return m, errors.New("engine manifest is incomplete")
	}
	m.dir = dir
	return m, nil
}

// ToolsInstalled reports whether the docker CLI and compose plugin are in place.
func ToolsInstalled() bool {
	for _, p := range []string{dockerCLI(), composePlugin()} {
		if _, err := os.Stat(p); err != nil {
			return false
		}
	}
	if runtime.GOOS == "windows" {
		if _, err := os.Stat(qemuBinary()); err != nil {
			return false
		}
	}
	return true
}

func dockerCLI() string { return filepath.Join(BinDir(), exe("docker")) }
func composePlugin() string {
	return filepath.Join(dockerConfig(), "cli-plugins", exe("docker-compose"))
}
func qemuDir() string    { return filepath.Join(StateDir(), "qemu") }
func qemuBinary() string { return filepath.Join(qemuDir(), "qemu-system-x86_64.exe") }

func engineDir0() string {
	_ = os.MkdirAll(engineDir(), 0o700)
	return engineDir()
}

func pruneEngines(keep string) {
	entries, _ := os.ReadDir(engineDir())
	for _, e := range entries {
		if e.IsDir() && e.Name() != keep {
			_ = os.RemoveAll(filepath.Join(engineDir(), e.Name()))
		}
	}
}

func installFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	tmp := dst + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	_ = os.Remove(dst)
	return os.Rename(tmp, dst)
}

func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// extract unpacks a .tar.gz/.tgz or .zip archive into dst, refusing entries
// that would land outside it.
func extract(archive, dst string, strip int) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	if strings.HasSuffix(strings.ToLower(archive), ".zip") {
		return extractZip(archive, dst, strip)
	}
	return extractTarGz(archive, dst, strip)
}

func target(dst, name string, strip int) (string, bool) {
	name = strings.TrimPrefix(filepath.ToSlash(name), "./")
	parts := strings.Split(name, "/")
	if len(parts) <= strip {
		return "", false
	}
	rel := filepath.FromSlash(strings.Join(parts[strip:], "/"))
	if rel == "" || rel == "." {
		return "", false
	}
	p := filepath.Join(dst, rel)
	if !strings.HasPrefix(p, filepath.Clean(dst)+string(os.PathSeparator)) {
		return "", false
	}
	return p, true
}

func extractTarGz(archive, dst string, strip int) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		p, ok := target(dst, h.Name, strip)
		if !ok {
			continue
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := writeEntry(p, tr, os.FileMode(h.Mode)&0o777|0o600); err != nil {
				return err
			}
		}
	}
}

func extractZip(archive, dst string, strip int) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer func() { _ = zr.Close() }()
	for _, zf := range zr.File {
		p, ok := target(dst, zf.Name, strip)
		if !ok {
			continue
		}
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		err = writeEntry(p, rc, zf.Mode()&0o777|0o600)
		_ = rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func writeEntry(p string, r io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// prepareDataDisk gives the VM its private, writable disk. The first boot
// copies the bundle's small formatted seed; the file is then grown sparsely
// to the configured size and the guest grows the filesystem to match. It is
// never shrunk or reformatted.
func prepareDataDisk(m Manifest, sizeGB int) error {
	if _, err := os.Stat(dataDisk()); errors.Is(err, os.ErrNotExist) {
		if err := installFile(m.path("data.img"), dataDisk()); err != nil {
			return err
		}
		_ = os.Chmod(dataDisk(), 0o600)
	}
	info, err := os.Stat(dataDisk())
	if err != nil {
		return err
	}
	want := int64(sizeGB) << 30
	if info.Size() < want {
		makeSparse(dataDisk())
		return os.Truncate(dataDisk(), want)
	}
	return nil
}
