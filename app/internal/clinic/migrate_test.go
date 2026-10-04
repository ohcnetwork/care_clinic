package clinic

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/release"
)

func migrationFixture(t *testing.T) (*Clinic, func() string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX command fixture")
	}
	var id [8]byte
	if _, err := rand.Read(id[:]); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs(".migration-test-" + hex.EncodeToString(id[:]))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "src", "backend"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	trace, credentials := filepath.Join(root, "calls"), filepath.Join(root, "credentials")
	t.Setenv("CARE_MIGRATION_TRACE", trace)
	t.Setenv("CARE_MIGRATION_CREDENTIALS", credentials)
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	const fixtureRef = "0123456789abcdef0123456789abcdef01234567"
	hash := func(data string) string {
		sum := sha256.Sum256([]byte(data))
		return hex.EncodeToString(sum[:])[:12]
	}
	pins := &release.Pins{
		AppVersion: "fixture", BeRef: fixtureRef, BeRepo: "fixture-repo", FeRef: fixtureRef, FeRepo: "fixture-repo",
		BackendImage: "fixture-backend", FrontendImage: "fixture-frontend", BackupImage: "fixture-backup",
		CaddyWafImage: "fixture-caddy", CaddyImage: "fixture-caddy-base", PostgresImage: "fixture-postgres", CorazaVersion: "fixture",
	}
	sourceKey := "fixture+" + fixtureRef + "+repo@" + hash("fixture-repo")
	t.Setenv("CARE_BACKEND_FINGERPRINT", sourceKey)
	t.Setenv("CARE_FRONTEND_FINGERPRINT", sourceKey+"+env@"+hash(""))
	t.Setenv("CARE_BACKUP_FINGERPRINT", pins.PostgresImage+"+dockerfile@"+hash("synthetic-backup"))
	t.Setenv("CARE_CADDY_FINGERPRINT", pins.CaddyImage+"+coraza@fixture+dockerfile@"+hash("synthetic-caddy"))
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$CARE_MIGRATION_TRACE"
case "$1 $2" in
  'image ls') printf 'fixture-image\n'; exit 0 ;;
  'image inspect')
    for last do :; done
    case "$last" in
      fixture-backend) printf '%s\n' "$CARE_BACKEND_FINGERPRINT" ;;
      fixture-frontend) printf '%s\n' "$CARE_FRONTEND_FINGERPRINT" ;;
      fixture-backup) printf '%s\n' "$CARE_BACKUP_FINGERPRINT" ;;
      fixture-caddy) printf '%s\n' "$CARE_CADDY_FINGERPRINT" ;;
      *) exit 99 ;;
    esac
    exit 0 ;;
  'compose stop') [ "$CARE_MIGRATION_FAILURE" != stop ]; exit $? ;;
  'compose ps')
    case "$*" in
      *--services*) printf 'caddy\n' ;;
      *) [ "$CARE_MIGRATION_FAILURE" != inspection ] || exit 1; printf 'fixture-worker\n' ;;
    esac
    exit 0 ;;
  'compose up') exit 0 ;;
  'compose run')
    printf '%s\n%s\n' "$POSTGRES_DB" "$DATABASE_URL" > "$CARE_MIGRATION_CREDENTIALS"
    exit 0 ;;
esac
case "$1" in
  inspect)
    if [ "$CARE_MIGRATION_FAILURE" = running ]; then printf 'running\n'; else printf 'exited\n'; fi
    exit 0 ;;
  build) exit 0 ;;
esac
case "$*" in
  *' manage.py migrate --noinput') [ "$CARE_MIGRATION_FAILURE" != migration ]; exit $? ;;
  *' manage.py createsuperuser '*)
    printf '%s\n' "$DJANGO_SUPERUSER_PASSWORD" > "$CARE_MIGRATION_CREDENTIALS"
    case "$CARE_MIGRATION_FAILURE" in
      admin) printf 'CommandError: cannot connect to database\n'; exit 1 ;;
      existing) printf 'CommandError: Error: That username is already taken.\n'; exit 1 ;;
      misleading) printf 'ConnectionError: address already taken\n'; exit 1 ;;
      *) exit 0 ;;
    esac ;;
esac
exit 99
`
	for path, data := range map[string]string{
		"docker":                   script,
		"backend.env":              "POSTGRES_PASSWORD='SyntheticDatabaseSecret'\nDATABASE_URL='postgres://fixture:SyntheticURLSecret@db:5432/care?sslmode=disable'\n",
		"frontend.env":             "",
		"backup.Dockerfile":        "synthetic-backup",
		"caddy.Dockerfile":         "synthetic-caddy",
		"src/backend/.care-source": sourceKey,
	} {
		mode := os.FileMode(0o600)
		if path == "docker" {
			mode = 0o700
		}
		if err := os.WriteFile(filepath.Join(root, path), []byte(data), mode); err != nil {
			t.Fatal(err)
		}
	}
	e := &Clinic{InstallDir: root, BackupDir: filepath.Join(root, "backups"), Pins: pins, AdminPassword: "SyntheticAdminSecret123"}
	readTrace := func() string {
		t.Helper()
		data, err := os.ReadFile(trace)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	return e, readTrace, credentials
}

func TestStartStopsAndChecksWorkersBeforeMigrations(t *testing.T) {
	for _, failure := range []string{"stop", "inspection", "running", "migration", "admin"} {
		t.Run(failure, func(t *testing.T) {
			e, trace, _ := migrationFixture(t)
			t.Setenv("CARE_MIGRATION_FAILURE", failure)
			if err := e.Start(); err == nil {
				t.Fatal("startup reported success after a worker, migration or administrator failure")
			}
			calls := trace()
			stop := strings.Index(calls, "compose stop celery-worker celery-beat")
			migrate := strings.Index(calls, "manage.py migrate")
			if stop < 0 {
				t.Fatal("startup never attempted to pause existing workers")
			}
			if failure == "stop" || failure == "inspection" || failure == "running" {
				if migrate >= 0 {
					t.Fatal("migration ran without a verified worker stop")
				}
			} else if migrate < stop {
				t.Fatal("migration ran before workers stopped")
			}
			if strings.Contains(calls, "compose up -d --wait --wait-timeout 300\n") {
				t.Fatal("startup resumed the stack after a safety failure")
			}
		})
	}
}

func TestRebuildPausesWorkersUntilMigrationsSucceed(t *testing.T) {
	for _, failure := range []string{"stop", "migration", ""} {
		t.Run(failure, func(t *testing.T) {
			e, trace, _ := migrationFixture(t)
			t.Setenv("CARE_MIGRATION_FAILURE", failure)
			err := e.RebuildBackend()
			if (err == nil) != (failure == "") {
				t.Fatalf("unexpected rebuild result: %v", err)
			}
			calls := trace()
			stop := strings.Index(calls, "compose stop celery-worker celery-beat")
			migrate := strings.Index(calls, "manage.py migrate")
			restart := strings.Index(calls, "compose up -d --wait --wait-timeout 300 celery-worker celery-beat")
			if stop < 0 || (migrate >= 0 && migrate < stop) {
				t.Fatal("rebuild migrated before stopping workers")
			}
			if failure != "" && restart >= 0 {
				t.Fatal("rebuild restarted workers after failure")
			}
			if failure == "" && restart < migrate {
				t.Fatal("rebuild restarted workers before migration completed")
			}
		})
	}
}

func TestCreateAdminErrorsAndEnvironment(t *testing.T) {
	for _, outcome := range []string{"", "existing", "admin", "misleading"} {
		t.Run(outcome, func(t *testing.T) {
			e, trace, credentials := migrationFixture(t)
			t.Setenv("CARE_MIGRATION_FAILURE", outcome)
			err := e.createAdmin()
			if (err == nil) != (outcome == "" || outcome == "existing") {
				t.Fatalf("unexpected administrator result: %v", err)
			}
			if strings.Contains(trace(), e.AdminPassword) || !strings.Contains(trace(), "-e DJANGO_SUPERUSER_PASSWORD backend") {
				t.Fatal("administrator password was not passed only through the child environment")
			}
			data, err := os.ReadFile(credentials)
			if err != nil || strings.TrimSpace(string(data)) != e.AdminPassword {
				t.Fatal("administrator password did not reach the child environment")
			}
		})
	}
}

func TestStagedMigrationOverridesDatabaseWithoutCredentialArguments(t *testing.T) {
	e, trace, credentials := migrationFixture(t)
	id := strings.Repeat("a", 24)
	database := "care_restore_" + id
	if err := e.migrateStaged(database, id); err != nil {
		t.Fatal(err)
	}
	calls := trace()
	for _, want := range []string{
		"compose run --rm --no-deps", "--entrypoint python", "-e POSTGRES_DB -e DATABASE_URL", "org.care-clinic.restore=" + id,
	} {
		if !strings.Contains(calls, want) {
			t.Fatalf("staged migration omitted %q: %s", want, calls)
		}
	}
	if strings.Contains(calls, "SyntheticURLSecret") || strings.Contains(calls, "SyntheticDatabaseSecret") {
		t.Fatal("staged migration exposed a database credential in its arguments")
	}
	data, err := os.ReadFile(credentials)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	u, err := url.Parse(lines[1])
	if err != nil || lines[0] != database || u.Path != "/"+database || u.Query().Get("sslmode") != "disable" {
		t.Fatalf("staged connection settings were not preserved and redirected: %s", data)
	}
}

func TestStagedDatabaseURLHandlesEscapedCredentials(t *testing.T) {
	for _, env := range []map[string]string{
		{"DATABASE_URL": "postgresql://user:p%40ss%2Fword@db:5433/old%20db?sslmode=require&dbname=wrong"},
		{"POSTGRES_USER": "user", "POSTGRES_PASSWORD": "p@ss/word", "POSTGRES_HOST": "::1", "POSTGRES_PORT": "5433"},
	} {
		value, err := stagedDatabaseURL(env, "care_restore_fixture")
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(value)
		if err != nil || u.Path != "/care_restore_fixture" || u.Query().Has("dbname") {
			t.Fatalf("incorrect staged database URL: %s, %v", value, err)
		}
		if password, ok := u.User.Password(); !ok || password != "p@ss/word" {
			t.Fatal("staged migration changed an escaped credential")
		}
	}
}

func TestRebuildAllBuildsBothImagesBeforeRestartingEverything(t *testing.T) {
	e, trace, _ := migrationFixture(t)
	stamp, err := os.ReadFile(filepath.Join(e.InstallDir, "src", "backend", ".care-source"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(e.InstallDir, "src", "frontend"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.InstallDir, "src", "frontend", ".care-source"), stamp, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CARE_MIGRATION_FAILURE", "migration")
	if err := e.RebuildAll(); err == nil {
		t.Fatal("rebuild reported success after a migration failure")
	}
	calls := trace()
	backend := strings.Index(calls, "build -f "+filepath.Join(e.InstallDir, "src", "backend", "docker", "prod.Dockerfile"))
	frontend := strings.Index(calls, "build -t fixture-frontend")
	stop := strings.Index(calls, "compose stop\n")
	migrate := strings.Index(calls, "manage.py migrate")
	if backend < 0 || frontend < 0 {
		t.Fatalf("rebuild did not build both images:\n%s", calls)
	}
	if stop < 0 || stop < backend || stop < frontend {
		t.Fatalf("rebuild stopped CARE before the images were built:\n%s", calls)
	}
	if migrate < stop {
		t.Fatalf("rebuild migrated before restarting:\n%s", calls)
	}
	if strings.Contains(calls, "compose up -d --wait --wait-timeout 300\n") {
		t.Fatal("rebuild resumed the stack after a migration failure")
	}
}
