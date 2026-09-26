package clinic

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

const backupScriptCommands = `#!/bin/sh
set -eu
name=${0##*/}
printf '%s %s\n' "$BACKUP_TEST_ID" "$name" >> "$BACKUP_TEST_ROOT/trace"
wait_for() {
	count=0
	while [ ! -f "$1" ]; do
		count=$((count + 1))
		[ "$count" -lt 500 ] || exit 98
		"$BACKUP_TEST_SLEEP" 0.02
	done
}
case "$name" in
	flock)
		[ "$BACKUP_TEST_FAIL" != lock ] || exit 1
		if [ -n "$BACKUP_TEST_FLOCK" ]; then
			exec "$BACKUP_TEST_FLOCK" "$@"
		fi
		;;
	date)
		printf '20260102-030405\n'
		;;
	pg_dump)
		while [ "$1" != -f ]; do shift; done
		printf 'database\n' > "$2"
		if [ "$BACKUP_TEST_ID" = "$BACKUP_TEST_HOLD" ]; then
			: > "$BACKUP_TEST_ROOT/holding"
			wait_for "$BACKUP_TEST_ROOT/release"
		fi
		;;
	pg_restore)
		;;
	tar)
		if [ "$1" = -czf ]; then
			printf 'readable but possibly incomplete archive\n' > "$2"
			[ "$BACKUP_TEST_FAIL" != tar ] || exit 1
		fi
		;;
	openssl)
		while [ "$#" -gt 0 ]; do
			case "$1" in
				-in) shift; input=$1 ;;
				-out) shift; output=$1 ;;
			esac
			shift
		done
		cat "$input" > "$output"
		;;
	mv)
		case "$BACKUP_TEST_FAIL:${2##*/}" in
			db-mv:care-*|files-mv:files-*) exit 1 ;;
		esac
		exec "$BACKUP_TEST_MV" "$@"
		;;
	rm)
		for arg in "$@"; do
			case "$BACKUP_TEST_FAIL:${arg##*/}" in
				db-rm:.care-*.dump.tmp|files-rm:.files-*.tar.gz.tmp) exit 1 ;;
			esac
		done
		exec "$BACKUP_TEST_RM" "$@"
		;;
	find)
		[ "$1" = "$BACKUP_TEST_ROOT/backups" ] || exit 95
		kind=temp
		partial=.care-stale.dump.tmp
		for arg in "$@"; do
			if [ "$arg" = -mtime ]; then
				kind=retention
				partial=care-old.dump.enc
			fi
		done
		if [ "$BACKUP_TEST_FAIL" = "$kind-prune" ]; then
			"$BACKUP_TEST_RM" -f "$BACKUP_TEST_ROOT/backups/$partial"
			exit 1
		fi
		exec "$BACKUP_TEST_FIND" "$@"
		;;
	df)
		if [ -n "$BACKUP_TEST_FREE_KB" ]; then
			printf 'Filesystem 1024-blocks Used Available Capacity Mounted on\nfixture 99999999 0 %s 1%% /backups\n' "$BACKUP_TEST_FREE_KB"
			exit 0
		fi
		exec "$BACKUP_TEST_DF" "$@"
		;;
	sleep)
		[ "$1" = 86400 ] || exit 97
		: > "$BACKUP_TEST_ROOT/sleeping"
		if [ "$BACKUP_TEST_HOLD_SLEEP" = 1 ]; then
			wait_for "$BACKUP_TEST_ROOT/wake"
		fi
		exit 99
		;;
	*) exit 96 ;;
esac
`

type backupScriptFixture struct {
	root  string
	env   []string
	flock string
}

func newBackupScriptFixture(t *testing.T) *backupScriptFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX shell")
	}
	script, err := os.ReadFile("../../../deployments/scripts/backup.sh")
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(".", ".backup-script-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	root, err = filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"bin", "backups", "minio-data", "state"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for old, replacement := range map[string]string{
		"BACKUP_DIR=/backups":        `BACKUP_DIR="$BACKUP_TEST_ROOT/backups"`,
		"MINIO_DIR=/minio-data":      `MINIO_DIR="$BACKUP_TEST_ROOT/minio-data"`,
		"CERT=/keys/backup-cert.pem": `CERT="$BACKUP_TEST_ROOT/cert"`,
		"STATE_DIR=/state":           `STATE_DIR="$BACKUP_TEST_ROOT/state"`,
	} {
		if strings.Count(string(script), old+"\n") != 1 {
			t.Fatalf("cannot isolate backup script assignment %q", old)
		}
		script = bytes.Replace(script, []byte(old+"\n"), []byte(replacement+"\n"), 1)
	}
	for name, data := range map[string][]byte{"backup.sh": script, "cert": []byte("fixture")} {
		if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"flock", "date", "pg_dump", "pg_restore", "tar", "openssl", "mv", "rm", "find", "sleep", "df"} {
		if err := os.WriteFile(filepath.Join(root, "bin", name), []byte(backupScriptCommands), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	mv, err := exec.LookPath("mv")
	if err != nil {
		t.Fatal(err)
	}
	rm, err := exec.LookPath("rm")
	if err != nil {
		t.Fatal(err)
	}
	find, err := exec.LookPath("find")
	if err != nil {
		t.Fatal(err)
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	df, err := exec.LookPath("df")
	if err != nil {
		t.Fatal(err)
	}
	flock, _ := exec.LookPath("flock")
	return &backupScriptFixture{
		root:  root,
		flock: flock,
		env: append(os.Environ(),
			"PATH="+filepath.Join(root, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
			"POSTGRES_PASSWORD=fixture",
			"DB_BACKUP_RETENTION_PERIOD=1",
			"BACKUP_TEST_ROOT="+root,
			"BACKUP_TEST_MV="+mv,
			"BACKUP_TEST_RM="+rm,
			"BACKUP_TEST_FIND="+find,
			"BACKUP_TEST_SLEEP="+sleep,
			"BACKUP_TEST_DF="+df,
			"BACKUP_TEST_FREE_KB=",
			"BACKUP_TEST_FLOCK="+flock,
			"BACKUP_TEST_FAIL=",
			"BACKUP_TEST_HOLD=",
			"BACKUP_TEST_HOLD_SLEEP=0",
		),
	}
}

type backupScriptRun struct {
	done   chan struct{}
	output bytes.Buffer
	err    error
}

func (f *backupScriptFixture) start(t *testing.T, id string, args []string, env ...string) *backupScriptRun {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	cmd := exec.CommandContext(ctx, "/bin/sh", append([]string{filepath.Join(f.root, "backup.sh")}, args...)...)
	cmd.Env = append(append(append([]string{}, f.env...), "BACKUP_TEST_ID="+id), env...)
	cmd.WaitDelay = time.Second
	run := &backupScriptRun{done: make(chan struct{})}
	cmd.Stdout = &run.output
	cmd.Stderr = &run.output
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	go func() {
		run.err = cmd.Wait()
		close(run.done)
	}()
	t.Cleanup(func() {
		f.write(t, "release")
		f.write(t, "wake")
		cancel()
		<-run.done
	})
	return run
}

func (r *backupScriptRun) wait(t *testing.T, code int) string {
	t.Helper()
	// CommandContext and WaitDelay bound this wait, including inherited output pipes.
	<-r.done
	got := 0
	if r.err != nil {
		var exitErr *exec.ExitError
		if !errors.As(r.err, &exitErr) {
			t.Fatalf("backup script failed: %v\n%s", r.err, &r.output)
		}
		got = exitErr.ExitCode()
	}
	if got != code {
		t.Fatalf("backup script exit = %d, want %d\n%s", got, code, &r.output)
	}
	return r.output.String()
}

func (f *backupScriptFixture) write(t *testing.T, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.root, name), []byte("fixture"), 0o600); err != nil {
		t.Error(err)
	}
}

func (f *backupScriptFixture) trace(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.root, "trace"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func (f *backupScriptFixture) waitFor(t *testing.T, name, content string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(filepath.Join(f.root, name))
		if err == nil && strings.Contains(string(data), content) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("backup script never reached %s %q", name, content)
}

func (f *backupScriptFixture) checkFile(t *testing.T, name string, want bool) {
	t.Helper()
	_, err := os.Stat(filepath.Join(f.root, "backups", name))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if (err == nil) != want {
		t.Fatalf("backup file %s exists = %v, want %v", name, err == nil, want)
	}
}

func TestBackupScriptScheduledFailures(t *testing.T) {
	for _, tc := range []struct {
		name, failure, message string
		database, files        bool
		temporary              string
	}{
		{"database publication", "db-mv", "publishing the dump failed", false, false, ""},
		{"database plaintext cleanup", "db-rm", "removing temporary backup files failed", false, false, ".care-20260102-030405.dump.tmp"},
		{"archive creation", "tar", "archiving the files failed", true, false, ""},
		{"files publication", "files-mv", "publishing the files archive failed", true, false, ""},
		{"files plaintext cleanup", "files-rm", "removing temporary backup files failed", true, false, ".files-20260102-030405.tar.gz.tmp"},
		{"lock acquisition", "lock", "acquiring the backup lock failed", false, false, ""},
		{"complete set", "", "SUCCESS", true, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBackupScriptFixture(t)
			f.write(t, "backups/care-old.dump.enc")
			output := f.start(t, "daily", nil, "BACKUP_TEST_FAIL="+tc.failure).wait(t, 99)
			if !strings.Contains(output, tc.message) {
				t.Fatalf("missing %q in output:\n%s", tc.message, output)
			}
			complete := tc.failure == ""
			if strings.Contains(f.trace(t), "daily find\n") != complete {
				t.Fatalf("retention did not match backup success:\n%s", f.trace(t))
			}
			if !complete && (!strings.Contains(output, "backup cycle failed") || strings.Contains(output, ": SUCCESS")) {
				t.Fatalf("incomplete backup reported success:\n%s", output)
			}
			if !complete && tc.failure != "lock" && !strings.Contains(output, "retention skipped") {
				t.Fatalf("incomplete backup did not report skipped retention:\n%s", output)
			}
			f.checkFile(t, "care-20260102-030405.dump.enc", tc.database)
			f.checkFile(t, "files-20260102-030405.tar.gz.enc", tc.files)
			f.checkFile(t, "care-old.dump.enc", true)
			wantTemporaries := 0
			if tc.temporary != "" {
				wantTemporaries = 1
				f.checkFile(t, tc.temporary, true)
			}
			leftovers, err := filepath.Glob(filepath.Join(f.root, "backups", ".*.tmp*"))
			if err != nil || len(leftovers) != wantTemporaries {
				t.Fatalf("unexpected temporary backup files: %v, %v", leftovers, err)
			}
		})
	}
}

func TestBackupScriptManualDatabaseOnly(t *testing.T) {
	for _, failure := range []string{"", "db-mv", "db-rm", "lock"} {
		t.Run("failure="+failure, func(t *testing.T) {
			f := newBackupScriptFixture(t)
			code := 0
			if failure != "" {
				code = 1
			}
			output := f.start(t, "manual", []string{"once", "manual-20260102-030405"}, "BACKUP_TEST_FAIL="+failure).wait(t, code)
			trace := f.trace(t)
			if strings.Contains(trace, "manual tar\n") || strings.Contains(trace, "manual find\n") || strings.Contains(trace, "manual sleep\n") {
				t.Fatalf("manual backup did more than the database backup:\n%s", trace)
			}
			if failure != "" && strings.Contains(output, "database: wrote") {
				t.Fatalf("failed manual backup reported success:\n%s", output)
			}
			f.checkFile(t, "care-manual-20260102-030405.dump.enc", failure == "")
		})
	}
}

func TestBackupScriptPruneFailures(t *testing.T) {
	for _, tc := range []struct {
		failure, message string
		findCalls        int
	}{
		{"temp-prune", "removing abandoned temporary backup files failed", 1},
		{"retention-prune", "pruning old backups failed", 2},
	} {
		t.Run(tc.failure, func(t *testing.T) {
			f := newBackupScriptFixture(t)
			for _, name := range []string{".care-stale.dump.tmp", ".files-stale.tar.gz.tmp", "care-old.dump.enc", "files-old.tar.gz.enc"} {
				f.write(t, "backups/"+name)
				old := time.Now().Add(-96 * time.Hour)
				if err := os.Chtimes(filepath.Join(f.root, "backups", name), old, old); err != nil {
					t.Fatal(err)
				}
			}
			output := f.start(t, "daily", nil, "BACKUP_TEST_FAIL="+tc.failure).wait(t, 99)
			if !strings.Contains(output, tc.message) || !strings.Contains(output, "published, but cleanup FAILED") {
				t.Fatalf("cleanup failure was not reported:\n%s", output)
			}
			for _, claim := range []string{": SUCCESS", "[backup] done", "retention skipped", "every existing backup retained"} {
				if strings.Contains(output, claim) {
					t.Fatalf("partial cleanup made a false claim %q:\n%s", claim, output)
				}
			}
			if got := strings.Count(f.trace(t), "daily find\n"); got != tc.findCalls {
				t.Fatalf("find calls = %d, want %d", got, tc.findCalls)
			}
			f.checkFile(t, ".care-stale.dump.tmp", false)
			f.checkFile(t, ".files-stale.tar.gz.tmp", tc.failure == "temp-prune")
			f.checkFile(t, "care-old.dump.enc", tc.failure == "temp-prune")
			f.checkFile(t, "files-old.tar.gz.enc", true)
			f.checkFile(t, "care-20260102-030405.dump.enc", true)
			f.checkFile(t, "files-20260102-030405.tar.gz.enc", true)
		})
	}
}

func TestBackupScriptPrunesOnlyTopLevelRegularFiles(t *testing.T) {
	for _, retention := range []string{"0", "1"} {
		t.Run("retention="+retention, func(t *testing.T) {
			f := newBackupScriptFixture(t)
			old := time.Now().Add(-96 * time.Hour)
			for _, name := range []string{"unrelated", ".care-directory.dump.tmp", "care-directory.dump.enc"} {
				path := filepath.Join(f.root, "backups", name)
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(path, old, old); err != nil {
					t.Fatal(err)
				}
			}
			files := map[string]bool{
				".care-stale.dump.tmp":               false,
				".files-stale.tar.gz.tmp":            false,
				"care-old.dump.enc":                  retention == "0",
				"files-old.tar.gz.enc":               retention == "0",
				"notes.txt":                          true,
				"unrelated/.care-nested.dump.tmp":    true,
				"unrelated/.files-nested.tar.gz.tmp": true,
				"unrelated/care-nested.dump.enc":     true,
				"unrelated/files-nested.tar.gz.enc":  true,
			}
			for name := range files {
				f.write(t, "backups/"+name)
				if err := os.Chtimes(filepath.Join(f.root, "backups", name), old, old); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink("notes.txt", filepath.Join(f.root, "backups", ".files-link.tar.gz.tmp")); err != nil {
				t.Fatal(err)
			}
			output := f.start(t, "daily", nil, "DB_BACKUP_RETENTION_PERIOD="+retention).wait(t, 99)
			if !strings.Contains(output, ": SUCCESS") {
				t.Fatalf("complete backup and cleanup failed:\n%s", output)
			}
			for name, want := range files {
				f.checkFile(t, name, want)
			}
			for _, name := range []string{".care-directory.dump.tmp", "care-directory.dump.enc", ".files-link.tar.gz.tmp", ".backup.lock"} {
				f.checkFile(t, name, true)
			}
		})
	}
}

func TestBackupScriptRejectsExistingBackupPaths(t *testing.T) {
	for _, tc := range []struct {
		name, final, kind string
		manual            bool
	}{
		{"manual replay", "care-manual-20260102-030405.dump.enc", "published", true},
		{"scheduled replay", "care-20260102-030405.dump.enc", "published", false},
		{"scheduled files collision", "files-20260102-030405.tar.gz.enc", "file", false},
		{"directory collision", "care-manual-20260102-030405.dump.enc", "directory", true},
		{"dangling symlink collision", "care-manual-20260102-030405.dump.enc", "symlink", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBackupScriptFixture(t)
			var args []string
			firstCode, retryCode := 99, 99
			if tc.manual {
				args = []string{"once", "manual-20260102-030405"}
				firstCode, retryCode = 0, 1
			}
			path := filepath.Join(f.root, "backups", tc.final)
			switch tc.kind {
			case "published":
				f.start(t, "first", args).wait(t, firstCode)
			case "file":
				f.write(t, "backups/"+tc.final)
			case "directory":
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink("missing", path); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			var original []byte
			if before.Mode().IsRegular() {
				original, err = os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
			}
			output := f.start(t, "retry", args).wait(t, retryCode)
			if !strings.Contains(output, "refusing to overwrite existing backup") || strings.Contains(output, ": SUCCESS") {
				t.Fatalf("existing backup path was not rejected:\n%s", output)
			}
			for _, command := range []string{"pg_dump", "tar", "mv", "find"} {
				if strings.Contains(f.trace(t), "retry "+command+"\n") {
					t.Fatalf("collision reached %s:\n%s", command, f.trace(t))
				}
			}
			after, err := os.Lstat(path)
			if err != nil || after.Mode() != before.Mode() {
				t.Fatalf("existing backup path changed: %v", err)
			}
			if original != nil {
				data, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(data, original) {
					t.Fatalf("previous backup contents changed: %v", err)
				}
			}
			if strings.HasPrefix(tc.final, "files-") {
				f.checkFile(t, "care-20260102-030405.dump.enc", false)
			}
		})
	}
}

func TestBackupScriptWritersSerialize(t *testing.T) {
	for _, firstID := range []string{"manual", "daily"} {
		t.Run(firstID+" first", func(t *testing.T) {
			f := newBackupScriptFixture(t)
			if f.flock == "" {
				t.Skip("native flock is unavailable")
			}
			args := map[string][]string{"manual": {"once", "manual-20260102-030405"}, "daily": nil}
			secondID := "daily"
			if firstID == "daily" {
				secondID = "manual"
			}
			runs := make(map[string]*backupScriptRun)
			runs[firstID] = f.start(t, firstID, args[firstID], "BACKUP_TEST_HOLD="+firstID, "BACKUP_TEST_HOLD_SLEEP=1")
			f.waitFor(t, "holding", "")
			runs[secondID] = f.start(t, secondID, args[secondID], "BACKUP_TEST_HOLD_SLEEP=1")
			f.waitFor(t, "trace", secondID+" flock\n")
			time.Sleep(150 * time.Millisecond)
			trace := f.trace(t)
			if strings.Contains(trace, secondID+" pg_dump\n") || strings.Contains(trace, secondID+" find\n") {
				t.Fatalf("another writer entered the active backup:\n%s", trace)
			}
			stamp := "20260102-030405"
			if firstID == "manual" {
				stamp = "manual-" + stamp
			}
			f.checkFile(t, ".care-"+stamp+".dump.tmp", true)
			f.write(t, "release")
			runs["manual"].wait(t, 0)
			f.waitFor(t, "sleeping", "")
			f.start(t, "late-manual", []string{"once", "manual-late-20260102-030405"}).wait(t, 0)
			select {
			case <-runs["daily"].done:
				t.Fatal("scheduler exited instead of releasing its lock before sleep")
			default:
			}
			f.checkFile(t, "care-manual-late-20260102-030405.dump.enc", true)
			f.checkFile(t, "care-20260102-030405.dump.enc", true)
			f.checkFile(t, "files-20260102-030405.tar.gz.enc", true)
			f.write(t, "wake")
			runs["daily"].wait(t, 99)
		})
	}
}

func (f *backupScriptFixture) status(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.root, "state", "backup-status"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestBackupScriptRefusesWhenBackupFolderIsFull(t *testing.T) {
	f := newBackupScriptFixture(t)
	output := f.start(t, "daily", nil, "BACKUP_TEST_FREE_KB=1024").wait(t, 99)
	if !strings.Contains(output, "not enough space in the backup folder") || strings.Contains(output, ": SUCCESS") {
		t.Fatalf("full folder was not refused:\n%s", output)
	}
	if strings.Contains(f.trace(t), "daily pg_dump\n") {
		t.Fatalf("backup started writing on a full folder:\n%s", f.trace(t))
	}
	status := f.status(t)
	for _, want := range []string{"state=failed\n", "reason=disk_full\n", "free_kb=1024\n", "need_kb=1048576\n"} {
		if !strings.Contains(status, want) {
			t.Fatalf("status missing %q:\n%s", want, status)
		}
	}
	f.checkFile(t, "care-20260102-030405.dump.enc", false)
}

func TestBackupScriptSizesNeedFromLatestSet(t *testing.T) {
	const need = (1024*1024+2*2*1024*1024)*5/4 + 262144
	for _, tc := range []struct {
		free int
		ok   bool
	}{{need - 1, false}, {need, true}} {
		t.Run(strconv.Itoa(tc.free), func(t *testing.T) {
			f := newBackupScriptFixture(t)
			for name, size := range map[string]int64{
				"care-20260101-020000.dump.enc":    1 << 30,
				"files-20260101-020000.tar.gz.enc": 2 << 30,
			} {
				fh, err := os.Create(filepath.Join(f.root, "backups", name))
				if err != nil {
					t.Fatal(err)
				}
				if err := fh.Truncate(size); err != nil {
					t.Fatal(err)
				}
				if err := fh.Close(); err != nil {
					t.Fatal(err)
				}
			}
			output := f.start(t, "daily", nil, "BACKUP_TEST_FREE_KB="+strconv.Itoa(tc.free)).wait(t, 99)
			if strings.Contains(output, "SUCCESS") != tc.ok {
				t.Fatalf("free=%d KB, need=%d KB:\n%s", tc.free, need, output)
			}
			want := "state=ok\n"
			if !tc.ok {
				want = "need_kb=" + strconv.Itoa(need) + "\n"
			}
			if !strings.Contains(f.status(t), want) {
				t.Fatalf("status missing %q:\n%s", want, f.status(t))
			}
		})
	}
}

func TestBackupScriptRecordsOrdinaryFailure(t *testing.T) {
	f := newBackupScriptFixture(t)
	f.start(t, "daily", nil, "BACKUP_TEST_FAIL=tar", "BACKUP_TEST_FREE_KB=99999999").wait(t, 99)
	status := f.status(t)
	if !strings.Contains(status, "state=failed\n") || !strings.Contains(status, "reason=error\n") {
		t.Fatalf("ordinary failure recorded as:\n%s", status)
	}
}

func TestBackupScriptManualRefusesWhenFull(t *testing.T) {
	f := newBackupScriptFixture(t)
	output := f.start(t, "manual", []string{"once", "manual-20260102-030405"}, "BACKUP_TEST_FREE_KB=10").wait(t, 1)
	if !strings.Contains(output, "not enough space in the backup folder") {
		t.Fatalf("manual backup on a full folder:\n%s", output)
	}
	if strings.Contains(f.trace(t), "manual pg_dump\n") {
		t.Fatalf("manual backup started writing on a full folder:\n%s", f.trace(t))
	}
	if _, err := os.Stat(filepath.Join(f.root, "state", "backup-status")); !os.IsNotExist(err) {
		t.Fatalf("manual backup touched the daily status: %v", err)
	}
}
