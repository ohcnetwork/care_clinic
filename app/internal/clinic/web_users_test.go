package clinic

import (
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestListWebUsersParsesTheMarkedLine(t *testing.T) {
	f := newWebPasswordFixture(t)
	t.Setenv("CARE_FAKE_RESULT", "users")
	users, err := f.clinic.ListWebUsers()
	if err != nil {
		t.Fatal(err)
	}
	want := []WebUser{
		{Username: "admin", Active: true, Superuser: true},
		{Username: "nurse", Name: "Asha Rao"},
	}
	if !reflect.DeepEqual(users, want) {
		t.Fatalf("got %+v, want %+v", users, want)
	}
	args := f.read(t, f.args)
	for _, want := range []string{"exec\n--\n-T\n", "backend\n--\npython\n--\nmanage.py\n--\nshell\n--\n-c\n", webUsersMarker} {
		if !strings.Contains(args, want) {
			t.Fatalf("docker call is missing %q:\n%s", want, args)
		}
	}
	if strings.Contains(args, "set_password") || strings.Contains(args, ".save(") {
		t.Fatal("listing users must not change anything")
	}
}

func TestListWebUsersOutcomes(t *testing.T) {
	f := newWebPasswordFixture(t)
	t.Setenv("CARE_FAKE_RESULT", "no-users")
	if users, err := f.clinic.ListWebUsers(); err != nil || users == nil || len(users) != 0 {
		t.Fatalf("an empty list should be returned as empty, got %v, %v", users, err)
	}
	for _, result := range []string{"users-crash", "silent"} {
		t.Run(result, func(t *testing.T) {
			f := newWebPasswordFixture(t)
			t.Setenv("CARE_FAKE_RESULT", result)
			if _, err := f.clinic.ListWebUsers(); err == nil || !strings.Contains(err.Error(), "couldn't list its web users") {
				t.Fatalf("got %v", err)
			}
		})
	}
	for _, state := range []string{"stopped", "unreachable"} {
		t.Run(state, func(t *testing.T) {
			f := newWebPasswordFixture(t)
			t.Setenv("CARE_FAKE_STATE", state)
			if _, err := f.clinic.ListWebUsers(); !errors.Is(err, ErrCareNotRunning) {
				t.Fatalf("got %v, want ErrCareNotRunning", err)
			}
			if strings.Contains(f.read(t, f.args), "exec") {
				t.Fatal("listing ran while CARE was not running")
			}
		})
	}
}

func TestListWebUsersScriptIsValidPython(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not available")
	}
	cmd := exec.Command(python, "-I", "-c", "import sys; compile(sys.stdin.read(), 'users', 'exec')")
	cmd.Stdin = strings.NewReader(webUsersScript)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, want := range []string{`"deleted", False`, `"is_service_account", False`, "USERNAME_FIELD", "[:5000]"} {
		if !strings.Contains(webUsersScript, want) {
			t.Fatalf("script is missing %q", want)
		}
	}
}
