package clinic

import (
	"strings"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/compose"
)

const (
	// stopStackCall is the gate's own "docker compose stop", unlike the one that
	// also names the celery services.
	stopStackCall    = "compose stop"
	startBackendCall = "compose up -d --wait --wait-timeout 300 db redis backend"
	startAllCall     = "compose up -d --wait --wait-timeout 300"
	migrateCall      = "compose exec -T backend python manage.py migrate"
)

func requireChannel(t *testing.T, name string, got compose.Channel, current, next string) {
	t.Helper()
	if got.Current != current || got.Next != next {
		t.Fatalf("%s is at %q with %q offered, want %q and %q", name, got.Current, got.Next, current, next)
	}
}

func requireInOrder(t *testing.T, calls callLog, steps ...[2]string) {
	t.Helper()
	previous, previousName := -1, ""
	for _, step := range steps {
		name, call := step[0], step[1]
		at := calls.first(call)
		if exact, ok := strings.CutPrefix(call, "="); ok {
			at = calls.exact(exact)
		}
		if at < 0 {
			t.Fatalf("%s never ran:\n%s", name, strings.Join(calls, "\n"))
		}
		if at <= previous {
			t.Fatalf("%s ran before %s:\n%s", name, previousName, strings.Join(calls, "\n"))
		}
		previous, previousName = at, name
	}
}

func TestApplyUpdateStopsCAREBeforeAnythingIsBuiltBackedUpOrMigrated(t *testing.T) {
	f := newLifecycle(t)
	f.encryptBackups()
	f.offer(newCommit, newCommit)

	if err := f.e.ApplyUpdate(); err != nil {
		t.Fatal(err)
	}

	calls := f.calls()
	if calls.exact(stopStackCall) != 0 {
		t.Fatalf("CARE was not stopped before anything else happened:\n%s", strings.Join(calls, "\n"))
	}
	requireInOrder(t, calls,
		[2]string{"stop", "=" + stopStackCall},
		[2]string{"backend build", "build -f "},
		[2]string{"frontend build", "build -t fixture-frontend-next "},
		[2]string{"safety backup", "compose run --rm -T backup once pre-update-"},
		[2]string{"backend promotion", "image tag fixture-backend-next fixture-backend"},
		[2]string{"frontend promotion", "image tag fixture-frontend-next fixture-frontend"},
		[2]string{"backend start", "=" + startBackendCall},
		[2]string{"migration", migrateCall},
		[2]string{"full start", "=" + startAllCall},
	)
	if builds := calls.count("build "); builds != 2 {
		t.Fatalf("expected the two staged builds, got %d:\n%s", builds, strings.Join(calls, "\n"))
	}
	for _, call := range calls {
		if strings.HasPrefix(call, "build ") && !strings.Contains(call, "-next --label") {
			t.Fatalf("the running image was rebuilt in place: %s", call)
		}
	}
	lock := f.lock()
	requireChannel(t, "backend", lock.Backend, newCommit, "")
	requireChannel(t, "frontend", lock.Frontend, newCommit, "")
	if label, _ := f.imageLabel("fixture-backend"); label != backendKey(newCommit) {
		t.Fatalf("the backend image was not promoted: %q", label)
	}
	if label, _ := f.imageLabel("fixture-frontend"); label != frontendKey(newCommit) {
		t.Fatalf("the frontend image was not promoted: %q", label)
	}
	if running := f.running(); len(running) != 4 {
		t.Fatalf("CARE is not fully running after the update: %v", running)
	}
	if !strings.Contains(f.logs.text(), "CARE is up to date.") {
		t.Fatal("the update did not report success")
	}
	assertDataKept(t, calls, false)
}

func TestApplyUpdateOfOnlyTheFrontendLeavesTheBackendAndItsBackupsAlone(t *testing.T) {
	f := newLifecycle(t)
	f.offer("", newCommit)

	if err := f.e.ApplyUpdate(); err != nil {
		t.Fatal(err)
	}

	calls := f.calls()
	requireInOrder(t, calls,
		[2]string{"stop", "=" + stopStackCall},
		[2]string{"frontend build", "build -t fixture-frontend-next "},
		[2]string{"frontend promotion", "image tag fixture-frontend-next fixture-frontend"},
		[2]string{"full start", "=" + startAllCall},
	)
	if calls.count("build ") != 1 || calls.count("compose run") != 0 || calls.count("image tag") != 1 {
		t.Fatalf("a frontend-only update touched the backend:\n%s", strings.Join(calls, "\n"))
	}
	lock := f.lock()
	requireChannel(t, "backend", lock.Backend, oldCommit, "")
	requireChannel(t, "frontend", lock.Frontend, newCommit, "")
	if label, _ := f.imageLabel("fixture-backend"); label != backendKey(oldCommit) {
		t.Fatalf("the backend image changed: %q", label)
	}
}

func TestApplyUpdateWithoutAnOfferTouchesNothing(t *testing.T) {
	f := newLifecycle(t)
	err := f.e.ApplyUpdate()
	if err == nil || !strings.Contains(err.Error(), "no update to install") {
		t.Fatalf("an update that was never offered was installed: %v", err)
	}
	if calls := f.calls(); len(calls) != 0 {
		t.Fatalf("docker was used without an update:\n%s", strings.Join(calls, "\n"))
	}
}

func TestApplyUpdateOfTheBackendNeedsEncryptedBackupsAndLeavesCAREUp(t *testing.T) {
	f := newLifecycle(t)
	f.offer(newCommit, "")

	err := f.e.ApplyUpdate()
	if err == nil || !strings.Contains(err.Error(), "cannot write encrypted backups") {
		t.Fatalf("a backend update went ahead without encrypted backups: %v", err)
	}
	if calls := f.calls(); len(calls) != 0 {
		t.Fatalf("CARE was stopped for an update that could not be installed:\n%s", strings.Join(calls, "\n"))
	}
	if running := f.running(); len(running) != 4 {
		t.Fatalf("CARE is no longer running: %v", running)
	}
}

func TestApplyUpdateChangesNothingWhenCAREWillNotStop(t *testing.T) {
	f := newLifecycle(t)
	f.encryptBackups()
	f.offer(newCommit, newCommit)
	f.flag("stubborn-c-backend")
	f.fail("compose-stop")

	err := f.e.ApplyUpdate()
	if err == nil || !strings.Contains(err.Error(), "did not stop completely") {
		t.Fatalf("the update went ahead while CARE was running: %v", err)
	}
	if strings.Contains(err.Error(), "running as before") {
		t.Fatalf("the error claims CARE was restarted: %v", err)
	}
	calls := f.calls()
	for _, prefix := range []string{"build ", "compose run", "image tag", "compose up", "compose down"} {
		if n := calls.count(prefix); n != 0 {
			t.Fatalf("%d %q command(s) ran against a stack that was still running:\n%s", n, prefix, strings.Join(calls, "\n"))
		}
	}
	lock := f.lock()
	requireChannel(t, "backend", lock.Backend, oldCommit, newCommit)
	requireChannel(t, "frontend", lock.Frontend, oldCommit, newCommit)
}

func TestApplyUpdateStartsThePreviousVersionWhenABuildFails(t *testing.T) {
	f := newLifecycle(t)
	f.encryptBackups()
	f.offer(newCommit, newCommit)
	f.fail("build-next")

	err := f.e.ApplyUpdate()
	if err == nil || !strings.Contains(err.Error(), "the update was not installed and CARE is running as before") {
		t.Fatalf("a failed build was not reported with the restart: %v", err)
	}
	calls := f.calls()
	requireInOrder(t, calls,
		[2]string{"stop", "=" + stopStackCall},
		[2]string{"failed build", "build "},
		[2]string{"restart", "=" + startBackendCall},
		[2]string{"full restart", "=" + startAllCall},
	)
	if calls.count("image tag") != 0 || calls.count("compose run") != 0 {
		t.Fatalf("the update was promoted or the database was backed up after a failed build:\n%s", strings.Join(calls, "\n"))
	}
	lock := f.lock()
	requireChannel(t, "backend", lock.Backend, oldCommit, newCommit)
	requireChannel(t, "frontend", lock.Frontend, oldCommit, newCommit)
	if label, _ := f.imageLabel("fixture-backend"); label != backendKey(oldCommit) {
		t.Fatalf("the running backend image changed: %q", label)
	}
	if running := f.running(); len(running) != 4 {
		t.Fatalf("CARE was not started again on its previous version: %v", running)
	}
	assertDataKept(t, calls, false)
}

func TestApplyUpdateStartsThePreviousVersionWhenTheSafetyBackupFails(t *testing.T) {
	f := newLifecycle(t)
	f.encryptBackups()
	f.offer(newCommit, newCommit)
	f.fail("backup")

	err := f.e.ApplyUpdate()
	if err == nil || !strings.Contains(err.Error(), "couldn't take a safety backup") ||
		!strings.Contains(err.Error(), "CARE is running as before") {
		t.Fatalf("a failed safety backup was not reported with the restart: %v", err)
	}
	calls := f.calls()
	if calls.count("build ") != 2 || calls.count("image tag") != 0 {
		t.Fatalf("the update was promoted without a safety backup:\n%s", strings.Join(calls, "\n"))
	}
	lock := f.lock()
	requireChannel(t, "backend", lock.Backend, oldCommit, newCommit)
	requireChannel(t, "frontend", lock.Frontend, oldCommit, newCommit)
	if label, _ := f.imageLabel("fixture-backend"); label != backendKey(oldCommit) {
		t.Fatalf("the running backend image changed: %q", label)
	}
	if running := f.running(); len(running) != 4 {
		t.Fatalf("CARE was not started again on its previous version: %v", running)
	}
}

func TestApplyUpdateKeepsTheNewVersionWhenItsMigrationFails(t *testing.T) {
	f := newLifecycle(t)
	f.encryptBackups()
	f.offer(newCommit, newCommit)
	f.fail("migrate")

	err := f.e.ApplyUpdate()
	if err == nil || !strings.Contains(err.Error(), "the update was installed, but CARE did not come back up") ||
		!strings.Contains(err.Error(), "database migrations failed") {
		t.Fatalf("a failed migration was not reported: %v", err)
	}
	calls := f.calls()
	requireInOrder(t, calls,
		[2]string{"safety backup", "compose run --rm -T backup once pre-update-"},
		[2]string{"promotion", "image tag fixture-backend-next fixture-backend"},
		[2]string{"migration", migrateCall},
	)
	if calls.count("image tag") != 2 || calls.count(startBackendCall) != 1 || calls.exact(startAllCall) >= 0 {
		t.Fatalf("a database that began migrating was started again or rolled back:\n%s", strings.Join(calls, "\n"))
	}
	lock := f.lock()
	requireChannel(t, "backend", lock.Backend, newCommit, "")
	requireChannel(t, "frontend", lock.Frontend, newCommit, "")
	if running := f.status("c-caddy"); running != "exited" {
		t.Fatalf("the proxy was started before the migration succeeded: %s", running)
	}
	assertDataKept(t, calls, false)
}
