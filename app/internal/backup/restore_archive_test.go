package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRestoreFilesRejectsCorruptionAndUnsafeEntries(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python is supplied by the backend image")
	}
	for _, kind := range []string{"regular", "truncated", "invalid-tar", "traversal", "absolute", "symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			root := restoreTestDir(t)
			var compressed bytes.Buffer
			gz := gzip.NewWriter(&compressed)
			tw := tar.NewWriter(gz)
			header := &tar.Header{Name: "bucket/object", Mode: 0o600, Size: 7}
			switch kind {
			case "traversal":
				header.Name = "../escaped"
			case "absolute":
				header.Name = filepath.Join(root, "escaped")
			case "symlink":
				header.Typeflag, header.Size, header.Linkname = tar.TypeSymlink, 0, "../escaped"
			case "hardlink":
				header.Typeflag, header.Size, header.Linkname = tar.TypeLink, 0, "../escaped"
			}
			if err := tw.WriteHeader(header); err != nil {
				t.Fatal(err)
			}
			if header.Size != 0 {
				if _, err := tw.Write([]byte("payload")); err != nil {
					t.Fatal(err)
				}
			}
			if err := tw.Close(); err != nil {
				t.Fatal(err)
			}
			if err := gz.Close(); err != nil {
				t.Fatal(err)
			}
			data := compressed.Bytes()
			if kind == "truncated" {
				data = data[:len(data)-8]
			}
			if kind == "invalid-tar" {
				compressed.Reset()
				gz = gzip.NewWriter(&compressed)
				if _, err := gz.Write(bytes.Repeat([]byte("invalid archive"), 100)); err != nil {
					t.Fatal(err)
				}
				if err := gz.Close(); err != nil {
					t.Fatal(err)
				}
				data = compressed.Bytes()
			}
			archive := filepath.Join(root, "files.tar.gz")
			if err := os.WriteFile(archive, data, 0o600); err != nil {
				t.Fatal(err)
			}
			script := strings.ReplaceAll(validateRestoreFiles, `"/restore/files.tar.gz"`, strconv.Quote(archive))
			script = strings.ReplaceAll(script, `"/restore/files"`, strconv.Quote(filepath.Join(root, "files")))
			script = strings.ReplaceAll(script, "os.sync()", "pass")
			cmd := exec.Command(python, "-B", "-c", script)
			out, err := cmd.CombinedOutput()
			if (err == nil) != (kind == "regular") {
				t.Fatalf("unexpected archive result: %v: %s", err, out)
			}
			if _, err := os.Stat(filepath.Join(root, "escaped")); !os.IsNotExist(err) {
				t.Fatal("archive extraction escaped its staging directory")
			}
			if kind == "regular" {
				data, err := os.ReadFile(filepath.Join(root, "files", "bucket", "object"))
				if err != nil || string(data) != "payload" {
					t.Fatal("validated archive was not fully extracted")
				}
			}
		})
	}
}

func TestRestorePostgresTransactionsAndCorruptPayload(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires an unprivileged local PostgreSQL fixture")
	}
	tools := map[string]string{}
	for _, name := range []string{"initdb", "postgres", "psql", "createdb", "pg_dump", "pg_restore"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Skip("local PostgreSQL tools are not installed")
		}
		tools[name] = path
	}
	root := restoreTestDir(t)
	dataDir := filepath.Join(root, "data")
	socketDir, err := filepath.Abs(filepath.Join("..", "..", "..", ".restore-socket-"+strings.TrimPrefix(filepath.Base(root), ".restore-test-")))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(socketDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(socketDir); err != nil {
			t.Error(err)
		}
	})
	env := []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + root, "PGHOST=" + socketDir, "PGPORT=6543",
		"PGUSER=restore_fixture", "PGDATABASE=postgres", "PGCONNECT_TIMEOUT=2", "LC_ALL=C",
	}
	init := exec.Command(tools["initdb"], "-D", dataDir, "--no-locale", "--encoding=UTF8", "--auth=trust", "--username=restore_fixture")
	init.Env = env
	if out, err := init.CombinedOutput(); err != nil {
		t.Fatalf("cannot initialize an isolated PostgreSQL fixture: %v: %s", err, out)
	}
	log, err := os.Create(filepath.Join(root, "postgres.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := log.Close(); err != nil {
			t.Error(err)
		}
	})
	server := exec.Command(tools["postgres"], "-D", dataDir, "-p", "6543",
		"-c", "listen_addresses=", "-c", "unix_socket_directories="+socketDir, "-c", "fsync=on")
	server.Dir, server.Env, server.Stdout, server.Stderr = dataDir, env, log, log
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Wait() }()
	t.Cleanup(func() {
		if err := server.Process.Signal(os.Interrupt); err != nil {
			t.Error(err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Error("PostgreSQL fixture shutdown:", err)
			}
		case <-time.After(10 * time.Second):
			if err := server.Process.Kill(); err != nil {
				t.Error(err)
			}
			<-done
			t.Error("PostgreSQL fixture did not shut down promptly")
		}
	})
	client := func(tool string, args ...string) ([]byte, error) {
		cmd := exec.Command(tools[tool], args...)
		cmd.Dir, cmd.Env = dataDir, env
		return cmd.CombinedOutput()
	}
	ready := false
	for n := 0; n < 100; n++ {
		if _, err := client("psql", "-X", "-A", "-t", "-c", "SELECT 1"); err == nil {
			ready = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		data, _ := os.ReadFile(filepath.Join(root, "postgres.log"))
		t.Fatalf("isolated PostgreSQL fixture did not become ready: %s", data)
	}
	j := &restoreJournal{ID: strings.Repeat("b", 24), Project: "care-clinic", Database: `clinic "o'brien`}
	runSQL := func(script string) ([]byte, error) {
		cmd := exec.Command("sh", "-c", script)
		cmd.Dir = dataDir
		cmd.Env = append(append([]string(nil), env...),
			"PGAPPNAME="+j.tag(),
			"RESTORE_DB="+j.Database, "RESTORE_STAGE="+j.stagedDatabase(), "RESTORE_OLD="+j.oldDatabase(),
			"RESTORE_TAG="+j.tag(), "RESTORE_ORIGINAL_OID="+j.OriginalOID, "RESTORE_STAGED_OID="+j.StagedOID)
		return cmd.CombinedOutput()
	}
	mustSQL := func(script string) []byte {
		t.Helper()
		out, err := runSQL(script)
		if err != nil {
			t.Fatalf("fixture SQL failed: %v: %s", err, out)
		}
		return out
	}
	mustClient := func(tool string, args ...string) []byte {
		t.Helper()
		out, err := client(tool, args...)
		if err != nil {
			t.Fatalf("fixture %s failed: %v: %s", tool, err, out)
		}
		return out
	}
	mustSQL("set -eu\n" + restorePSQL + " <<'SQL'\nCREATE DATABASE :\"live\";\nCREATE DATABASE fixture_source;\nSQL")
	mustClient("psql", "-X", "-v", "ON_ERROR_STOP=1", "-d", j.Database, "-c",
		"CREATE TABLE records (value text); INSERT INTO records VALUES ('original');")
	mustClient("psql", "-X", "-v", "ON_ERROR_STOP=1", "-d", "fixture_source", "-c",
		"CREATE TABLE records (value text); INSERT INTO records SELECT repeat(md5(i::text), 4) FROM generate_series(1, 5000) i;")
	dump := filepath.Join(root, "valid.dump")
	mustClient("pg_dump", "-Fc", "-d", "fixture_source", "-f", dump)
	data, err := os.ReadFile(dump)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := filepath.Join(root, "corrupt.dump")
	if err := os.WriteFile(corrupt, data[:len(data)-32], 0o600); err != nil {
		t.Fatal(err)
	}
	mustClient("pg_restore", "--list", corrupt)
	if _, err := client("pg_restore", "--exit-on-error", "--file=/dev/null", corrupt); err == nil {
		t.Fatal("full payload validation accepted the truncated dump")
	}
	replaceDump := func(path string) string {
		return strings.ReplaceAll(createRestoreDatabase, "/restore/database.dump", "'"+strings.ReplaceAll(path, "'", "'\\''")+"'")
	}
	if _, err := runSQL(replaceDump(corrupt)); err == nil {
		t.Fatal("staged restore accepted a corrupt payload")
	}
	if value := strings.TrimSpace(string(mustClient("psql", "-X", "-A", "-t", "-d", j.Database, "-c", "SELECT value FROM records"))); value != "original" {
		t.Fatal("corrupt restore changed the original database")
	}
	if value := strings.TrimSpace(string(mustClient("psql", "-X", "-A", "-t", "-d", j.stagedDatabase(), "-c",
		"SELECT count(*) FROM information_schema.tables WHERE table_name = 'records'"))); value != "0" {
		t.Fatal("failed staged restoration left a partially restored schema")
	}
	mustSQL(dropStagedDatabase)
	mustSQL(replaceDump(dump))
	var state restoreDatabases
	if err := json.Unmarshal(mustSQL(inspectRestoreDatabases), &state); err != nil {
		t.Fatal(err)
	}
	j.OriginalOID, j.StagedOID = state.Live.OID, state.Staged.OID
	if state.Staged.Tag != j.tag() {
		t.Fatal("staged database ownership was not recorded")
	}
	pending := exec.Command(tools["psql"], "-X", "-v", "ON_ERROR_STOP=1", "-c", "SELECT pg_sleep(30)")
	pending.Dir, pending.Stdout, pending.Stderr = dataDir, io.Discard, io.Discard
	pending.Env = append(append([]string(nil), env...), "PGAPPNAME="+j.tag())
	if err := pending.Start(); err != nil {
		t.Fatal(err)
	}
	pendingDone := make(chan error, 1)
	go func() { pendingDone <- pending.Wait() }()
	waiting := false
	for n := 0; n < 100; n++ {
		out := mustSQL("set -eu\n" + restorePSQL + " -A -t <<'SQL'\nSELECT EXISTS (SELECT FROM pg_stat_activity WHERE application_name = :'tag' AND pid <> pg_backend_pid());\nSQL")
		if strings.TrimSpace(string(out)) == "t" {
			waiting = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mustSQL(stopRestoreDatabaseHelpers)
	select {
	case err := <-pendingDone:
		if !waiting || err == nil {
			t.Fatal("interrupted restore database session was not terminated")
		}
	case <-time.After(10 * time.Second):
		if err := pending.Process.Kill(); err != nil {
			t.Error(err)
		}
		<-pendingDone
		t.Fatal("interrupted restore session is still active")
	}
	brokenSwap := strings.ReplaceAll(swapRestoreDatabases, `ALTER DATABASE :"staged" RENAME TO :"live";`, "SELECT 1 / 0;")
	if _, err := runSQL(brokenSwap); err == nil {
		t.Fatal("synthetic second-rename failure was ignored")
	}
	if err := json.Unmarshal(mustSQL(inspectRestoreDatabases), &state); err != nil {
		t.Fatal(err)
	}
	if state.Live.OID != j.OriginalOID || state.Staged.OID != j.StagedOID || state.Old != nil {
		t.Fatal("database renames were not transactional")
	}
	mustSQL(reconnectOriginalDatabase)
	mustSQL(swapRestoreDatabases)
	if value := strings.TrimSpace(string(mustClient("psql", "-X", "-A", "-t", "-d", j.Database, "-c", "SELECT count(*) FROM records"))); value != "5000" {
		t.Fatal("cutover did not activate the fully restored database")
	}
	mustSQL(rollbackRestoreDatabases)
	if value := strings.TrimSpace(string(mustClient("psql", "-X", "-A", "-t", "-d", j.Database, "-c", "SELECT value FROM records"))); value != "original" {
		t.Fatal("interrupted cutover did not restore the original database")
	}
}
