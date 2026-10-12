package vm

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func engineFiles(version, kernel string) map[string]string {
	files := map[string]string{"kernel": kernel, "initrd": "i", "system.img": "s", "data.img": "d"}
	sums := map[string]string{}
	for n, c := range files {
		sums[n] = sum(c)
	}
	mb, _ := json.Marshal(map[string]any{"version": version, "arch": "arm64", "kernel": "kernel",
		"initrd": "initrd", "system": "/nix/store/x", "files": sums})
	files["./manifest.json"] = string(mb)
	return files
}

// toolFiles writes the docker, compose and (on Windows) QEMU downloads whose
// contents are tagged with tag, so tests can tell versions apart.
func toolFiles(t *testing.T, engineArchive, tag string) EngineFiles {
	t.Helper()
	dir := t.TempDir()
	f := EngineFiles{Engine: engineArchive, Docker: filepath.Join(dir, "docker.tgz"), Compose: filepath.Join(dir, "compose")}
	writeTarGz(t, f.Docker, map[string]string{"docker/" + exe("docker"): "docker-" + tag})
	if err := os.WriteFile(f.Compose, []byte("compose-"+tag), 0o600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		f.QEMU = filepath.Join(dir, "qemu.zip")
		out, err := os.Create(f.QEMU)
		if err != nil {
			t.Fatal(err)
		}
		zw := zip.NewWriter(out)
		w, _ := zw.Create("qemu-system-x86_64.exe")
		_, _ = w.Write([]byte("qemu-" + tag))
		_ = zw.Close()
		_ = out.Close()
	}
	return f
}

func engineArchive(t *testing.T, version string) string {
	t.Helper()
	arc := filepath.Join(t.TempDir(), "engine-"+version+".tar.gz")
	writeTarGz(t, arc, engineFiles(version, "kernel-"+version))
	return arc
}

func installEngineForTest(t *testing.T, arc string) {
	t.Helper()
	s, err := StageEngineUpdate(toolFiles(t, arc, "v1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Activate(); err != nil {
		t.Fatal(err)
	}
	if err := CommitEngineUpdate(); err != nil {
		t.Fatal(err)
	}
}

func setupUpdateTest(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LOCALAPPDATA", home)
	installEngineForTest(t, engineArchive(t, "0.1.0"))
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func assertEngine(t *testing.T, version, tag string) {
	t.Helper()
	if m, err := Installed(); err != nil || m.Version != version {
		t.Fatalf("installed engine = %+v, %v; want %s", m, err, version)
	}
	if got := read(t, dockerCLI()); got != "docker-"+tag {
		t.Fatalf("docker = %q, want %s", got, tag)
	}
	if got := read(t, composePlugin()); got != "compose-"+tag {
		t.Fatalf("compose = %q, want %s", got, tag)
	}
	if runtime.GOOS == "windows" {
		if got := read(t, qemuBinary()); got != "qemu-"+tag {
			t.Fatalf("qemu = %q, want %s", got, tag)
		}
	}
}

func engineBundles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(engineDir())
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

func TestStageFailureLeavesInstalledEngineUntouched(t *testing.T) {
	setupUpdateTest(t)
	files := engineFiles("0.1.1", "k")
	files["kernel"] = "tampered"
	arc := filepath.Join(t.TempDir(), "bad.tar.gz")
	writeTarGz(t, arc, files)
	if _, err := StageEngineUpdate(toolFiles(t, arc, "v2")); err == nil {
		t.Fatal("corrupt update was staged")
	}
	assertEngine(t, "0.1.0", "v1")
	if _, ok, _ := PendingEngineUpdate(); ok {
		t.Fatal("a failed stage left an update record")
	}
	if _, err := os.Stat(updateDir()); !os.IsNotExist(err) {
		t.Fatal("a failed stage left staged files")
	}
}

func TestCommitKeepsNewEngineAndPrunesOld(t *testing.T) {
	setupUpdateTest(t)
	s, err := StageEngineUpdate(toolFiles(t, engineArchive(t, "0.1.1"), "v2"))
	if err != nil {
		t.Fatal(err)
	}
	assertEngine(t, "0.1.0", "v1")
	if err := s.Activate(); err != nil {
		t.Fatal(err)
	}
	if u, ok, _ := PendingEngineUpdate(); !ok || u.Phase != UpdateVerifying || u.From != "0.1.0" || u.To != "0.1.1" {
		t.Fatalf("record after activation = %+v", u)
	}
	assertEngine(t, "0.1.1", "v2")
	if got := engineBundles(t); len(got) != 2 {
		t.Fatalf("previous bundle must be kept until commit, have %v", got)
	}
	if err := CommitEngineUpdate(); err != nil {
		t.Fatal(err)
	}
	assertEngine(t, "0.1.1", "v2")
	if got := engineBundles(t); len(got) != 1 || got[0] != "0.1.1" {
		t.Fatalf("bundles after commit = %v", got)
	}
	if _, err := os.Stat(updateDir()); !os.IsNotExist(err) {
		t.Fatal("commit left update files behind")
	}
}

func TestRollbackRestoresPreviousEngineAndRecordsFailure(t *testing.T) {
	setupUpdateTest(t)
	if err := os.WriteFile(dataDisk(), []byte("patient data"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := StageEngineUpdate(toolFiles(t, engineArchive(t, "0.1.1"), "v2"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Activate(); err != nil {
		t.Fatal(err)
	}
	if err := RollbackEngineUpdate("docker never answered"); err != nil {
		t.Fatal(err)
	}
	assertEngine(t, "0.1.0", "v1")
	if got := engineBundles(t); len(got) != 1 || got[0] != "0.1.0" {
		t.Fatalf("bundles after rollback = %v", got)
	}
	if got := read(t, dataDisk()); got != "patient data" {
		t.Fatalf("rollback touched the data disk: %q", got)
	}
	u, ok, err := PendingEngineUpdate()
	if err != nil || !ok || u.Interrupted() || u.Error != "docker never answered" || u.To != "0.1.1" {
		t.Fatalf("failed record = %+v %v %v", u, ok, err)
	}
	// Rolling back again is harmless.
	if err := RollbackEngineUpdate("again"); err != nil {
		t.Fatal(err)
	}
	assertEngine(t, "0.1.0", "v1")
	if err := ClearFailedEngineUpdate(); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := PendingEngineUpdate(); ok {
		t.Fatal("retry did not clear the failed update")
	}
}

func TestInterruptedActivationRollsBackEveryStep(t *testing.T) {
	setupUpdateTest(t)
	s, err := StageEngineUpdate(toolFiles(t, engineArchive(t, "0.1.1"), "v2"))
	if err != nil {
		t.Fatal(err)
	}
	// Losing the staged compose plugin stops activation after the docker CLI
	// was replaced and compose was moved aside: the state an interruption leaves.
	if err := os.Remove(filepath.Join(newDir(), "docker-compose")); err != nil {
		t.Fatal(err)
	}
	if err := s.Activate(); err == nil {
		t.Fatal("activation succeeded without compose")
	}
	u, ok, _ := PendingEngineUpdate()
	if !ok || !u.Interrupted() {
		t.Fatalf("record after interrupted activation = %+v", u)
	}
	if _, err := os.Stat(composePlugin()); !os.IsNotExist(err) {
		t.Fatal("expected compose to be moved aside mid-activation")
	}
	if _, err := StageEngineUpdate(toolFiles(t, engineArchive(t, "0.1.1"), "v3")); err == nil ||
		!strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("a new update started over an unrecovered one: %v", err)
	}
	if err := RollbackEngineUpdate("the update was interrupted before it finished"); err != nil {
		t.Fatal(err)
	}
	assertEngine(t, "0.1.0", "v1")
}

func TestToolsOnlyUpdateKeepsBundle(t *testing.T) {
	setupUpdateTest(t)
	if err := os.Remove(composePlugin()); err != nil {
		t.Fatal(err)
	}
	s, err := StageEngineUpdate(toolFiles(t, engineArchive(t, "0.1.0"), "v2"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Activate(); err != nil {
		t.Fatal(err)
	}
	if err := RollbackEngineUpdate("failed"); err != nil {
		t.Fatal(err)
	}
	if m, err := Installed(); err != nil || m.Version != "0.1.0" {
		t.Fatalf("same-version rollback lost the bundle: %+v %v", m, err)
	}
	if _, err := os.Stat(composePlugin()); !os.IsNotExist(err) {
		t.Fatal("rollback should restore the missing compose plugin as missing")
	}
	if got := read(t, dockerCLI()); got != "docker-v1" {
		t.Fatalf("docker = %q", got)
	}
}

func TestFirstInstallRollbackRemovesPartialInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LOCALAPPDATA", home)
	s, err := StageEngineUpdate(toolFiles(t, engineArchive(t, "0.1.0"), "v1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Activate(); err != nil {
		t.Fatal(err)
	}
	if err := RollbackEngineUpdate("failed"); err != nil {
		t.Fatal(err)
	}
	if _, err := Installed(); err == nil || ToolsInstalled() {
		t.Fatal("first-install rollback left an engine behind")
	}
}
