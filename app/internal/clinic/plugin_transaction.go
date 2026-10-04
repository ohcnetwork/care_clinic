package clinic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/compose"
	"github.com/ohcnetwork/care_desktop/app/internal/plugins"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/atomicfile"
)

const pluginRecoveryFile = "plugin-recovery.json"

var pluginSnapshotFiles = []string{"backend.env", "plugins.json", compose.LockFile}

type pluginFile struct {
	Data   []byte `json:"data"`
	Exists bool   `json:"exists"`
}

type pluginRecovery struct {
	Version      int                   `json:"version"`
	Files        map[string]pluginFile `json:"files"`
	BackendImage string                `json:"backend_image"`
	ImageID      string                `json:"image_id"`
	Containers   []pluginContainer     `json:"containers"`
	FrontendRows json.RawMessage       `json:"frontend_rows"`
}

func (e *Clinic) PendingPluginRecovery() (bool, error) {
	_, err := e.readPluginRecovery()
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return true, err
}

func (e *Clinic) readPluginRecovery() (*pluginRecovery, error) {
	data, err := os.ReadFile(filepath.Join(e.InstallDir, pluginRecoveryFile))
	if err != nil {
		return nil, err
	}
	var state pluginRecovery
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("could not read plugin recovery state: %w", err)
	}
	if state.Version != 1 || state.BackendImage == "" || !strings.HasPrefix(state.ImageID, "sha256:") {
		return nil, errors.New("invalid plugin recovery state; keep the recovery file and contact support")
	}
	for _, name := range pluginSnapshotFiles {
		if _, ok := state.Files[name]; !ok {
			return nil, errors.New("incomplete plugin recovery configuration; keep the recovery file and contact support")
		}
	}
	if !state.Files["backend.env"].Exists || !json.Valid(state.FrontendRows) || len(state.Containers) == 0 {
		return nil, errors.New("incomplete plugin recovery runtime; keep the recovery file and contact support")
	}
	return &state, nil
}

func (e *Clinic) ApplyPlugins() error {
	return e.applyPlugins(e.applyPluginCandidate)
}

func (e *Clinic) applyPlugins(candidate func() error) error {
	if pending, err := e.PendingPluginRecovery(); err != nil {
		return err
	} else if pending {
		return errors.New("a plugin rollback is unfinished; start CARE to recover it before changing plugins")
	}
	if err := e.Backups().RecoverRestore(); err != nil {
		return err
	}
	manager := plugins.New(e.InstallDir)
	list, err := manager.PendingPlugins()
	if err != nil {
		return fmt.Errorf("plugin changes are not staged; save the plugin settings again: %w", err)
	}
	// A failed attempt must not be silently retried by a later Start or Apply.
	if err := manager.DiscardPending(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	previousContext := e.ctx
	e.ctx = ctx
	defer func() { e.ctx = previousContext }()

	state, err := e.preparePluginRecovery(list)
	if err != nil {
		return fmt.Errorf("plugin changes were not applied: %w", err)
	}
	applyErr := manager.SavePlugins(list)
	if applyErr == nil {
		applyErr = candidate()
	}
	if applyErr == nil {
		if err := e.finishPluginRecovery(state); err != nil {
			applyErr = fmt.Errorf("could not commit plugin changes: %w", err)
		} else {
			e.logln("Plugins applied. CARE is online.")
			return nil
		}
	}
	e.logln("Plugin loading failed. Restoring the previous plugin configuration and backend image...")
	// Recovery has its own deadline, even if the build exhausted its deadline.
	if recoveryErr := e.recoverPlugins(state); recoveryErr != nil {
		return fmt.Errorf("plugin loading failed (%v). Plugin rollback is unfinished; CARE could not be recovered. Start CARE to retry recovery: %w", applyErr, recoveryErr)
	}
	return fmt.Errorf("plugin loading failed; previous settings were restored and CARE is back online: %w", applyErr)
}

func (e *Clinic) preparePluginRecovery(list []plugins.Plugin) (*pluginRecovery, error) {
	state := &pluginRecovery{Version: 1, Files: map[string]pluginFile{}, BackendImage: e.Pins.BackendImage}
	for _, name := range pluginSnapshotFiles {
		data, err := os.ReadFile(filepath.Join(e.InstallDir, name))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if name == "backend.env" && err != nil {
			return nil, err
		}
		state.Files[name] = pluginFile{Data: data, Exists: err == nil}
	}
	containers, err := e.pluginContainers()
	if err != nil {
		return nil, err
	}
	running := false
	for _, c := range containers {
		if c.Status == "running" {
			running = true
		}
		if c.Service == "backend" {
			state.ImageID = c.Image
		}
	}
	if !running {
		return nil, errors.New("start CARE successfully before changing plugins; the current settings were not changed")
	}
	state.Containers = containers
	e.pluginBaseline = containers
	if err := e.waitPluginRuntime(); err != nil {
		return nil, fmt.Errorf("the existing clinic is not healthy; start CARE successfully before changing plugins: %w", err)
	}
	state.FrontendRows, err = e.snapshotPluginRows(list)
	if err != nil {
		return nil, err
	}
	if state.ImageID == "" {
		state.ImageID, err = e.capture("docker", "image", "inspect", "--format", "{{.Id}}", state.BackendImage)
		if err != nil {
			return nil, fmt.Errorf("cannot preserve the previous backend image: %w", err)
		}
	}
	if !strings.HasPrefix(state.ImageID, "sha256:") {
		return nil, errors.New("cannot identify the previous backend image")
	}
	// Pin the image so replacing the normal tag cannot make it dangling.
	if err := e.run(nil, "docker", "image", "tag", state.ImageID, state.BackendImage+"-plugin-rollback"); err != nil {
		return nil, err
	}
	data, err := json.Marshal(state)
	if err == nil {
		err = atomicfile.WritePrivate(filepath.Join(e.InstallDir, pluginRecoveryFile), data)
	}
	if err != nil {
		cleanupErr := e.run(nil, "docker", "image", "rm", state.BackendImage+"-plugin-rollback")
		return nil, errors.Join(err, cleanupErr)
	}
	return state, nil
}

// RecoverPlugins is used before normal startup can build from uncommitted inputs.
func (e *Clinic) RecoverPlugins() (bool, error) {
	state, err := e.readPluginRecovery()
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return true, fmt.Errorf("plugin rollback is unfinished; CARE could not be recovered: %w", err)
	}
	e.logln("Recovering an interrupted plugin change...")
	if err := e.recoverPlugins(state); err != nil {
		return true, fmt.Errorf("plugin rollback is unfinished; CARE could not be recovered. Start CARE to retry recovery: %w", err)
	}
	e.logln("The interrupted plugin change was rolled back.")
	return true, nil
}

func (e *Clinic) recoverPlugins(state *pluginRecovery) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	previousContext := e.ctx
	e.ctx = ctx
	defer func() { e.ctx = previousContext }()
	e.pluginBaseline = state.Containers
	var restoreErrors []error
	for _, name := range pluginSnapshotFiles {
		saved := state.Files[name]
		path := filepath.Join(e.InstallDir, name)
		var err error
		if saved.Exists {
			err = atomicfile.WritePrivate(path, saved.Data)
		} else {
			err = os.Remove(path)
			if errors.Is(err, os.ErrNotExist) {
				err = nil
			}
		}
		if err != nil {
			restoreErrors = append(restoreErrors, fmt.Errorf("restore %s: %w", name, err))
		}
	}
	if err := e.run(nil, "docker", "image", "tag", state.ImageID, state.BackendImage); err != nil {
		restoreErrors = append(restoreErrors, fmt.Errorf("restore backend image: %w", err))
	}
	if err := errors.Join(restoreErrors...); err != nil {
		return err
	}
	if err := e.stopWorkers(); err != nil {
		return err
	}
	if err := e.dc("up", "-d", "--no-build", "--pull", "never", "--wait", "--wait-timeout", "180",
		"--force-recreate", "backend"); err != nil {
		return err
	}
	// Do not run candidate migrations again or apply a queued CARE update.
	if len(state.FrontendRows) > 0 {
		if err := e.restorePluginRows(state.FrontendRows); err != nil {
			return err
		}
	}
	if err := e.dc("up", "-d", "--no-build", "--pull", "never", "--wait", "--wait-timeout", "180",
		"--force-recreate", "celery-worker", "celery-beat"); err != nil {
		return err
	}
	if err := e.dc("up", "-d", "--no-build", "--pull", "never", "--wait", "--wait-timeout", "180"); err != nil {
		return err
	}
	if err := e.waitPluginRuntime(); err != nil {
		return err
	}
	return e.finishPluginRecovery(state)
}

func (e *Clinic) finishPluginRecovery(state *pluginRecovery) error {
	if err := os.Remove(filepath.Join(e.InstallDir, pluginRecoveryFile)); err != nil {
		return err
	}
	// The transaction is committed. A leftover safety tag is harmless, unlike
	// reporting a failed rollback after removing the journal.
	if err := e.run(nil, "docker", "image", "rm", state.BackendImage+"-plugin-rollback"); err != nil {
		e.logln("warning: could not remove the plugin safety image tag: " + err.Error())
	}
	return nil
}
