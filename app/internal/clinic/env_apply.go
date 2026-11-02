package clinic

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/compose-spec/compose-go/v2/dotenv"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/atomicfile"
)

// EnvChange is the complete new text of one environment file that staff edit.
type EnvChange struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// EnvFilePath maps the name the interface uses to the file in the install directory.
func EnvFilePath(installDir, name string) (string, error) {
	switch name {
	case "backend", "frontend":
		return filepath.Join(installDir, name+".env"), nil
	}
	return "", errors.New("unknown env file: " + name)
}

// writeEnvFile replaces an environment file. Tests swap it to simulate a failing disk.
var writeEnvFile = func(path string, data []byte) error {
	return atomicfile.Write(path, data, 0o600)
}

type envFile struct {
	name, path     string
	previous, next []byte
}

// ApplyEnvFiles replaces environment files and starts CARE on them. CARE is
// stopped before any file changes. When it does not start on the new settings,
// the previous files are put back and CARE is started on them, so the files and
// the running clinic never disagree. Files are applied even when their text is
// unchanged, which lets staff retry a change that was saved but never applied.
func (e *Clinic) ApplyEnvFiles(changes []EnvChange) error {
	files, err := e.planEnvFiles(changes)
	if err != nil {
		return err
	}
	e.logln("Applying the new settings - CARE stops, then starts again on them (patient data is kept)...")
	if err := e.StopStack(); err != nil {
		return fmt.Errorf("nothing was changed: %w", err)
	}
	for i, f := range files {
		if err := writeEnvFile(f.path, f.next); err != nil {
			// A failed write leaves that file as it was, so only the earlier ones are put back.
			return e.revertEnvFiles(fmt.Errorf("couldn't save the %s settings: %w", f.name, err), files[:i])
		}
	}
	if err := e.startStopped(nil); err != nil {
		return e.revertEnvFiles(err, files)
	}
	e.logln("The new settings are applied.")
	return nil
}

// CheckEnvFiles reports whether ApplyEnvFiles would accept the request. It
// touches neither CARE nor any file, so the interface can turn a bad request
// away before a job starts.
func (e *Clinic) CheckEnvFiles(changes []EnvChange) error {
	_, err := e.planEnvFiles(changes)
	return err
}

// planEnvFiles checks everything that can be checked while CARE is still
// running, so a bad request never stops the clinic.
func (e *Clinic) planEnvFiles(changes []EnvChange) ([]envFile, error) {
	if len(changes) == 0 {
		return nil, errors.New("there are no settings to apply")
	}
	files := make([]envFile, 0, len(changes))
	named := map[string]bool{}
	for _, change := range changes {
		path, err := EnvFilePath(e.InstallDir, change.Name)
		if err != nil {
			return nil, err
		}
		if named[change.Name] {
			return nil, fmt.Errorf("the %s settings were given twice", change.Name)
		}
		named[change.Name] = true
		if _, err := dotenv.Parse(strings.NewReader(change.Content)); err != nil {
			return nil, fmt.Errorf("the %s settings have invalid syntax; use KEY=value assignments and balanced quotes", change.Name)
		}
		previous, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("couldn't read the current %s settings: %w", change.Name, err)
		}
		files = append(files, envFile{name: change.Name, path: path, previous: previous, next: []byte(change.Content)})
	}
	return files, nil
}

// revertEnvFiles goes back to the previous settings after the new ones could
// not be applied: it puts back the files that were written and starts CARE on
// them. CARE is stopped again first, because the failed start may have left
// containers running on the new settings.
func (e *Clinic) revertEnvFiles(cause error, written []envFile) error {
	e.logln("The new settings were not applied; going back to the previous settings...")
	stopErr := e.StopStack()
	var restoreErr error
	for _, f := range written {
		if err := writeEnvFile(f.path, f.previous); err != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("couldn't put back the %s settings: %w", f.name, err))
		}
	}
	if restoreErr != nil {
		return errors.Join(cause, restoreErr)
	}
	if stopErr != nil {
		return errors.Join(cause, fmt.Errorf("the previous settings are back, but CARE could not be stopped to start it on them: %w", stopErr))
	}
	if err := e.startStopped(nil); err != nil {
		return errors.Join(cause, fmt.Errorf("the previous settings are back, but CARE could not start on them: %w", err))
	}
	return fmt.Errorf("the new settings were not applied and CARE is running on the previous ones: %w", cause)
}
