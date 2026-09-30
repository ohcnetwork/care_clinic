package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/health"
)

type Backup struct {
	DBDump       string `json:"db_dump"`
	FilesArchive string `json:"files_archive"`
	Label        string `json:"label"`
	Manual       bool   `json:"manual"`
	Encrypted    bool   `json:"encrypted"`
	SizeBytes    int64  `json:"size_bytes"`
}

var dumpRe = regexp.MustCompile(`^care-(?:manual-)?(\d{8}-\d{6})\.dump(?:\.enc)?$`)

var safeName = regexp.MustCompile(`^(?:care-(?:manual-)?\d{8}-\d{6}\.dump(?:\.enc)?|files-(?:manual-)?\d{8}-\d{6}\.tar\.gz(?:\.enc)?)$`)

var filesRe = regexp.MustCompile(`^files-((?:manual-)?\d{8}-\d{6})\.tar\.gz(?:\.enc)?$`)

func SetName(dbDump string) string {
	name := strings.TrimPrefix(dbDump, "care-")
	name = strings.TrimSuffix(name, ".enc")
	return strings.TrimSuffix(name, ".dump")
}

func FilesCandidates(dbDump string) []string {
	set := SetName(dbDump)
	return []string{"files-" + set + ".tar.gz.enc", "files-" + set + ".tar.gz"}
}

func (s *Store) ListBackups() ([]Backup, error) {
	dir := s.BackupDir
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Backup{}, nil
		}
		return nil, err
	}
	files := map[string]bool{}
	for _, en := range entries {
		m := filesRe.FindStringSubmatch(en.Name())
		if m == nil {
			continue
		}
		info, err := en.Info()
		if err != nil {
			return nil, err
		}
		if info.Mode().IsRegular() {
			files[en.Name()] = true
		}
	}
	out := []Backup{}
	for _, en := range entries {
		m := dumpRe.FindStringSubmatch(en.Name())
		if m == nil {
			continue
		}
		info, err := en.Info()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		ts := m[1]
		if _, err := time.Parse("20060102-150405", ts); err != nil {
			continue
		}
		b := Backup{
			DBDump:    en.Name(),
			Manual:    strings.HasPrefix(en.Name(), "care-manual-"),
			Encrypted: strings.HasSuffix(en.Name(), ".enc"),
			SizeBytes: info.Size(),
		}
		candidates := FilesCandidates(en.Name())
		if !b.Encrypted {
			candidates[0], candidates[1] = candidates[1], candidates[0]
		}
		for _, name := range candidates {
			if files[name] {
				b.FilesArchive = name
				b.Encrypted = b.Encrypted || strings.HasSuffix(name, ".enc")
				break
			}
		}
		b.Label = backupLabel(ts, b.Manual, b.FilesArchive != "", b.Encrypted)
		out = append(out, b)
	}

	tsOf := func(name string) string {
		if m := dumpRe.FindStringSubmatch(name); m != nil {
			return m[1]
		}
		return name
	}
	sort.Slice(out, func(i, j int) bool { return tsOf(out[i].DBDump) > tsOf(out[j].DBDump) })
	return out, nil
}

func backupLabel(ts string, manual, withFiles, encrypted bool) string {
	when := ts
	if t, err := time.Parse("20060102-150405", ts); err == nil {
		when = t.Format("2006-01-02 15:04")
	}
	kind := "daily"
	if manual {
		kind = "manual"
	}
	scope := "DB only"
	if withFiles {
		scope = "DB + files"
	}
	label := fmt.Sprintf("%s - %s - %s", when, kind, scope)
	if encrypted {
		label += " - encrypted"
	}
	return label
}

func (s *Store) Restore(dbDump, filesArchive, recoveryFile string) error {
	return s.RestoreFrom(s.BackupDir, dbDump, filesArchive, recoveryFile)
}

func (s *Store) RestoreFrom(srcDir, dbDump, filesArchive, recoveryFile string) error {
	if srcDir == "" {
		srcDir = s.BackupDir
	}
	srcDir, err := filepath.Abs(srcDir)
	if err != nil {
		return err
	}
	m := dumpRe.FindStringSubmatch(dbDump)
	if m == nil {
		return fmt.Errorf("not a database dump: %q", dbDump)
	}
	if _, err := time.Parse("20060102-150405", m[1]); err != nil {
		return fmt.Errorf("invalid backup timestamp: %q", dbDump)
	}
	if err := s.mustExist(srcDir, dbDump); err != nil {
		return err
	}
	if filesArchive != "" {
		f := filesRe.FindStringSubmatch(filesArchive)
		if f == nil {
			return fmt.Errorf("not a files archive: %q", filesArchive)
		}
		if f[1] != SetName(dbDump) {
			return fmt.Errorf("the database and files backups must come from the same backup")
		}
		if err := s.mustExist(srcDir, filesArchive); err != nil {
			return err
		}
	}

	var key string
	encrypted := strings.HasSuffix(dbDump, ".enc") || strings.HasSuffix(filesArchive, ".enc")
	if encrypted {
		if recoveryFile == "" {
			return fmt.Errorf("select the backup recovery file saved when this clinic was set up")
		}
		key, err = filepath.Abs(recoveryFile)
		if err != nil {
			return err
		}
		if _, err := ReadRecoveryFile(key); err != nil {
			return err
		}
	}
	if pending, err := s.readRestoreJournal(); err != nil {
		return err
	} else if pending != nil {
		return fmt.Errorf("restore %s still has recovery data; start CARE to recover or finish it before restoring another backup", pending.ID)
	}
	if s.EnsureRestoreImages == nil || s.Migrate == nil || s.BackendImage == "" {
		return fmt.Errorf("staged restore images and migrations are not configured")
	}
	if err := s.EnsureRestoreImages(); err != nil {
		return err
	}
	j, err := s.newRestoreJournal(filesArchive != "")
	if err != nil {
		return err
	}
	if err := s.writeRestoreJournal(j); err != nil {
		return err
	}
	if err := s.prepareRestore(j, srcDir, dbDump, filesArchive, key); err != nil {
		return s.restoreFailure(j, err)
	}
	s.logln("Stopping and checking all CARE writers, including backups and uploads...")
	if err := s.stopRestoreWriters(); err != nil {
		return s.restoreFailure(j, err)
	}
	if j.WithFiles {
		if err := s.snapshotRestoreFiles(j); err != nil {
			return s.restoreFailure(j, err)
		}
	}
	j.Phase = "prepared"
	if err := s.writeRestoreJournal(j); err != nil {
		return s.restoreFailure(j, err)
	}
	if j.WithFiles {
		if err := s.replaceRestoreFiles(j, false); err != nil {
			return s.restoreFailure(j, err)
		}
	}
	if err := s.swapRestoreDatabase(j, false); err != nil {
		return s.restoreFailure(j, err)
	}
	j.Phase = "committed"
	if err := s.writeRestoreJournal(j); err != nil {
		return s.restoreFailure(j, err)
	}
	s.logln("Bringing CARE back up...")
	if err := s.dc("up", "-d", "--wait", "--wait-timeout", "300"); err != nil {
		return s.restoreFailure(j, err)
	}
	s.logln("Waiting for CARE to become healthy...")
	if err := health.Wait(s.Log, 3*time.Minute); err != nil {
		return s.restoreFailure(j, err)
	}
	if err := s.FinishRestore(); err != nil {
		return err
	}
	s.logln("")
	s.logln("Restore complete -> https://" + s.Host + "/")
	return nil
}

func (s *Store) mustExist(dir, name string) error {
	info, err := os.Lstat(filepath.Join(dir, name))
	if err != nil {
		return fmt.Errorf("cannot read backup %s: %w", name, err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return fmt.Errorf("backup must be a nonempty regular file, not a directory or link: %s", name)
	}
	return nil
}

func (s *Store) prepareRestore(j *restoreJournal, srcDir, dump, archive, key string) error {
	s.logln("Copying and fully validating the backup without changing the current database or files...")
	if err := s.createRestoreVolume(j, j.stageVolume()); err != nil {
		return err
	}
	script := "set -eu\numask 077\n" + stageBackupFile(dump, "database.dump")
	if archive != "" {
		script += stageBackupFile(archive, "files.tar.gz")
	}
	script += "pg_restore --exit-on-error --file=/dev/null /restore/database.dump\nsync\n"
	mounts := []string{"--mount", restoreMount("volume", j.stageVolume(), "/restore", false),
		"--mount", restoreMount("bind", srcDir, "/backups", true)}
	if key != "" {
		mounts = append(mounts, "--mount", restoreMount("bind", key, "/restore-key.pem", true))
	}
	args := s.restoreHelperArgs(j, "preflight", "none", mounts...)
	args = append(args, s.Image, "sh", "-c", script)
	if err := s.run.Run("docker", args...); err != nil {
		return fmt.Errorf("backup copy, decryption or full dump validation failed: %w", err)
	}
	if j.WithFiles {
		args := s.restoreHelperArgs(j, "extract", "none",
			"--mount", restoreMount("volume", j.stageVolume(), "/restore", false))
		args = append(args, "--read-only", "--user", "0:0", "--entrypoint", "python", s.BackendImage, "-B", "-c", validateRestoreFiles)
		if err := s.run.Run("docker", args...); err != nil {
			return fmt.Errorf("files archive validation or staged extraction failed: %w", err)
		}
	}
	if err := s.startRestoreDB(); err != nil {
		return err
	}
	state, err := s.restoreDatabaseState(j)
	if err != nil {
		return err
	}
	if state.Live == nil || state.Staged != nil || state.Old != nil {
		return fmt.Errorf("the current database is missing or a staging database name is already in use")
	}
	j.OriginalOID = state.Live.OID
	if err := s.writeRestoreJournal(j); err != nil {
		return err
	}
	s.logln("Restoring the replacement into a separate database...")
	if err := s.runRestoreSQL(j, "stage-db", createRestoreDatabase,
		"--mount", restoreMount("volume", j.stageVolume(), "/restore", true)); err != nil {
		return fmt.Errorf("staged database restore failed: %w", err)
	}
	s.logln("Applying migrations only to the staged database...")
	if err := s.Migrate(j.stagedDatabase(), j.ID); err != nil {
		return fmt.Errorf("staged database migration failed: %w", err)
	}
	state, err = s.restoreDatabaseState(j)
	if err != nil {
		return err
	}
	if state.Live == nil || state.Live.OID != j.OriginalOID || state.Staged == nil ||
		state.Staged.Tag != j.tag() || state.Old != nil {
		return fmt.Errorf("database identity changed while preparing the restore")
	}
	j.StagedOID = state.Staged.OID
	return s.writeRestoreJournal(j)
}

func stageBackupFile(name, target string) string {
	script := "[ -f /backups/" + name + " ] && [ ! -L /backups/" + name + " ]\n"
	if strings.HasSuffix(name, ".enc") {
		return script + "cp /backups/" + name + " /restore/" + target + ".enc\n" +
			"openssl cms -decrypt -binary -inform DER -in /restore/" + target + ".enc -out /restore/" + target +
			" -inkey /restore-key.pem -passin pass:\n"
	}
	return script + "cp /backups/" + name + " /restore/" + target + "\n"
}
