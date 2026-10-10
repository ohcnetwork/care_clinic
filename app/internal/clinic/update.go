package clinic

import (
	"errors"
	"fmt"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/compose"
	"github.com/ohcnetwork/care_desktop/app/internal/lines"
)

type LineInfo struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	SupportedUntil string `json:"supported_until,omitempty"`
	Expired        bool   `json:"expired"`
}

type ChannelStatus struct {
	Line            LineInfo   `json:"line"`
	Backend         string     `json:"backend"`
	Frontend        string     `json:"frontend"`
	PendingBackend  string     `json:"pending_backend"`
	PendingFrontend string     `json:"pending_frontend"`
	PendingLine     string     `json:"pending_line,omitempty"`
	NewerLines      []LineInfo `json:"newer_lines"`
	Recommended     string     `json:"recommended,omitempty"`
}

func lineInfo(l lines.Line, now time.Time) LineInfo {
	title := l.Title
	if title == "" {
		title = l.ID
	}
	return LineInfo{ID: l.ID, Title: title, SupportedUntil: l.SupportedUntil, Expired: l.Expired(now)}
}

// ChannelStatus reports the clinic's release line and versions using the
// last downloaded list of lines, so it never waits on the network.
func (e *Clinic) ChannelStatus() ChannelStatus {
	lock := compose.ReadLock(e.InstallDir)
	id := e.Builder().LineID()
	out := ChannelStatus{
		Line:        LineInfo{ID: id, Title: id},
		Backend:     lock.Backend.Current.Commit,
		Frontend:    lock.Frontend.Current.Commit,
		PendingLine: lock.NextLine,
		NewerLines:  []LineInfo{},
	}
	if lock.Backend.Next != nil {
		out.PendingBackend = lock.Backend.Next.Commit
	}
	if lock.Frontend.Next != nil {
		out.PendingFrontend = lock.Frontend.Next.Commit
	}
	m, err := lines.Cached(e.InstallDir)
	if err != nil {
		return out
	}
	now := time.Now()
	if l, ok := m.Line(id); ok {
		out.Line = lineInfo(l, now)
	}
	for _, l := range m.Newer(id) {
		if l.SupportsApp(e.Pins.AppVersion) == nil {
			out.NewerLines = append(out.NewerLines, lineInfo(l, now))
		}
	}
	if m.IsNewer(id, m.Recommended) {
		out.Recommended = m.Recommended
	}
	return out
}

// LineAllowsApp reports why an app version cannot run this clinic's release
// line, using the cached list of lines.
func (e *Clinic) LineAllowsApp(version string) error {
	m, err := lines.Cached(e.InstallDir)
	if err != nil {
		return nil
	}
	l, ok := m.Line(e.Builder().LineID())
	if !ok {
		return nil
	}
	return l.SupportsApp(version)
}

func (e *Clinic) CheckForUpdate() (compose.Update, error) {
	return e.Builder().PrepareUpdate()
}

// UpgradeLine moves the clinic to a newer CARE release line: it downloads
// both images, takes a safety backup and migrates the database. Going back
// to an older line is only possible by restoring a backup.
func (e *Clinic) UpgradeLine(id string) error {
	if !e.Backups().BackupEncryptionOn() {
		return errors.New("this install cannot write encrypted backups, so it cannot change release line - " +
			"recover the backup encryption certificate first")
	}
	b := e.Builder()
	if b.Waiting().Any() {
		if err := b.DeclineUpdate(); err != nil {
			return err
		}
	}
	e.logln("Downloading CARE release line " + id + "...")
	if _, err := b.StageLine(id); err != nil {
		return err
	}
	return e.ApplyUpdate()
}

func (e *Clinic) DeclineUpdate() error { return e.Builder().DeclineUpdate() }

func (e *Clinic) ApplyUpdate() error {
	if err := e.Backups().RecoverRestore(); err != nil {
		return err
	}
	b := e.Builder()
	waiting := b.Waiting()
	if !waiting.Any() {
		return errors.New("there is no prepared update to apply")
	}
	if waiting.Backend != "" {
		if err := e.backupBeforeUpdate(); err != nil {
			return err
		}
		if err := e.stopWorkers(); err != nil {
			return err
		}
	}
	applied, err := b.ApplyPending()
	if err != nil {
		return err
	}
	if !applied.Any() {
		return errors.New("the prepared update is no longer available - it will be built again on the next check")
	}
	if applied.Backend != "" {
		if err := e.dc("up", "-d", "--wait", "--wait-timeout", "300", "backend"); err != nil {
			return fmt.Errorf("the updated backend did not start; workers and the scheduler remain stopped: %w", err)
		}
		e.logln("Applying database migrations...")
		if err := e.migrate(); err != nil {
			return err
		}
	}
	if err := e.dc("up", "-d", "--wait", "--wait-timeout", "300"); err != nil {
		return err
	}
	b.PruneDangling()
	e.logln("CARE is up to date.")
	return nil
}

func (e *Clinic) applyStagedUpdate() error {
	b := e.Builder()
	waiting := b.Waiting()
	if !waiting.Any() {
		return nil
	}
	if !e.Backups().BackupEncryptionOn() {
		e.logln("A CARE update is ready, but this install cannot write encrypted backups, " +
			"so it was not applied. Recover the backup encryption certificate before applying updates.")
		return nil
	}
	if waiting.Backend != "" {
		if err := e.backupBeforeUpdate(); err != nil {
			e.logln("The CARE update was not applied: " + err.Error())
			return nil
		}
	}
	_, err := b.ApplyPending()
	return err
}

func (e *Clinic) backupBeforeUpdate() error {
	if !e.Backups().BackupEncryptionOn() {
		return errors.New("this install cannot write encrypted backups, so the update was not applied - " +
			"recover the backup encryption certificate first")
	}
	ts := time.Now().Format("20060102-150405")
	e.logln("Taking a safety backup before the update changes the database...")
	if err := e.run(nil, "docker", "compose", "run", "--rm", "-T", "backup", "once", "pre-update-"+ts); err != nil {
		return fmt.Errorf("couldn't take a safety backup, so the update was not applied: %w", err)
	}
	e.logln("Safety backup written: care-pre-update-" + ts + ".dump.enc")
	return nil
}
