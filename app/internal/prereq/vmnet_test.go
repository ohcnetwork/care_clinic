package prereq

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/elevate"
)

func writeVMNetFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := []struct{ name, body string }{
		{"socket_vmnet.rancher-desktop-shared", ""},
		{"rancher-desktop-shared_socket_vmnet.pid", "100\n"},
		{"socket_vmnet.rancher-desktop-bridged_en0", ""},
		{"rancher-desktop-bridged_en0_socket_vmnet.pid", "200\n"},
		{"socket_vmnet.host", ""},
		{"socket_vmnet.shared", ""},
		{"shared_socket_vmnet.pid", "300\n"},
		{"rancher-desktop-bridged_en5_socket_vmnet.pid", "garbage"},
		{"docker.sock", ""},
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f.name), []byte(f.body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func onlyAlive(pids ...int) func(int) bool {
	return func(pid int) bool {
		for _, p := range pids {
			if p == pid {
				return true
			}
		}
		return false
	}
}

func TestStaleVMNetFilesSkipsLiveAndForeignDaemons(t *testing.T) {
	dir := writeVMNetFixture(t)
	got := staleVMNetFiles(dir, onlyAlive(200, 300))
	want := []string{
		filepath.Join(dir, "socket_vmnet.host"),
		filepath.Join(dir, "rancher-desktop-bridged_en5_socket_vmnet.pid"),
		filepath.Join(dir, "socket_vmnet.rancher-desktop-shared"),
		filepath.Join(dir, "rancher-desktop-shared_socket_vmnet.pid"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestRancherVMNetRemoveCmdsStopsLiveDaemonsFirst(t *testing.T) {
	dir := writeVMNetFixture(t)
	cmds := rancherVMNetRemoveCmds(dir, onlyAlive(200, 300))
	if len(cmds) != 2 {
		t.Fatalf("got %d commands: %v", len(cmds), cmds)
	}
	livePid := elevate.ShQuote(filepath.Join(dir, "rancher-desktop-bridged_en0_socket_vmnet.pid"))
	if cmds[0] != "pkill -F "+livePid+" socket_vmnet 2>/dev/null" {
		t.Errorf("unexpected stop command: %s", cmds[0])
	}
	for _, name := range []string{"socket_vmnet.rancher-desktop-bridged_en0", "socket_vmnet.rancher-desktop-shared", "socket_vmnet.host"} {
		if !strings.Contains(cmds[1], elevate.ShQuote(filepath.Join(dir, name))) {
			t.Errorf("rm command misses %s: %s", name, cmds[1])
		}
	}
	for _, name := range []string{"socket_vmnet.shared", "shared_socket_vmnet.pid", "docker.sock"} {
		if strings.Contains(cmds[1], elevate.ShQuote(filepath.Join(dir, name))) {
			t.Errorf("rm command touches a file Rancher does not own: %s", name)
		}
	}
}

func TestVMNetCleanupIsEmptyWithoutLeftovers(t *testing.T) {
	dir := t.TempDir()
	if files := staleVMNetFiles(dir, onlyAlive()); len(files) != 0 {
		t.Errorf("expected nothing, got %v", files)
	}
	if cmds := staleVMNetSudoCmds(dir, onlyAlive()); len(cmds) != 0 {
		t.Errorf("expected no sudo cleanup, got %v", cmds)
	}
	if cmds := rancherVMNetRemoveCmds(dir, onlyAlive()); len(cmds) != 0 {
		t.Errorf("expected nothing, got %v", cmds)
	}
	if files := staleVMNetFiles(filepath.Join(dir, "missing"), onlyAlive()); len(files) != 0 {
		t.Errorf("expected nothing for a missing dir, got %v", files)
	}
}

func TestStaleVMNetSudoCmdsRemoveOnlyStoppedDaemons(t *testing.T) {
	dir := writeVMNetFixture(t)
	if err := os.WriteFile(filepath.Join(dir, "socket_vmnet.rancher-desktop-bad name"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got := staleVMNetSudoCmds(dir, onlyAlive(200, 300))
	var want [][]string
	for _, name := range []string{"host", "rancher-desktop-bridged_en5", "rancher-desktop-shared"} {
		want = append(want, []string{"-n", "/bin/rm", "-f",
			filepath.Join(dir, "socket_vmnet."+name), filepath.Join(dir, name+"_socket_vmnet.pid")})
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}
