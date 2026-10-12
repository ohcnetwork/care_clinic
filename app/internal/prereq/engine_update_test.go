package prereq

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/vm"
)

func writeTestTarGz(t *testing.T, p string, files map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for n, c := range files {
		_ = tw.WriteHeader(&tar.Header{Name: n, Mode: 0o644, Size: int64(len(c)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(c))
	}
	_ = tw.Close()
	_ = gz.Close()
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func stageTestEngine(t *testing.T, version string) *vm.StagedUpdate {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{"kernel": "k" + version, "initrd": "i", "system.img": "s", "data.img": "d"}
	sums := map[string]string{}
	for n, c := range files {
		h := sha256.Sum256([]byte(c))
		sums[n] = hex.EncodeToString(h[:])
	}
	mb, _ := json.Marshal(map[string]any{"version": version, "kernel": "kernel", "initrd": "initrd",
		"system": "/nix/store/x", "files": sums})
	files["manifest.json"] = string(mb)
	f := vm.EngineFiles{
		Engine:  filepath.Join(dir, "engine.tar.gz"),
		Docker:  filepath.Join(dir, "docker.tgz"),
		Compose: filepath.Join(dir, "compose"),
	}
	writeTestTarGz(t, f.Engine, files)
	docker := "docker/docker"
	if runtime.GOOS == "windows" {
		docker += ".exe"
	}
	writeTestTarGz(t, f.Docker, map[string]string{docker: "docker " + version})
	if err := os.WriteFile(f.Compose, []byte("compose "+version), 0o600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		f.QEMU = filepath.Join(dir, "qemu.zip")
		out, _ := os.Create(f.QEMU)
		zw := zip.NewWriter(out)
		w, _ := zw.Create("qemu-system-x86_64.exe")
		_, _ = w.Write([]byte("qemu"))
		_ = zw.Close()
		_ = out.Close()
	}
	s, err := vm.StageEngineUpdate(f)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type fakeEngine struct {
	running   bool
	version   string
	bootErr   error
	stopErr   error
	stops     int
	boots     int
	bootSetsV string
}

func (f *fakeEngine) ops() *engineOps {
	return &engineOps{
		running: func(context.Context) bool { return f.running },
		stop: func(context.Context) error {
			f.stops++
			if f.stopErr != nil {
				return f.stopErr
			}
			f.running = false
			return nil
		},
		boot: func() error {
			f.boots++
			if f.bootErr != nil {
				return f.bootErr
			}
			f.running, f.version = true, f.bootSetsV
			return nil
		},
		version: func(context.Context) string { return f.version },
	}
}

// installedEngine sets up a home with engine 0.1.0 and returns a provisioner
// whose VM operations are faked by f.
func installedEngine(t *testing.T, f *fakeEngine) (*Provisioner, *[]string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LOCALAPPDATA", home)
	if err := (&Provisioner{testEngineOps: (&fakeEngine{bootSetsV: "0.1.0"}).ops()}).
		applyEngine(stageTestEngine(t, "0.1.0"), nil); err != nil {
		t.Fatal(err)
	}
	var logs []string
	return &Provisioner{testEngineOps: f.ops(), Log: func(s string) { logs = append(logs, s) }}, &logs
}

func installedVersion(t *testing.T) string {
	t.Helper()
	m, err := vm.Installed()
	if err != nil {
		t.Fatal(err)
	}
	return m.Version
}

func TestEngineUpdateCommitsWhenNewEngineAnswers(t *testing.T) {
	f := &fakeEngine{running: true, version: "0.1.0", bootSetsV: "0.1.1"}
	pr, _ := installedEngine(t, f)
	if err := pr.applyEngine(stageTestEngine(t, "0.1.1"), nil); err != nil {
		t.Fatal(err)
	}
	if v := installedVersion(t); v != "0.1.1" || f.stops != 1 || f.boots != 1 {
		t.Fatalf("installed %s, stops %d, boots %d", v, f.stops, f.boots)
	}
	if _, ok, _ := vm.PendingEngineUpdate(); ok {
		t.Fatal("a committed update left its record")
	}
}

func TestEngineUpdateRollsBackWhenNewEngineFails(t *testing.T) {
	for name, f := range map[string]*fakeEngine{
		"boot fails":       {running: true, version: "0.1.0", bootErr: errors.New("docker never answered")},
		"wrong version up": {running: true, version: "0.1.0", bootSetsV: "0.1.0"},
	} {
		t.Run(name, func(t *testing.T) {
			pr, _ := installedEngine(t, f)
			err := pr.applyEngine(stageTestEngine(t, "0.1.1"), nil)
			if err == nil || !strings.Contains(err.Error(), "restored CARE engine 0.1.0") {
				t.Fatalf("error = %v", err)
			}
			if v := installedVersion(t); v != "0.1.0" {
				t.Fatalf("installed %s after rollback", v)
			}
			// Once to make room for the update, once to stop the failed engine.
			if f.stops != 2 {
				t.Fatalf("stops = %d", f.stops)
			}
			u, ok, _ := vm.PendingEngineUpdate()
			if !ok || u.Interrupted() || u.To != "0.1.1" || u.Error == "" {
				t.Fatalf("failed record = %+v", u)
			}
		})
	}
}

func TestEngineUpdateReportsRestoreFailureAndRecoversLater(t *testing.T) {
	if !vm.Supported() {
		t.Skip("the CARE engine is not used on " + runtime.GOOS)
	}
	f := &fakeEngine{running: true, version: "0.1.0", bootErr: errors.New("docker never answered")}
	pr, _ := installedEngine(t, f)
	s := stageTestEngine(t, "0.1.1")
	// The first stop (before activation) works; stopping the failed engine does not.
	ops := f.ops()
	stop := ops.stop
	ops.stop = func(ctx context.Context) error {
		if f.stops >= 1 {
			f.stops++
			return errors.New("helper not answering")
		}
		return stop(ctx)
	}
	pr.testEngineOps = ops
	err := pr.applyEngine(s, nil)
	if err == nil || !strings.Contains(err.Error(), "docker never answered") || !strings.Contains(err.Error(), "helper not answering") {
		t.Fatalf("error must name the update and restore failures: %v", err)
	}
	if u, ok, _ := vm.PendingEngineUpdate(); !ok || !u.Interrupted() {
		t.Fatalf("an unrestored update must stay pending: %+v", u)
	}
	// Starting the engine later rolls the update back before anything else.
	pr.testEngineOps = (&fakeEngine{}).ops()
	if err := pr.RecoverEngineUpdate(); err != nil {
		t.Fatal(err)
	}
	if v := installedVersion(t); v != "0.1.0" {
		t.Fatalf("installed %s after recovery", v)
	}
	u, ok, _ := vm.PendingEngineUpdate()
	if !ok || u.Interrupted() || !strings.Contains(u.Error, "docker never answered") {
		t.Fatalf("recovered record = %+v", u)
	}
}

func TestInterruptedEngineUpdateIsRolledBackOnNextLaunch(t *testing.T) {
	if !vm.Supported() {
		t.Skip("the CARE engine is not used on " + runtime.GOOS)
	}
	f := &fakeEngine{running: true, version: "0.1.1"}
	pr, logs := installedEngine(t, f)
	// The app quit while the new engine was being verified; its helper is
	// still running the new engine.
	if err := stageTestEngine(t, "0.1.1").Activate(); err != nil {
		t.Fatal(err)
	}
	if err := pr.RecoverEngineUpdate(); err != nil {
		t.Fatal(err)
	}
	if v := installedVersion(t); v != "0.1.0" || f.stops != 1 || f.running {
		t.Fatalf("installed %s, stops %d, running %v", v, f.stops, f.running)
	}
	if u, ok, _ := vm.PendingEngineUpdate(); !ok || u.Interrupted() || !strings.Contains(u.Error, "interrupted") {
		t.Fatalf("record = %+v", u)
	}
	if !strings.Contains(strings.Join(*logs, "\n"), "didn't finish") {
		t.Fatalf("recovery was not logged: %v", *logs)
	}
	// A failed update is not recovered again.
	if err := pr.RecoverEngineUpdate(); err != nil || f.stops != 1 {
		t.Fatalf("second recovery: %v, stops %d", err, f.stops)
	}
}

func TestEngineUpdateLeavesClinicAloneWhenBeforeStopFails(t *testing.T) {
	f := &fakeEngine{running: true, version: "0.1.0"}
	pr, _ := installedEngine(t, f)
	err := pr.applyEngine(stageTestEngine(t, "0.1.1"), func() error { return errors.New("compose down failed") })
	if err == nil || f.stops != 0 || f.boots != 0 {
		t.Fatalf("err %v, stops %d, boots %d", err, f.stops, f.boots)
	}
	if v := installedVersion(t); v != "0.1.0" {
		t.Fatalf("installed %s", v)
	}
	if _, ok, _ := vm.PendingEngineUpdate(); ok {
		t.Fatal("an update that never started left a record")
	}
}
