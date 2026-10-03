package clinic

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/compose"
	"github.com/ohcnetwork/care_desktop/app/internal/plugins"
)

func pluginFixture(t *testing.T) (*Clinic, func() string, []plugins.Plugin) {
	t.Helper()
	e, trace, _ := migrationFixture(t)
	e.pluginReady = func() error { return nil }
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$CARE_MIGRATION_TRACE"
case "$1 $2" in
  'image ls') printf 'fixture-image\n'; exit 0 ;;
  'image inspect') printf 'old-fingerprint\n'; exit 0 ;;
  'image tag')
    case "$4" in
      *-plugin-rollback) ;;
      *) touch recovering ;;
    esac
    exit 0 ;;
  'image rm') exit 0 ;;
  'compose ps')
    case "$*" in
      *celery-worker*) printf 'worker\n' ;;
      *) printf 'backend\ndb\n' ;;
    esac
    exit 0 ;;
  'compose stop') exit 0 ;;
  'compose up')
    if [ -f recovering ]; then
      [ "$CARE_PLUGIN_RECOVERY_FAILURE" != yes ]; exit $?
    fi
    case "$*" in
      *' db redis backend') [ "$CARE_PLUGIN_FAILURE" != backend-start ]; exit $? ;;
      *) [ "$CARE_PLUGIN_FAILURE" != worker-start ]; exit $? ;;
    esac ;;
esac
case "$1" in
  inspect)
    case "$*" in
      *'{{.State.Status}}'*) printf 'exited\n' ;;
      *) printf '%s\n' '{"ID":"old-backend","Service":"backend","Image":"sha256:old","Status":"running","StartedAt":"old"}' '{"ID":"db","Service":"db","Image":"sha256:db","Status":"running","Health":"healthy","StartedAt":"old"}' ;;
    esac
    exit 0 ;;
  build) [ "$CARE_PLUGIN_FAILURE" != build ]; exit $? ;;
esac
case "$*" in
  *'print(json.dumps('*)
    printf '%s\n' '{"slugs":["test_frontend"],"rows":[{"slug":"manual","meta":{"url":"https://example.invalid/old.js"}}]}'
    exit 0 ;;
  *' manage.py migrate --noinput') [ "$CARE_PLUGIN_FAILURE" != migration ]; exit $? ;;
  *' shell '*)
    [ -f recovering ] && exit 0
    [ "$CARE_PLUGIN_FAILURE" != sync ]; exit $? ;;
esac
exit 99
`
	if err := os.WriteFile(filepath.Join(e.InstallDir, "docker"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	list := []plugins.Plugin{{
		ID: "test", Backend: &plugins.Backend{Name: "test_plugin", PackageName: "test-plugin", Configs: map[string]any{"TOKEN": "synthetic-private-value"}},
		Frontend: &plugins.Frontend{Slug: "test_frontend", URL: "https://example.invalid/remoteEntry.js"},
	}}
	return e, trace, list
}

func TestPluginFailuresRestoreConfigurationAndImage(t *testing.T) {
	for _, failure := range []string{"build", "backend-start", "migration", "sync", "worker-start", "health"} {
		t.Run(failure, func(t *testing.T) {
			e, trace, list := pluginFixture(t)
			before, err := os.ReadFile(filepath.Join(e.InstallDir, "backend.env"))
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("CARE_PLUGIN_FAILURE", failure)
			checks := 0
			e.pluginReady = func() error {
				checks++
				if failure == "health" && checks == 2 {
					return errors.New("celery-worker restarted during readiness checks")
				}
				return nil
			}
			if err := plugins.New(e.InstallDir).StagePlugins(list); err != nil {
				t.Fatal(err)
			}
			err = e.ApplyPlugins()
			if err == nil || !strings.Contains(err.Error(), "CARE is back online") {
				t.Fatalf("failure did not recover clinic: %v", err)
			}
			after, err := os.ReadFile(filepath.Join(e.InstallDir, "backend.env"))
			if err != nil || string(after) != string(before) {
				t.Fatalf("backend environment was not restored exactly: %v", err)
			}
			for _, name := range []string{"plugins.json", "channel.lock", "plugins-pending.json", pluginRecoveryFile} {
				if _, err := os.Stat(filepath.Join(e.InstallDir, name)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("%s was not removed: %v", name, err)
				}
			}
			calls := trace()
			pin := strings.Index(calls, "image tag sha256:old fixture-backend-plugin-rollback")
			build := strings.Index(calls, "build ")
			restore := strings.Index(calls, "image tag sha256:old fixture-backend\n")
			if pin < 0 || build < pin || restore < build {
				t.Fatal("previous image was not pinned before building and restored after failure")
			}
			if !strings.Contains(calls[restore:], "--no-build --pull never") ||
				strings.Contains(calls[restore:], " manage.py migrate ") ||
				strings.Contains(calls, "down") || strings.Contains(calls, "--volumes") {
				t.Fatal("recovery must reuse the saved image without rebuilding, migrating or deleting data")
			}
			if strings.Contains(calls, "synthetic-private-value") && !strings.Contains(calls, "--build-arg") {
				t.Fatal("frontend registration snapshot exposed private values in command arguments")
			}
		})
	}
}

func TestPluginApplyCommitsOnlyAfterHealthy(t *testing.T) {
	e, _, list := pluginFixture(t)
	if err := compose.WriteLock(e.InstallDir, compose.Lock{Backend: compose.Channel{Next: strings.Repeat("a", 40)}}); err != nil {
		t.Fatal(err)
	}
	if err := plugins.New(e.InstallDir).StagePlugins(list); err != nil {
		t.Fatal(err)
	}
	checks := 0
	e.pluginReady = func() error {
		checks++
		if checks == 2 {
			if pending, err := e.PendingPluginRecovery(); err != nil || !pending {
				t.Fatalf("journal disappeared before clinic health was verified: %v", err)
			}
		}
		return nil
	}
	if err := e.ApplyPlugins(); err != nil {
		t.Fatal(err)
	}
	got, err := plugins.New(e.InstallDir).ReadPlugins()
	if err != nil || !reflect.DeepEqual(got, list) {
		t.Fatalf("healthy plugin configuration not committed: %+v, %v", got, err)
	}
	if checks != 2 {
		t.Fatalf("expected preflight and post-start health checks, got %d", checks)
	}
	if pending, err := e.PendingPluginRecovery(); err != nil || pending {
		t.Fatalf("successful transaction still pending: %v", err)
	}
	if compose.ReadLock(e.InstallDir).Backend.Next != "" {
		t.Fatal("queued backend update still uses the previous plugin inputs")
	}
}

func TestPluginRecoveryFailureRemainsRetryableOnStart(t *testing.T) {
	e, trace, list := pluginFixture(t)
	t.Setenv("CARE_PLUGIN_FAILURE", "build")
	t.Setenv("CARE_PLUGIN_RECOVERY_FAILURE", "yes")
	if err := plugins.New(e.InstallDir).StagePlugins(list); err != nil {
		t.Fatal(err)
	}
	err := e.ApplyPlugins()
	if err == nil || !strings.Contains(err.Error(), "CARE could not be recovered") || strings.Contains(err.Error(), "back online") {
		t.Fatalf("recovery failure was not explicit: %v", err)
	}
	if pending, err := e.PendingPluginRecovery(); err != nil || !pending {
		t.Fatalf("recovery journal was lost: %v", err)
	}
	if err := e.ApplyPlugins(); err == nil || !strings.Contains(err.Error(), "rollback is unfinished") {
		t.Fatalf("new transaction overwrote recovery: %v", err)
	}
	t.Setenv("CARE_PLUGIN_RECOVERY_FAILURE", "")
	if err := e.Start(); err != nil {
		t.Fatalf("Start did not recover: %v", err)
	}
	if pending, err := e.PendingPluginRecovery(); err != nil || pending {
		t.Fatalf("recovery journal not cleared: %v", err)
	}
	if strings.Count(trace(), "\nbuild ") != 1 {
		t.Fatal("Start rebuilt the failing plugin instead of restoring the pinned image")
	}
}

func TestInterruptedPluginChangeRestoresExistingFiles(t *testing.T) {
	e, _, list := pluginFixture(t)
	if err := compose.WriteLock(e.InstallDir, compose.Lock{Backend: compose.Channel{Next: strings.Repeat("b", 40)}}); err != nil {
		t.Fatal(err)
	}
	old := []plugins.Plugin{{ID: "previous", Frontend: &plugins.Frontend{Slug: "previous", URL: "https://example.invalid/old.js"}}}
	if err := plugins.New(e.InstallDir).SavePlugins(old); err != nil {
		t.Fatal(err)
	}
	state, err := e.preparePluginRecovery(list)
	if err != nil {
		t.Fatal(err)
	}
	if err := plugins.New(e.InstallDir).SavePlugins(list); err != nil {
		t.Fatal(err)
	}
	// A fresh engine must find the on-disk journal, not depend on in-memory state.
	e.pluginBaseline = nil
	if err := e.Start(); err != nil {
		t.Fatal(err)
	}
	for name, saved := range state.Files {
		got, err := os.ReadFile(filepath.Join(e.InstallDir, name))
		if saved.Exists && (err != nil || string(got) != string(saved.Data)) {
			t.Fatalf("%s did not recover exactly: %v", name, err)
		}
	}
}

func TestPluginPreflightDoesNotTouchActiveConfiguration(t *testing.T) {
	e, trace, list := pluginFixture(t)
	e.pluginReady = func() error { return errors.New("existing clinic is unhealthy") }
	if err := plugins.New(e.InstallDir).StagePlugins(list); err != nil {
		t.Fatal(err)
	}
	if err := e.ApplyPlugins(); err == nil || !strings.Contains(err.Error(), "were not applied") {
		t.Fatalf("unhealthy baseline accepted: %v", err)
	}
	if strings.Contains(trace(), "build ") || strings.Contains(trace(), "compose up") {
		t.Fatal("preflight failure changed the runtime")
	}
	if pending, err := e.PendingPluginRecovery(); err != nil || pending {
		t.Fatalf("preflight failure left a transaction: %v", err)
	}
}

func TestPluginRecoveryHasIndependentDeadline(t *testing.T) {
	e, _, list := pluginFixture(t)
	if err := plugins.New(e.InstallDir).StagePlugins(list); err != nil {
		t.Fatal(err)
	}
	err := e.applyPlugins(func() error {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		cancel()
		e.ctx = ctx
		return context.DeadlineExceeded
	})
	if err == nil || !strings.Contains(err.Error(), "CARE is back online") {
		t.Fatalf("expired apply deadline prevented recovery: %v", err)
	}
}

func TestPluginPartialConfigurationWriteRollsBack(t *testing.T) {
	e, _, list := pluginFixture(t)
	before, err := os.ReadFile(filepath.Join(e.InstallDir, "backend.env"))
	if err != nil {
		t.Fatal(err)
	}
	checks := 0
	e.pluginReady = func() error {
		checks++
		if checks == 1 {
			// Fail the second configuration write after backend.env was written.
			return os.Mkdir(filepath.Join(e.InstallDir, "plugins.json"), 0o700)
		}
		return nil
	}
	if err := plugins.New(e.InstallDir).StagePlugins(list); err != nil {
		t.Fatal(err)
	}
	if err := e.ApplyPlugins(); err == nil || !strings.Contains(err.Error(), "CARE is back online") {
		t.Fatalf("partial configuration write was not recovered: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(e.InstallDir, "backend.env"))
	if err != nil || string(before) != string(after) {
		t.Fatalf("partial backend environment write remained active: %v", err)
	}
}
