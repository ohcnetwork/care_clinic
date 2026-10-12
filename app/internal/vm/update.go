package vm

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// An engine update replaces the engine bundle and the host tools as one
// transaction. Everything is downloaded, unpacked and verified into
// engine-update/new while the clinic keeps running. Activation moves the
// previous tools to engine-update/old and the new ones into place; the
// previous engine bundle stays on disk until the update is committed, so
// a failed or interrupted update can put the previous engine back. The
// data disk is never copied or reverted.

// Engine update phases persisted in the update record.
const (
	UpdateActivating  = "activating"
	UpdateVerifying   = "verifying"
	UpdateRollingBack = "rolling-back"
	UpdateFailed      = "failed"
)

// EngineUpdate is the persisted update record.
type EngineUpdate struct {
	Phase string `json:"phase"`
	From  string `json:"from"`
	To    string `json:"to"`
	// Missing lists the tools that did not exist before activation, so
	// rolling back removes them instead of looking for a backup.
	Missing []string  `json:"missing,omitempty"`
	Error   string    `json:"error,omitempty"`
	Started time.Time `json:"started"`
}

// Interrupted reports whether the record belongs to an update that stopped
// part-way and still has to be rolled back.
func (u EngineUpdate) Interrupted() bool { return u.Phase != UpdateFailed }

// EngineFiles are the verified downloads of one engine update.
type EngineFiles struct {
	Engine, Docker, Compose, QEMU string
}

// StagedUpdate is an update that is unpacked and verified but not active.
type StagedUpdate struct {
	Manifest Manifest
	From     string
}

func updateDir() string  { return filepath.Join(StateDir(), "engine-update") }
func recordFile() string { return filepath.Join(updateDir(), "record.json") }
func newDir() string     { return filepath.Join(updateDir(), "new") }
func oldDir() string     { return filepath.Join(updateDir(), "old") }

type tool struct {
	name string
	live func() string
}

func updateTools() []tool {
	t := []tool{{"docker", dockerCLI}, {"docker-compose", composePlugin}}
	if runtime.GOOS == "windows" {
		t = append(t, tool{"qemu", qemuDir})
	}
	return t
}

// PendingEngineUpdate returns the update record, if any.
func PendingEngineUpdate() (EngineUpdate, bool, error) {
	var u EngineUpdate
	b, err := os.ReadFile(recordFile())
	if errors.Is(err, os.ErrNotExist) {
		return u, false, nil
	}
	if err != nil {
		return u, false, err
	}
	if err := json.Unmarshal(b, &u); err != nil {
		return u, false, fmt.Errorf("CARE engine update record: %w", err)
	}
	return u, true, nil
}

func writeRecord(u EngineUpdate) error {
	if err := os.MkdirAll(updateDir(), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(u, "", "  ")
	return writeAtomic(recordFile(), append(b, '\n'))
}

func writeAtomic(p string, b []byte) error {
	tmp := p + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// StageEngineUpdate unpacks and verifies every file of an update without
// touching the installed engine, its tools or a running VM. A failed update
// left from an earlier attempt is cleared; an interrupted one must be
// rolled back first.
func StageEngineUpdate(f EngineFiles) (*StagedUpdate, error) {
	if u, ok, err := PendingEngineUpdate(); err != nil {
		return nil, err
	} else if ok && u.Interrupted() {
		return nil, errors.New("an earlier CARE engine update was interrupted and has not been rolled back yet")
	}
	if err := os.RemoveAll(updateDir()); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(newDir(), 0o700); err != nil {
		return nil, err
	}
	s := &StagedUpdate{}
	if m, err := Installed(); err == nil {
		s.From = m.Version
	}
	m, err := unpackEngine(f.Engine, filepath.Join(newDir(), "engine"))
	if err != nil {
		s.Discard()
		return nil, err
	}
	s.Manifest = m
	if err := stageDockerCLI(f.Docker); err != nil {
		s.Discard()
		return nil, fmt.Errorf("docker command: %w", err)
	}
	if err := installFile(f.Compose, filepath.Join(newDir(), "docker-compose")); err != nil {
		s.Discard()
		return nil, fmt.Errorf("Docker Compose: %w", err)
	}
	if runtime.GOOS == "windows" {
		if err := stageQEMU(f.QEMU, filepath.Join(newDir(), "qemu")); err != nil {
			s.Discard()
			return nil, fmt.Errorf("QEMU: %w", err)
		}
	}
	return s, nil
}

// Discard removes a staged update that was never activated.
func (s *StagedUpdate) Discard() { _ = os.RemoveAll(updateDir()) }

func unpackEngine(archive, dest string) (Manifest, error) {
	if err := extract(archive, dest, 0); err != nil {
		return Manifest{}, err
	}
	m, err := readManifest(dest)
	if err != nil {
		return m, err
	}
	for name, want := range m.Files {
		got, err := fileSHA256(filepath.Join(dest, name))
		if err != nil {
			return m, err
		}
		if !strings.EqualFold(got, want) {
			return m, fmt.Errorf("engine file %s is corrupt", name)
		}
	}
	return m, nil
}

func stageDockerCLI(archive string) error {
	tmp := filepath.Join(newDir(), ".docker")
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := extract(archive, tmp, 0); err != nil {
		return err
	}
	return installFile(filepath.Join(tmp, "docker", exe("docker")), filepath.Join(newDir(), "docker"))
}

func stageQEMU(archive, dest string) error {
	if err := extract(archive, dest, 0); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dest, "qemu-system-x86_64.exe")); err != nil {
		return errors.New("QEMU bundle has no qemu-system-x86_64.exe")
	}
	return nil
}

// Activate makes the staged engine and tools the installed ones. The VM must
// be stopped. The update record is written first, so an interruption at any
// point is rolled back on the next launch.
func (s *StagedUpdate) Activate() error {
	u := EngineUpdate{Phase: UpdateActivating, From: s.From, To: s.Manifest.Version, Started: time.Now().UTC()}
	for _, t := range updateTools() {
		if _, err := os.Lstat(t.live()); errors.Is(err, os.ErrNotExist) {
			u.Missing = append(u.Missing, t.name)
		}
	}
	if err := writeRecord(u); err != nil {
		return err
	}
	if err := os.MkdirAll(oldDir(), 0o700); err != nil {
		return err
	}
	for _, t := range updateTools() {
		live := t.live()
		if !contains(u.Missing, t.name) {
			if err := os.Rename(live, filepath.Join(oldDir(), t.name)); err != nil {
				return err
			}
		}
		if err := os.MkdirAll(filepath.Dir(live), 0o755); err != nil {
			return err
		}
		if err := os.Rename(filepath.Join(newDir(), t.name), live); err != nil {
			return err
		}
	}
	if u.To != u.From {
		dest := filepath.Join(engineDir0(), u.To)
		if err := os.RemoveAll(dest); err != nil {
			return err
		}
		if err := os.Rename(filepath.Join(newDir(), "engine"), dest); err != nil {
			return err
		}
		if err := writeAtomic(currentFile(), []byte(u.To+"\n")); err != nil {
			return err
		}
	}
	u.Phase = UpdateVerifying
	return writeRecord(u)
}

// CommitEngineUpdate keeps the active engine and deletes the previous
// engine bundle and tools.
func CommitEngineUpdate() error {
	u, ok, err := PendingEngineUpdate()
	if err != nil {
		return err
	}
	if !ok || u.Phase != UpdateVerifying {
		return errors.New("there is no CARE engine update to commit")
	}
	if err := os.Remove(recordFile()); err != nil {
		return err
	}
	pruneEngines(u.To)
	return os.RemoveAll(updateDir())
}

// RollbackEngineUpdate puts the previous engine and tools back and records
// the update as failed with cause. The VM must be stopped. It is safe to run
// again after an interruption: every step checks what is still to be undone.
func RollbackEngineUpdate(cause string) error {
	u, ok, err := beginRollback(cause)
	if err != nil || !ok {
		return err
	}
	for _, t := range updateTools() {
		backup := filepath.Join(oldDir(), t.name)
		switch _, err := os.Lstat(backup); {
		case err == nil:
			if err := os.RemoveAll(t.live()); err != nil {
				return err
			}
			if err := os.Rename(backup, t.live()); err != nil {
				return err
			}
		case !errors.Is(err, os.ErrNotExist):
			return err
		case contains(u.Missing, t.name):
			if err := os.RemoveAll(t.live()); err != nil {
				return err
			}
		}
	}
	if u.To != u.From {
		var err error
		if u.From != "" {
			err = writeAtomic(currentFile(), []byte(u.From+"\n"))
		} else if err = os.Remove(currentFile()); errors.Is(err, os.ErrNotExist) {
			err = nil
		}
		if err != nil {
			return err
		}
		if err := os.RemoveAll(filepath.Join(engineDir(), u.To)); err != nil {
			return err
		}
	}
	_ = os.RemoveAll(newDir())
	_ = os.RemoveAll(oldDir())
	u.Phase = UpdateFailed
	return writeRecord(u)
}

// BeginEngineRollback records why an update is being rolled back before the
// new engine is stopped, so the first cause is kept if stopping fails.
func BeginEngineRollback(cause string) error {
	_, _, err := beginRollback(cause)
	return err
}

func beginRollback(cause string) (EngineUpdate, bool, error) {
	u, ok, err := PendingEngineUpdate()
	if err != nil || !ok || !u.Interrupted() {
		return u, false, err
	}
	if u.Error == "" {
		u.Error = cause
	}
	u.Phase = UpdateRollingBack
	return u, true, writeRecord(u)
}

// ClearFailedEngineUpdate forgets a failed update so it can be retried.
func ClearFailedEngineUpdate() error {
	u, ok, err := PendingEngineUpdate()
	if err != nil || !ok || u.Interrupted() {
		return err
	}
	return os.RemoveAll(updateDir())
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
