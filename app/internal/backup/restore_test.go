package backup

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

type restoreDockerCall struct {
	Args []string          `json:"args"`
	Env  map[string]string `json:"env"`
}

type restoreDockerState struct {
	Calls         []restoreDockerCall          `json:"calls"`
	Volumes       map[string]map[string]string `json:"volumes"`
	Databases     restoreDatabases             `json:"databases"`
	Files         string                       `json:"files"`
	PreviousFiles string                       `json:"previous_files"`
	Fail          string                       `json:"fail"`
	Stopped       bool                         `json:"stopped"`
	Helper        bool                         `json:"helper"`
}

func restoreTestDir(t *testing.T) string {
	t.Helper()
	var id [8]byte
	if _, err := rand.Read(id[:]); err != nil {
		t.Fatal(err)
	}
	path, err := filepath.Abs(".restore-test-" + hex.EncodeToString(id[:]))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(path); err != nil {
			t.Error(err)
		}
	})
	return path
}

func newRestoreFixture(t *testing.T) (*Store, func() restoreDockerState, func(restoreDockerState)) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX command fixture")
	}
	root := restoreTestDir(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(executable, "'", "'\\''") + "' -test.run=^TestRestoreDockerProcess$ -- \"$@\"\n"
	if err := os.WriteFile(filepath.Join(root, "docker"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "docker-state.json")
	t.Setenv("CARE_RESTORE_DOCKER_STATE", path)
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	save := func(state restoreDockerState) {
		t.Helper()
		data, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	load := func() restoreDockerState {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var state restoreDockerState
		if err := json.Unmarshal(data, &state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	save(restoreDockerState{
		Volumes:   map[string]map[string]string{},
		Databases: restoreDatabases{Live: &restoreDatabase{OID: "100"}},
		Files:     "original-files",
	})
	s := New(proc.Runner{Dir: root, Env: os.Environ()}, &Store{
		Dir: root, BackupDir: root, Project: "care-clinic", Image: "fixture-backup", BackendImage: "fixture-backend",
		EnsureImage: func() error { return nil }, EnsureRestoreImages: func() error { return nil },
		Migrate: func(string, string) error { return nil },
	})
	for name, contents := range map[string]string{
		"backend.env":                      "POSTGRES_PASSWORD='SyntheticDatabaseSecret'\n",
		"care-20260101-010101.dump":        "synthetic-dump",
		"care-20260101-010101.dump.enc":    "synthetic-encrypted-dump",
		"files-20260101-010101.tar.gz":     "synthetic-archive",
		"files-20260101-010101.tar.gz.enc": "synthetic-encrypted-archive",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return s, load, save
}

func TestRestoreDockerProcess(t *testing.T) {
	path := os.Getenv("CARE_RESTORE_DOCKER_STATE")
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		os.Exit(98)
	}
	var state restoreDockerState
	if err := json.Unmarshal(data, &state); err != nil {
		os.Exit(98)
	}
	i := slices.Index(os.Args, "--")
	if i < 0 {
		os.Exit(98)
	}
	args := os.Args[i+1:]
	value := func(flag string) string {
		if i := slices.Index(args, flag); i >= 0 && i+1 < len(args) {
			return args[i+1]
		}
		return ""
	}
	env := map[string]string{}
	for _, name := range []string{"BACKUP_PASS", "PGPASSWORD", "RESTORE_STAGE", "RESTORE_TAG"} {
		env[name] = os.Getenv(name)
	}
	state.Calls = append(state.Calls, restoreDockerCall{Args: args, Env: env})
	code, output := 0, ""
	command := strings.Join(args, " ")
	switch {
	case strings.HasPrefix(command, "volume create "):
		labels := map[string]string{}
		for i, arg := range args[:len(args)-1] {
			if arg == "--label" {
				key, value, _ := strings.Cut(args[i+1], "=")
				labels[key] = value
			}
		}
		name := args[len(args)-1]
		if _, exists := state.Volumes[name]; !exists {
			state.Volumes[name] = labels
		}
	case strings.HasPrefix(command, "volume ls "):
		name := strings.TrimPrefix(value("--filter"), "name=")
		if _, exists := state.Volumes[name]; exists {
			output = name
		}
	case strings.HasPrefix(command, "volume inspect "):
		labels, exists := state.Volumes[args[len(args)-1]]
		if !exists {
			code = 1
		} else {
			data, _ := json.Marshal(labels)
			output = string(data)
		}
	case strings.HasPrefix(command, "volume rm "):
		if state.Fail == "cleanup-volume" {
			code = 1
		} else {
			delete(state.Volumes, args[len(args)-1])
		}
	case strings.HasPrefix(command, "compose stop "):
		if state.Fail == "stop" {
			code = 1
		} else {
			state.Stopped = true
		}
	case strings.HasPrefix(command, "compose ps "):
		if state.Fail == "writer-inspection" {
			code = 1
		} else {
			output = "fixture-writer"
		}
	case strings.HasPrefix(command, "inspect "):
		output = "exited"
		if !state.Stopped || state.Fail == "writer-still-running" {
			output = "running"
		}
	case strings.HasPrefix(command, "compose up "):
		if !slices.Contains(args, "db") {
			state.Stopped = false
			if state.Fail == "activation" {
				code = 1
			}
		}
	case strings.HasPrefix(command, "compose exec -T db "):
	case strings.HasPrefix(command, "ps --all --quiet "):
		if state.Helper {
			output = "fixture-owned-helper"
		}
	case command == "rm --force fixture-owned-helper":
		if state.Fail == "helper-stop" {
			code = 1
		} else {
			state.Helper = false
		}
	case args[0] == "run":
		_, suffix, _ := strings.Cut(value("--name"), "-restore-")
		if len(suffix) < 26 {
			os.Exit(98)
		}
		step := suffix[25:]
		switch step {
		case "preflight", "extract", "stop-db-helpers":
		case "database-state":
			data, _ := json.Marshal(state.Databases)
			output = string(data)
		case "stage-db":
			state.Databases.Staged = &restoreDatabase{OID: "200", Tag: os.Getenv("RESTORE_TAG")}
		case "snapshot-files":
			state.PreviousFiles = state.Files
		case "replace-files":
			state.Files = "replacement-files"
		case "rollback-files":
			state.Files = state.PreviousFiles
		case "swap-db":
			if state.Fail != "swap-before" {
				state.Databases.Old = state.Databases.Live
				state.Databases.Live = state.Databases.Staged
				state.Databases.Staged = nil
			}
			if state.Fail == "swap-before" || state.Fail == "swap-after" {
				code = 1
			}
		case "rollback-db":
			if state.Databases.Old != nil {
				state.Databases.Staged = state.Databases.Live
				state.Databases.Live = state.Databases.Old
				state.Databases.Old = nil
			}
		case "cleanup-db":
			if strings.Contains(args[len(args)-1], `DROP DATABASE :"old"`) {
				state.Databases.Old = nil
			} else {
				state.Databases.Staged = nil
			}
		default:
			code = 99
		}
		if state.Fail == step {
			code = 1
		}
	default:
		code = 99
	}
	data, err = json.Marshal(state)
	if err != nil || os.WriteFile(path, data, 0o600) != nil {
		os.Exit(98)
	}
	if output != "" {
		fmt.Println(output)
	}
	os.Exit(code)
}

func TestRestoreRejectsUnsafeInputs(t *testing.T) {
	for _, test := range []struct {
		name, dump, files string
	}{
		{"path", "../care-20260101-010101.dump", ""},
		{"timestamp", "care-20269901-010101.dump", ""},
		{"mismatched-set", "care-20260101-010101.dump", "files-20260101-010102.tar.gz"},
		{"manual-with-scheduled-files", "care-manual-20260101-010101.dump", "files-20260101-010101.tar.gz"},
		{"directory", "care-20260101-010102.dump", ""},
		{"symlink", "care-20260101-010103.dump", ""},
		{"empty", "care-20260101-010104.dump", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, load, _ := newRestoreFixture(t)
			for name, data := range map[string]string{
				"care-manual-20260101-010101.dump": "synthetic",
				"care-20260101-010104.dump":        "",
			} {
				if err := os.WriteFile(filepath.Join(s.Dir, name), []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Mkdir(filepath.Join(s.Dir, "care-20260101-010102.dump"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(s.Dir, "care-20260101-010101.dump"), filepath.Join(s.Dir, "care-20260101-010103.dump")); err != nil {
				t.Fatal(err)
			}
			if err := s.Restore(test.dump, test.files, ""); err == nil {
				t.Fatal("unsafe backup input was accepted")
			}
			if len(load().Calls) != 0 {
				t.Fatal("input rejection reached Docker")
			}
		})
	}
}

func TestRestorePreflightPreservesLiveData(t *testing.T) {
	for _, failure := range []string{"preflight", "extract", "stage-db", "migration", "stop", "writer-inspection", "writer-still-running", "snapshot-files"} {
		t.Run(failure, func(t *testing.T) {
			s, load, save := newRestoreFixture(t)
			state := load()
			state.Fail = failure
			save(state)
			if failure == "migration" {
				s.Migrate = func(database, id string) error {
					if database != "care_restore_"+id {
						t.Fatal("migration was not isolated to the staging database")
					}
					return fmt.Errorf("synthetic migration failure")
				}
			}
			if err := s.Restore("care-20260101-010101.dump", "files-20260101-010101.tar.gz", ""); err == nil {
				t.Fatal("restore ignored a preparation failure")
			}
			state = load()
			if state.Databases.Live.OID != "100" || state.Databases.Old != nil || state.Files != "original-files" {
				t.Fatalf("preparation failure changed live data: %+v", state)
			}
			j, err := s.readRestoreJournal()
			if err != nil || j == nil || j.Phase != "staging" {
				t.Fatalf("failure did not retain staging recovery metadata: %+v, %v", j, err)
			}
			if failure == "preflight" || failure == "extract" || failure == "stage-db" || failure == "migration" {
				for _, call := range state.Calls {
					if len(call.Args) > 1 && call.Args[0] == "compose" && call.Args[1] == "stop" {
						t.Fatal("writers were stopped before the replacement was fully prepared")
					}
				}
			}
		})
	}
}

func TestInterruptedRestoreRollsBackBeforeActivation(t *testing.T) {
	for _, failure := range []string{"replace-files", "swap-before", "swap-after"} {
		t.Run(failure, func(t *testing.T) {
			s, load, save := newRestoreFixture(t)
			state := load()
			state.Fail = failure
			save(state)
			if err := s.Restore("care-20260101-010101.dump", "files-20260101-010101.tar.gz", ""); err == nil {
				t.Fatal("cutover failure was ignored")
			}
			j, err := s.readRestoreJournal()
			if err != nil || j.Phase != "prepared" {
				t.Fatalf("missing recoverable cutover journal: %+v, %v", j, err)
			}
			state = load()
			state.Fail, state.Helper = "", true
			save(state)
			if err := s.RecoverRestore(); err != nil {
				t.Fatal(err)
			}
			state = load()
			if state.Databases.Live.OID != "100" || state.Databases.Old != nil ||
				state.Files != "original-files" || !state.Stopped || state.Helper {
				t.Fatalf("recovery did not restore the original stopped clinic: %+v", state)
			}
			if len(state.Volumes) != 3 {
				t.Fatal("recovery removed copies before successful activation")
			}
			j, err = s.readRestoreJournal()
			if err != nil || j.Phase != "rolled-back" {
				t.Fatalf("recovery state was not persisted: %+v, %v", j, err)
			}
			state.Files, state.Stopped = "writes-after-recovery", false
			save(state)
			if err := s.RecoverRestore(); err != nil || load().Files != "writes-after-recovery" {
				t.Fatalf("a completed rollback replayed its snapshot after services resumed: %v", err)
			}
			if err := s.FinishRestore(); err != nil {
				t.Fatal(err)
			}
			state = load()
			if state.Databases.Staged != nil || len(state.Volumes) != 1 {
				t.Fatal("completed recovery did not remove only its staging resources")
			}
			if _, err := os.Stat(s.restoreJournalPath()); !os.IsNotExist(err) {
				t.Fatalf("completed journal was not removed: %v", err)
			}
		})
	}
}

func TestCommittedRestoreNeverRollsBackNewWrites(t *testing.T) {
	s, load, save := newRestoreFixture(t)
	state := load()
	state.Fail = "activation"
	save(state)
	if err := s.Restore("care-20260101-010101.dump", "files-20260101-010101.tar.gz", ""); err == nil {
		t.Fatal("activation failure was ignored")
	}
	j, err := s.readRestoreJournal()
	if err != nil || j.Phase != "committed" {
		t.Fatalf("activation began before commit was durable: %+v, %v", j, err)
	}
	state = load()
	state.Files, state.Fail = "writes-after-activation", ""
	previousCalls := len(state.Calls)
	save(state)
	if err := s.RecoverRestore(); err != nil {
		t.Fatal(err)
	}
	state = load()
	if state.Files != "writes-after-activation" || state.Databases.Live.OID != "200" || state.Databases.Old.OID != "100" {
		t.Fatal("recovery rolled back an already committed restore")
	}
	for _, call := range state.Calls[previousCalls:] {
		if call.Args[0] != "ps" {
			t.Fatalf("committed recovery changed running state: %v", call.Args)
		}
	}
	state.Fail = "cleanup-volume"
	save(state)
	if err := s.FinishRestore(); err == nil {
		t.Fatal("cleanup failure was ignored")
	}
	if _, err := s.readRestoreJournal(); err != nil {
		t.Fatal(err)
	}
	state = load()
	state.Fail = ""
	save(state)
	if err := s.FinishRestore(); err != nil {
		t.Fatal("idempotent cleanup could not resume:", err)
	}
	if load().Files != "writes-after-activation" {
		t.Fatal("cleanup changed live files")
	}
}

func TestRecoveryFailsClosedForMissingOrUnownedCopies(t *testing.T) {
	for _, failure := range []string{"helper-stop", "missing-volume", "foreign-volume"} {
		t.Run(failure, func(t *testing.T) {
			s, load, save := newRestoreFixture(t)
			state := load()
			state.Fail = "swap-after"
			save(state)
			if err := s.Restore("care-20260101-010101.dump", "files-20260101-010101.tar.gz", ""); err == nil {
				t.Fatal("expected interrupted restore")
			}
			j, err := s.readRestoreJournal()
			if err != nil {
				t.Fatal(err)
			}
			state = load()
			state.Fail = failure
			switch failure {
			case "helper-stop":
				state.Helper = true
			case "missing-volume":
				delete(state.Volumes, j.oldVolume())
			case "foreign-volume":
				state.Volumes[j.oldVolume()][RestoreLabel] = "another-restore"
			}
			save(state)
			if err := s.RecoverRestore(); err == nil {
				t.Fatal("recovery ignored missing ownership or a failed helper stop")
			}
			if load().Databases.Live.OID != "200" {
				t.Fatal("recovery touched databases before checking recovery resources")
			}
			j, err = s.readRestoreJournal()
			if err != nil || j.Phase != "prepared" {
				t.Fatal("failed recovery discarded metadata")
			}
		})
	}
}

func TestRestoreJournalRejectsCorruptOrForeignState(t *testing.T) {
	for _, field := range []string{"json", "id", "project", "phase", "database", "missing-identities"} {
		t.Run(field, func(t *testing.T) {
			s, load, _ := newRestoreFixture(t)
			j, err := s.newRestoreJournal(true)
			if err != nil {
				t.Fatal(err)
			}
			switch field {
			case "id":
				j.ID = "../../another-volume"
			case "project":
				j.Project = "another-project"
			case "phase":
				j.Phase = "unknown"
			case "database":
				j.Database = "postgres"
			case "missing-identities":
				j.Phase = "prepared"
			}
			data, err := json.Marshal(j)
			if err != nil {
				t.Fatal(err)
			}
			if field == "json" {
				data = []byte("{")
			}
			if err := os.WriteFile(s.restoreJournalPath(), data, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := s.RecoverRestore(); err == nil {
				t.Fatal("recovery accepted corrupt or foreign resource metadata")
			}
			if len(load().Calls) != 0 {
				t.Fatal("invalid recovery metadata reached Docker")
			}
		})
	}
}

func TestRestoreSecretsAndManualScope(t *testing.T) {
	s, load, save := newRestoreFixture(t)
	if err := os.WriteFile(filepath.Join(s.Dir, "care-manual-20260101-010101.dump.enc"), []byte("synthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := load()
	state.Fail = "activation"
	save(state)
	_, key, err := GenerateRecoveryFile()
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(t.TempDir(), "recovery.pem")
	if err := os.WriteFile(keyPath, key, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Restore("care-manual-20260101-010101.dump.enc", "", keyPath); err == nil {
		t.Fatal("expected activation failure")
	}
	foundKeyMount := false
	for _, call := range load().Calls {
		for _, arg := range call.Args {
			if strings.Contains(arg, "PRIVATE KEY") || strings.Contains(arg, "SyntheticDatabaseSecret") {
				t.Fatal("credential value appeared in Docker arguments")
			}
			if strings.Contains(arg, "_minio-data") {
				t.Fatal("database-only restore touched uploaded files")
			}
			if strings.Contains(arg, "source="+keyPath) && strings.Contains(arg, "readonly") {
				foundKeyMount = true
			}
		}
		if call.Env["BACKUP_PASS"] != "" || slices.Contains(call.Args, "BACKUP_PASS") {
			t.Fatal("restore still expects a backup password")
		}
		if call.Args[0] == "compose" && slices.Contains(call.Args, "up") &&
			slices.Contains(call.Args, "backup") {
			t.Fatal("restore started the backup scheduler as a readiness probe")
		}
	}
	if !foundKeyMount || load().Files != "original-files" {
		t.Fatal("database-only restore lost its credential or changed files")
	}
}

func TestManualBackupRestoresItsFiles(t *testing.T) {
	s, load, save := newRestoreFixture(t)
	for _, name := range []string{"care-manual-20260101-010101.dump", "files-manual-20260101-010101.tar.gz"} {
		if err := os.WriteFile(filepath.Join(s.Dir, name), []byte("synthetic"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	state := load()
	state.Fail = "activation"
	save(state)
	err := s.Restore("care-manual-20260101-010101.dump", "files-manual-20260101-010101.tar.gz", "")
	if err == nil {
		t.Fatal("expected activation failure")
	}
	if strings.Contains(err.Error(), "same backup") || strings.Contains(err.Error(), "not a files archive") {
		t.Fatalf("a manual backup's own files archive was rejected: %v", err)
	}
	touchedFiles := false
	for _, call := range load().Calls {
		for _, arg := range call.Args {
			if strings.Contains(arg, "_minio-data") {
				touchedFiles = true
			}
		}
	}
	if !touchedFiles {
		t.Fatal("the manual files archive was not staged")
	}
}

func TestListBackupsPairsManualDumpsWithManualFiles(t *testing.T) {
	s, _, _ := newRestoreFixture(t)
	for _, name := range []string{"care-manual-20260102-010101.dump.enc", "files-manual-20260102-010101.tar.gz.enc"} {
		if err := os.WriteFile(filepath.Join(s.Dir, name), []byte("synthetic"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	backups, err := s.ListBackups()
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range backups {
		if b.DBDump == "care-manual-20260102-010101.dump.enc" {
			if b.FilesArchive != "files-manual-20260102-010101.tar.gz.enc" || !strings.Contains(b.Label, "manual - DB + files") {
				t.Fatalf("manual backup not paired with its files: %+v", b)
			}
			return
		}
	}
	t.Fatal("manual backup was not listed")
}

func TestListBackupsDoesNotPairManualDumps(t *testing.T) {
	s, _, _ := newRestoreFixture(t)
	if err := os.WriteFile(filepath.Join(s.Dir, "care-manual-20260101-010101.dump"), []byte("synthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(s.Dir, "care-20260101-010102.dump"), 0o700); err != nil {
		t.Fatal(err)
	}
	backups, err := s.ListBackups()
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range backups {
		if b.Manual && b.FilesArchive != "" {
			t.Fatal("a manual backup was paired with an unrelated scheduled files archive")
		}
		if b.DBDump == "care-20260101-010102.dump" {
			t.Fatal("a directory was listed as a backup")
		}
	}
}

func TestListBackupsIsNeverNil(t *testing.T) {
	for name, dir := range map[string]string{"missing": filepath.Join(t.TempDir(), "absent"), "empty": t.TempDir()} {
		backups, err := (&Store{BackupDir: dir}).ListBackups()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if backups == nil {
			t.Fatalf("%s backup folder listed as nil, which the UI receives as null", name)
		}
	}
}
