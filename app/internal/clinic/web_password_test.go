package clinic

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/release"
)

const fakeWebDocker = `#!/bin/sh
for arg do printf '%s\n--\n' "$arg"; done >> "$CARE_FAKE_ARGS"
case "$1 $2" in
  'compose ps')
    case "$CARE_FAKE_STATE" in
      stopped) printf 'db running\nbackend exited\n' ;;
      unreachable) exit 1 ;;
      *) printf 'db running\nbackend running\n' ;;
    esac
    exit 0 ;;
  'compose exec')
    printf '%s\n%s\n' "$CARE_WEB_USERNAME" "$CARE_WEB_PASSWORD" > "$CARE_FAKE_ENV"
    case "$CARE_FAKE_RESULT" in
      missing) printf 'care-clinic:no-user\n'; exit 3 ;;
      invalid) printf 'care-clinic:invalid:This password is too short.\ncare-clinic:invalid:This password is too common.\n'; exit 4 ;;
      crash) printf 'Traceback: OperationalError while saving %s\n' "$CARE_WEB_PASSWORD"; exit 1 ;;
      silent) exit 0 ;;
      *) printf '7 objects imported automatically\ncare-clinic:password-set\n'; exit 0 ;;
    esac ;;
esac
exit 99
`

type webPasswordFixture struct {
	clinic *Clinic
	args   string
	env    string
	mu     sync.Mutex
	logs   []string
}

func newWebPasswordFixture(t *testing.T) *webPasswordFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX command fixture")
	}
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(fakeWebDocker), 0o700); err != nil {
		t.Fatal(err)
	}
	f := &webPasswordFixture{args: filepath.Join(root, "args"), env: filepath.Join(root, "env")}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CARE_FAKE_ARGS", f.args)
	t.Setenv("CARE_FAKE_ENV", f.env)
	t.Setenv("CARE_FAKE_STATE", "")
	t.Setenv("CARE_FAKE_RESULT", "")
	f.clinic = &Clinic{
		InstallDir: root, BackupDir: filepath.Join(root, "backups"), Pins: &release.Pins{},
		Log: func(line string) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.logs = append(f.logs, line)
		},
	}
	return f
}

func (f *webPasswordFixture) read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func (f *webPasswordFixture) log() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.logs, "\n")
}

const webSecret = "Synthetic-Web-Secret-472"

func TestResetWebPasswordPassesSecretsOnlyThroughTheEnvironment(t *testing.T) {
	f := newWebPasswordFixture(t)
	if err := f.clinic.ResetWebPassword("  admin ", webSecret); err != nil {
		t.Fatal(err)
	}
	args := f.read(t, f.args)
	if strings.Contains(args, webSecret) {
		t.Fatal("the new password appeared on the docker command line")
	}
	for _, want := range []string{"exec\n--\n-T\n", "-e\n--\nCARE_WEB_USERNAME\n", "-e\n--\nCARE_WEB_PASSWORD\n",
		"backend\n--\npython\n--\nmanage.py\n--\nshell\n--\n-c\n", "validate_password(password, user)", "set_password(password)"} {
		if !strings.Contains(args, want) {
			t.Fatalf("docker call is missing %q:\n%s", want, args)
		}
	}
	if strings.Contains(args, "\nadmin\n") {
		t.Fatal("the username was passed as an argument instead of through the environment")
	}
	if got := f.read(t, f.env); got != "admin\n"+webSecret+"\n" {
		t.Fatalf("the container did not receive the username and password through the environment: %q", got)
	}
	if log := f.log(); strings.Contains(log, webSecret) || !strings.Contains(log, "Changed the CARE web password for admin.") {
		t.Fatalf("unexpected log: %s", log)
	}
}

func TestResetWebPasswordReportsOutcomes(t *testing.T) {
	for _, tc := range []struct {
		result string
		want   string
	}{
		{"missing", `no CARE user named "nurse"`},
		{"invalid", "CARE didn't accept that password: This password is too short. This password is too common."},
		{"crash", "CARE couldn't change that web password; nothing was confirmed. Open the log file for details"},
		{"silent", "CARE couldn't change that web password; nothing was confirmed. Open the log file for details"},
	} {
		t.Run(tc.result, func(t *testing.T) {
			f := newWebPasswordFixture(t)
			t.Setenv("CARE_FAKE_RESULT", tc.result)
			err := f.clinic.ResetWebPassword("nurse", webSecret)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
			if strings.Contains(err.Error(), webSecret) || strings.Contains(f.log(), webSecret) {
				t.Fatal("the new password leaked into an error or the log")
			}
			if tc.result == "crash" && !strings.Contains(f.log(), "OperationalError") {
				t.Fatalf("the failure detail was not logged: %s", f.log())
			}
		})
	}
}

func TestResetWebPasswordRefusesWhileCareIsNotRunning(t *testing.T) {
	for _, state := range []string{"stopped", "unreachable"} {
		t.Run(state, func(t *testing.T) {
			f := newWebPasswordFixture(t)
			t.Setenv("CARE_FAKE_STATE", state)
			if err := f.clinic.ResetWebPassword("admin", webSecret); !errors.Is(err, ErrCareNotRunning) {
				t.Fatalf("got %v, want ErrCareNotRunning", err)
			}
			if strings.Contains(f.read(t, f.args), "exec") || f.read(t, f.env) != "" {
				t.Fatal("the password change ran while CARE was not running")
			}
		})
	}
}

func TestResetWebPasswordRequiresAUsernameAndPassword(t *testing.T) {
	f := newWebPasswordFixture(t)
	if err := f.clinic.ResetWebPassword("  ", webSecret); err == nil {
		t.Fatal("an empty username was accepted")
	}
	if err := f.clinic.ResetWebPassword("admin", ""); err == nil {
		t.Fatal("an empty password was accepted")
	}
	if f.read(t, f.args) != "" {
		t.Fatal("docker ran for an incomplete request")
	}
}

func TestResetWebPasswordScriptIsValidPython(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not available")
	}
	cmd := exec.Command(python, "-I", "-c", "import sys; compile(sys.stdin.read(), 'reset', 'exec')")
	cmd.Stdin = strings.NewReader(webPasswordScript)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, want := range []string{"sys.exit(3)", "sys.exit(4)", "USERNAME_FIELD"} {
		if !strings.Contains(webPasswordScript, want) {
			t.Fatalf("script is missing %q", want)
		}
	}
}
