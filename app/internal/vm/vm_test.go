package vm

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestWindowsToGuest(t *testing.T) {
	cases := map[string]string{
		`C:\Users\Asha\care clinic`: "/c/Users/Asha/care clinic",
		`d:/backups/`:               "/d/backups",
		`E:\`:                       "/e",
	}
	for in, want := range cases {
		if got, ok := windowsToGuest(in); !ok || got != want {
			t.Errorf("%q -> %q %v, want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"/usr/x", "relative", `\\server\share`, "1:/x"} {
		if _, ok := windowsToGuest(in); ok {
			t.Errorf("%q should not map", in)
		}
	}
}

func TestRewriteDockerArgs(t *testing.T) {
	args := []string{"run", "--rm", "-v", `C:\care\backups:/backups:ro`,
		"--mount", `type=bind,source=C:\care\key.age,target=/key,readonly`,
		"--volume=care_pg:/var/lib/postgresql", "img", "sh", "-c", "-v x"}
	got, paths := RewriteDockerArgs(args)
	want := []string{"run", "--rm", "-v", "/c/care/backups:/backups:ro",
		"--mount", "type=bind,source=/c/care/key.age,target=/key,readonly",
		"--volume=care_pg:/var/lib/postgresql", "img", "sh", "-c", "-v x"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
	if !reflect.DeepEqual(paths, []string{`C:\care\backups`, `C:\care\key.age`}) {
		t.Fatalf("paths %q", paths)
	}
	compose := []string{"compose", "down", "-v"}
	if got, p := RewriteDockerArgs(compose); !reflect.DeepEqual(got, compose) || p != nil {
		t.Fatalf("compose changed: %q", got)
	}
	cr := []string{"--log-level", "warn", "container", "create", "-v", `C:\x:/x`, "img"}
	if got, _ := RewriteDockerArgs(cr); got[5] != "/c/x:/x" {
		t.Fatalf("container create: %q", got)
	}
}

func TestCommandLine(t *testing.T) {
	m := Manifest{System: "/nix/store/abc-system", Cmdline: "console=hvc0 console=ttyS0 reboot=t panic=1 loglevel=4"}
	if got := m.commandLine("ttyS0"); got != "console=ttyS0 reboot=t panic=1 loglevel=4 init=/nix/store/abc-system/init" {
		t.Fatal(got)
	}
}

func TestConfigDefaults(t *testing.T) {
	c := Config{}.withDefaults(8)
	if c.CPUs != 4 || c.MemoryMB != 4096 || c.DiskGB != 64 {
		t.Fatalf("%+v", c)
	}
	if c := (Config{CPUs: 16, MemoryMB: 512}).withDefaults(2); c.CPUs != 2 || c.MemoryMB != minMemoryMB {
		t.Fatalf("%+v", c)
	}
}

func writeTarGz(t *testing.T, p string, files map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		_ = tw.WriteHeader(&tar.Header{Name: n, Mode: 0o644, Size: int64(len(files[n])), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(files[n]))
	}
	_ = tw.Close()
	_ = gz.Close()
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func sum(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func TestInstallEngineChecksAndExtracts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LOCALAPPDATA", home)
	files := map[string]string{"kernel": "k", "initrd": "i", "system.img": "s", "data.img": "d"}
	man := map[string]any{"version": "0.1.0", "arch": "arm64", "kernel": "kernel", "initrd": "initrd", "system": "/nix/store/x", "files": map[string]string{}}
	for n, c := range files {
		man["files"].(map[string]string)[n] = sum(c)
	}
	mb, _ := json.Marshal(man)
	files["./manifest.json"] = string(mb)
	files["../evil"] = "x"
	arc := filepath.Join(t.TempDir(), "engine.tar.gz")
	writeTarGz(t, arc, files)
	m, err := InstallEngine(arc)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Installed(); err != nil || got.Version != "0.1.0" || got.dir != m.dir {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(engineDir()), "evil")); !os.IsNotExist(err) {
		t.Fatal("path traversal entry was extracted")
	}
	if err := prepareDataDisk(m, 1); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(dataDisk()); fi.Size() != 1<<30 {
		t.Fatalf("data disk size %d", fi.Size())
	}

	files["kernel"] = "tampered"
	writeTarGz(t, arc, files)
	if _, err := InstallEngine(arc); err == nil || !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("tampered bundle accepted: %v", err)
	}
}

// fakeGuest is an in-memory guest file system.
type fakeGuest struct{ files map[string]fakeFile }

type fakeFile struct {
	data  string
	mtime int64
}

func (f *fakeGuest) list(_ context.Context, root string) ([]guestEntry, error) {
	var out []guestEntry
	for p, ff := range f.files {
		if strings.HasPrefix(p, root+"/") {
			out = append(out, guestEntry{Path: strings.TrimPrefix(p, root+"/"), Size: int64(len(ff.data)), MtimeNs: ff.mtime})
		}
	}
	return out, nil
}

func (f *fakeGuest) get(_ context.Context, p string) (io.ReadCloser, int64, error) {
	ff, ok := f.files[p]
	if !ok {
		return nil, 0, os.ErrNotExist
	}
	return io.NopCloser(strings.NewReader(ff.data)), ff.mtime, nil
}

func (f *fakeGuest) put(_ context.Context, p string, r io.Reader, _ uint32, mt time.Time) error {
	b, _ := io.ReadAll(r)
	f.files[p] = fakeFile{string(b), mt.UnixNano()}
	return nil
}

func (f *fakeGuest) remove(_ context.Context, p string) error { delete(f.files, p); return nil }

func TestSyncThreeWay(t *testing.T) {
	root := t.TempDir()
	g := &fakeGuest{files: map[string]fakeFile{}}
	groot := "/guest/install"
	s := &syncer{g: g, logf: t.Logf, state: t.TempDir(), roots: func() []string { return []string{root} },
		toGst: func(string) string { return groot }, skipFn: skipSync}
	ctx := context.Background()
	write := func(rel, data string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(data), 0o644)
	}
	write("Caddyfile", "one")
	write("scripts/backup.sh", "#!/bin/sh")
	write(".git/HEAD", "ref")
	if err := s.run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if g.files[groot+"/Caddyfile"].data != "one" || g.files[groot+"/scripts/backup.sh"].data == "" {
		t.Fatalf("push: %+v", g.files)
	}
	if _, ok := g.files[groot+"/.git/HEAD"]; ok {
		t.Fatal(".git should be skipped")
	}

	// The guest writes a new file and changes nothing else: it comes back.
	g.files[groot+"/backup-state/last.json"] = fakeFile{"{}", time.Now().UnixNano()}
	// The host deletes a file: the guest copy goes too.
	_ = os.Remove(filepath.Join(root, "scripts", "backup.sh"))
	if err := s.run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(root, "backup-state", "last.json")); err != nil || string(b) != "{}" {
		t.Fatalf("pull: %q %v", b, err)
	}
	if _, ok := g.files[groot+"/scripts/backup.sh"]; ok {
		t.Fatal("host delete not propagated")
	}

	// The guest deletes a file the host did not touch: the host copy goes.
	delete(g.files, groot+"/backup-state/last.json")
	// Both change the same file: the host wins.
	write("Caddyfile", "host-two")
	g.files[groot+"/Caddyfile"] = fakeFile{"guest-two", time.Now().Add(time.Hour).UnixNano()}
	if err := s.run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "backup-state", "last.json")); !os.IsNotExist(err) {
		t.Fatal("guest delete not propagated")
	}
	if g.files[groot+"/Caddyfile"].data != "host-two" {
		t.Fatalf("conflict: %q", g.files[groot+"/Caddyfile"].data)
	}

	// A steady state does no work.
	before := len(g.files)
	if err := s.run(ctx, nil); err != nil || len(g.files) != before {
		t.Fatalf("steady state changed: %v", err)
	}
}

func TestMountSensitive(t *testing.T) {
	yes := [][]string{{"run", "img"}, {"compose", "up", "-d"}, {"compose", "-f", "x.yml", "run", "backup"}, {"container", "start", "x"}, {"compose", "exec", "backup", "sh"}}
	no := [][]string{{"version"}, {"compose", "ps"}, {"image", "inspect", "x"}, {"compose", "-p", "up", "ps"}, {}}
	for _, a := range yes {
		if !MountSensitive(a) {
			t.Errorf("%q should be mount-sensitive", a)
		}
	}
	for _, a := range no {
		if MountSensitive(a) {
			t.Errorf("%q should not be mount-sensitive", a)
		}
	}
}
