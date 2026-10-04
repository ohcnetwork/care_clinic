package backup

import (
	"bytes"
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/compose-spec/compose-go/v2/dotenv"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/atomicfile"
)

const RestoreLabel = "org.care-clinic.restore"

var restoreIDPattern = regexp.MustCompile(`^[0-9a-f]{24}$`)
var restoreProjectPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)
var restoreOIDPattern = regexp.MustCompile(`^[1-9][0-9]*$`)

type restoreJournal struct {
	ID          string `json:"id"`
	Project     string `json:"project"`
	Phase       string `json:"phase"`
	Database    string `json:"database"`
	Host        string `json:"host"`
	Port        string `json:"port"`
	User        string `json:"user"`
	OriginalOID string `json:"original_oid,omitempty"`
	StagedOID   string `json:"staged_oid,omitempty"`
	WithFiles   bool   `json:"with_files"`
}

func (j *restoreJournal) stagedDatabase() string { return "care_restore_" + j.ID }
func (j *restoreJournal) oldDatabase() string    { return "care_previous_" + j.ID }
func (j *restoreJournal) stageVolume() string    { return j.Project + "_restore_" + j.ID + "_stage" }
func (j *restoreJournal) oldVolume() string      { return j.Project + "_restore_" + j.ID + "_previous" }
func (j *restoreJournal) tag() string            { return j.Project + ":restore:" + j.ID }
func (s *Store) restoreJournalPath() string      { return filepath.Join(s.Dir, "restore-state.json") }

func (s *Store) restoreSettings() (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(s.Dir, "backend.env"))
	if err != nil {
		return nil, err
	}
	env, err := dotenv.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("cannot read restore database settings: %w", err)
	}
	for name, fallback := range map[string]string{
		"POSTGRES_DB": "care", "POSTGRES_HOST": "db", "POSTGRES_PORT": "5432", "POSTGRES_USER": "postgres",
	} {
		if env[name] == "" {
			env[name] = fallback
		}
	}
	return env, nil
}

func (s *Store) newRestoreJournal(withFiles bool) (*restoreJournal, error) {
	env, err := s.restoreSettings()
	if err != nil {
		return nil, err
	}
	var id [12]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	j := &restoreJournal{
		ID: hex.EncodeToString(id[:]), Project: s.Project, Phase: "staging",
		Database: env["POSTGRES_DB"], Host: env["POSTGRES_HOST"], Port: env["POSTGRES_PORT"],
		User: env["POSTGRES_USER"], WithFiles: withFiles,
	}
	return j, s.validateRestoreJournal(j)
}

func (s *Store) validateRestoreJournal(j *restoreJournal) error {
	if !restoreIDPattern.MatchString(j.ID) || !restoreProjectPattern.MatchString(j.Project) || j.Project != s.Project {
		return fmt.Errorf("restore journal does not identify this installation's resources")
	}
	if len(j.Database) == 0 || len(j.Database) > 63 || strings.ContainsRune(j.Database, 0) ||
		j.Database == "postgres" || j.Database == "template0" || j.Database == "template1" ||
		j.Database == j.stagedDatabase() || j.Database == j.oldDatabase() ||
		j.Host == "" || j.Port == "" || j.User == "" {
		return fmt.Errorf("restore journal has invalid database settings")
	}
	if j.OriginalOID != "" && !restoreOIDPattern.MatchString(j.OriginalOID) ||
		j.StagedOID != "" && !restoreOIDPattern.MatchString(j.StagedOID) {
		return fmt.Errorf("restore journal has invalid database identities")
	}
	switch j.Phase {
	case "staging", "rolled-back":
	case "prepared", "committed":
		if j.OriginalOID == "" || j.StagedOID == "" || j.OriginalOID == j.StagedOID {
			return fmt.Errorf("restore journal is missing the original and replacement database identities")
		}
	default:
		return fmt.Errorf("unknown restore journal phase %q", j.Phase)
	}
	return nil
}

func (s *Store) readRestoreJournal() (*restoreJournal, error) {
	info, err := os.Lstat(s.restoreJournalPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 16*1024 {
		return nil, fmt.Errorf("restore journal is not a regular recovery metadata file")
	}
	data, err := os.ReadFile(s.restoreJournalPath())
	if err != nil {
		return nil, err
	}
	var j restoreJournal
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, fmt.Errorf("cannot read restore recovery metadata; keep %s intact: %w", s.restoreJournalPath(), err)
	}
	if err := s.validateRestoreJournal(&j); err != nil {
		return nil, err
	}
	return &j, nil
}

func (s *Store) writeRestoreJournal(j *restoreJournal) error {
	if err := s.validateRestoreJournal(j); err != nil {
		return err
	}
	data, err := json.Marshal(j)
	if err != nil {
		return err
	}
	return atomicfile.Write(s.restoreJournalPath(), append(data, '\n'), 0o600)
}

func (s *Store) PendingRestore() (bool, error) {
	j, err := s.readRestoreJournal()
	return j != nil, err
}

func (s *Store) restoreFailure(j *restoreJournal, err error) error {
	return fmt.Errorf("restore %s failed: %w; recovery data was retained. Start CARE to recover safely; do not start containers manually (some services may be stopped)", j.ID, err)
}

func restoreMount(kind, source, target string, readOnly bool) string {
	fields := []string{"type=" + kind, "source=" + source, "target=" + target}
	if readOnly {
		fields = append(fields, "readonly")
	}
	var b strings.Builder
	w := csv.NewWriter(&b)
	_ = w.Write(fields)
	w.Flush()
	return strings.TrimSuffix(b.String(), "\n")
}

func (s *Store) restoreHelperArgs(j *restoreJournal, step, network string, extra ...string) []string {
	args := []string{"run", "--rm", "--name", j.Project + "-restore-" + j.ID + "-" + step,
		"--label", RestoreLabel + "=" + j.ID,
		"--label", "com.docker.compose.project=" + j.Project, "--network", network}
	return append(args, extra...)
}

func (s *Store) stopRestoreHelpers(j *restoreJournal) error {
	args := []string{"ps", "--all", "--quiet",
		"--filter", "label=com.docker.compose.project=" + j.Project,
		"--filter", "label=" + RestoreLabel + "=" + j.ID}
	ids, err := s.run.Lines("docker", args...)
	if err != nil {
		return fmt.Errorf("cannot inspect interrupted restore helpers: %w", err)
	}
	if len(ids) == 0 {
		return nil
	}
	if err := s.run.Run("docker", append([]string{"rm", "--force"}, ids...)...); err != nil {
		return fmt.Errorf("cannot stop interrupted restore helpers: %w", err)
	}
	ids, err = s.run.Lines("docker", args...)
	if err != nil {
		return err
	}
	if len(ids) != 0 {
		return fmt.Errorf("restore helper containers are still present; recovery has not started")
	}
	return nil
}

func (s *Store) stopRestoreWriters() error {
	services := []string{"backend", "celery-worker", "celery-beat", "minio", "backup", "caddy"}
	if err := s.dc(append([]string{"stop"}, services...)...); err != nil {
		return fmt.Errorf("cannot stop CARE writers; no cutover was attempted: %w", err)
	}
	ids, err := s.run.Lines("docker", append([]string{"compose", "ps", "--all", "--quiet"}, services...)...)
	if err != nil {
		return fmt.Errorf("cannot check whether CARE writers stopped: %w", err)
	}
	if len(ids) == 0 {
		return nil
	}
	states, err := s.run.Lines("docker", append([]string{"inspect", "--format", "{{.State.Status}}"}, ids...)...)
	if err != nil {
		return err
	}
	if len(states) != len(ids) {
		return fmt.Errorf("could not confirm the state of every CARE writer")
	}
	for _, state := range states {
		if state != "exited" && state != "created" {
			return fmt.Errorf("a CARE writer is still %s; cutover has not started", state)
		}
	}
	return nil
}

func (s *Store) restoreVolumeExists(name string, labels map[string]string) (bool, error) {
	names, err := s.run.Lines("docker", "volume", "ls", "--format", "{{.Name}}", "--filter", "name="+name)
	if err != nil {
		return false, err
	}
	found := false
	for _, existing := range names {
		if existing == name {
			found = true
		}
	}
	if !found {
		return false, nil
	}
	out, err := s.run.Capture("docker", "volume", "inspect", "--format", "{{json .Labels}}", name)
	if err != nil {
		return false, err
	}
	var actual map[string]string
	if err := json.Unmarshal([]byte(out), &actual); err != nil {
		return false, err
	}
	for label, want := range labels {
		if actual[label] != want {
			return false, fmt.Errorf("volume %s is not owned by this restore/installation", name)
		}
	}
	return true, nil
}

func (s *Store) createRestoreVolume(j *restoreJournal, name string) error {
	if err := s.run.Run("docker", "volume", "create",
		"--label", RestoreLabel+"="+j.ID,
		"--label", "com.docker.compose.project="+j.Project, name); err != nil {
		return err
	}
	return s.requireRestoreVolume(j, name)
}

func (s *Store) requireRestoreVolume(j *restoreJournal, name string) error {
	exists, err := s.restoreVolumeExists(name, map[string]string{
		RestoreLabel: j.ID, "com.docker.compose.project": j.Project,
	})
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("restore volume %s is missing; recovery data must not be replaced with an empty volume", name)
	}
	return nil
}

func (s *Store) requireLiveFiles(create bool) error {
	name := s.Project + "_minio-data"
	if create {
		if err := s.run.Run("docker", "volume", "create",
			"--label", "com.docker.compose.project="+s.Project,
			"--label", "com.docker.compose.volume=minio-data", name); err != nil {
			return err
		}
	}
	exists, err := s.restoreVolumeExists(name, map[string]string{
		"com.docker.compose.project": s.Project, "com.docker.compose.volume": "minio-data",
	})
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("the installation's uploaded-files volume is missing")
	}
	return nil
}

func (s *Store) RecoverRestore() error {
	j, err := s.readRestoreJournal()
	if err != nil || j == nil {
		return err
	}
	if err := s.stopRestoreHelpers(j); err != nil {
		return s.restoreFailure(j, err)
	}
	switch j.Phase {
	case "committed":
		s.logln("Resuming the committed restore; its current database and files will not be rolled back.")
		return nil
	case "rolled-back":
		return nil
	case "prepared":
		s.logln("Recovering an interrupted cutover before any CARE services can start...")
		if err := s.stopRestoreWriters(); err != nil {
			return s.restoreFailure(j, err)
		}
		if s.EnsureImage != nil {
			if err := s.EnsureImage(); err != nil {
				return s.restoreFailure(j, err)
			}
		}
		if j.WithFiles {
			if err := s.requireRestoreVolume(j, j.oldVolume()); err != nil {
				return s.restoreFailure(j, err)
			}
			if err := s.requireLiveFiles(false); err != nil {
				return s.restoreFailure(j, err)
			}
		}
		if err := s.startRestoreDB(); err != nil {
			return s.restoreFailure(j, err)
		}
		if err := s.runRestoreSQL(j, "stop-db-helpers", stopRestoreDatabaseHelpers); err != nil {
			return s.restoreFailure(j, err)
		}
		if err := s.swapRestoreDatabase(j, true); err != nil {
			return s.restoreFailure(j, err)
		}
		if j.WithFiles {
			if err := s.replaceRestoreFiles(j, true); err != nil {
				return s.restoreFailure(j, err)
			}
		}
	}
	j.Phase = "rolled-back"
	if err := s.writeRestoreJournal(j); err != nil {
		return s.restoreFailure(j, err)
	}
	s.logln("The original data is ready; recovery copies will be kept until CARE starts successfully.")
	return nil
}

func (s *Store) FinishRestore() error {
	j, err := s.readRestoreJournal()
	if err != nil || j == nil {
		return err
	}
	if j.Phase != "committed" && j.Phase != "rolled-back" {
		return fmt.Errorf("restore %s must be recovered before its recovery copies can be removed", j.ID)
	}
	if err := s.stopRestoreHelpers(j); err != nil {
		return s.restoreFailure(j, err)
	}
	if err := s.runRestoreSQL(j, "stop-db-helpers", stopRestoreDatabaseHelpers); err != nil {
		return s.restoreFailure(j, err)
	}
	state, err := s.restoreDatabaseState(j)
	if err != nil {
		return s.restoreFailure(j, err)
	}
	var cleanup string
	if j.Phase == "committed" {
		if state.Live == nil || state.Live.OID != j.StagedOID || state.Staged != nil ||
			state.Old != nil && state.Old.OID != j.OriginalOID {
			return s.restoreFailure(j, fmt.Errorf("database identities do not match the committed restore"))
		}
		if state.Old != nil {
			cleanup = dropPreviousDatabase
		}
	} else {
		if state.Old != nil || j.OriginalOID != "" && (state.Live == nil || state.Live.OID != j.OriginalOID) {
			return s.restoreFailure(j, fmt.Errorf("database identities do not match the recovered restore"))
		}
		if state.Staged != nil {
			if state.Staged.Tag != j.tag() || j.StagedOID != "" && state.Staged.OID != j.StagedOID {
				return s.restoreFailure(j, fmt.Errorf("staged database ownership cannot be confirmed; %s was retained", j.stagedDatabase()))
			}
			cleanup = dropStagedDatabase
		}
	}
	if cleanup != "" {
		if err := s.runRestoreSQL(j, "cleanup-db", cleanup); err != nil {
			return s.restoreFailure(j, err)
		}
	}
	volumes := []string{j.stageVolume()}
	if j.WithFiles {
		volumes = append(volumes, j.oldVolume())
	}
	for _, name := range volumes {
		exists, err := s.restoreVolumeExists(name, map[string]string{
			RestoreLabel: j.ID, "com.docker.compose.project": j.Project,
		})
		if err != nil {
			return s.restoreFailure(j, err)
		}
		if exists {
			if err := s.run.Run("docker", "volume", "rm", name); err != nil {
				return s.restoreFailure(j, err)
			}
		}
	}
	if err := os.Remove(s.restoreJournalPath()); err != nil {
		return err
	}
	s.logln("CARE started successfully; the completed restore's recovery copies were removed.")
	return nil
}
