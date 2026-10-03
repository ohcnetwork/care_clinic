package plugins

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/atomicfile"
)

const pendingFile = "plugins-pending.json"

// StagePlugins never changes the inputs used by a running clinic or by Start.
func (m *Manager) StagePlugins(list []Plugin) error {
	list, err := Prepare(list)
	if err != nil {
		return err
	}
	data, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return atomicfile.WritePrivate(filepath.Join(m.Dir, pendingFile), data)
}

func (m *Manager) PendingPlugins() ([]Plugin, error) {
	data, err := os.ReadFile(filepath.Join(m.Dir, pendingFile))
	if err != nil {
		return nil, err
	}
	var list []Plugin
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	return Prepare(list)
}

func (m *Manager) DiscardPending() error {
	err := os.Remove(filepath.Join(m.Dir, pendingFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
