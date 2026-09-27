package clinic

import (
	"encoding/json"
	"fmt"

	"github.com/ohcnetwork/care_desktop/app/internal/plugins"
)

const frontendPluginsEnv = "CARE_DESKTOP_FRONTEND_PLUGINS"

const frontendPluginsScript = `import json, os
from django.core.cache import cache
from care.users.models import PlugConfig
wanted = json.loads(os.environ["` + frontendPluginsEnv + `"])
for slug, meta in wanted.items():
    PlugConfig.objects.update_or_create(slug=slug, defaults={"meta": meta})
stale = PlugConfig.objects.filter(meta__` + plugins.ManagedKey + `="` + plugins.ManagedValue + `").exclude(slug__in=list(wanted))
removed = [row.slug for row in stale]
stale.delete()
cache.delete("care_plug_viewset_list")
print("Frontend plugins: %d registered, %d removed" % (len(wanted), len(removed)))
`

func (e *Clinic) SyncFrontendPlugins() error {
	rows, err := plugins.New(e.InstallDir).FrontendRows()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	if err := e.run([]string{frontendPluginsEnv + "=" + string(payload)},
		"docker", "compose", "exec", "-T", "-e", frontendPluginsEnv,
		"backend", "python", "manage.py", "shell", "-c", frontendPluginsScript); err != nil {
		return fmt.Errorf("frontend plugins could not be registered with CARE: %w", err)
	}
	return nil
}

func (e *Clinic) syncFrontendPluginsOrWarn() {
	if err := e.SyncFrontendPlugins(); err != nil {
		e.logln("warning: " + err.Error())
	}
}

func (e *Clinic) ApplyPlugins() error {
	if err := e.Backups().RecoverRestore(); err != nil {
		return err
	}
	current, err := e.Builder().BackendImageCurrent()
	if err != nil {
		return err
	}
	if !current {
		return e.RebuildBackend()
	}
	running, err := e.backendRunning()
	if err != nil {
		return err
	}
	if !running {
		e.logln("CARE is not running; the plugins will be set up the next time it starts.")
		return nil
	}
	if err := e.SyncFrontendPlugins(); err != nil {
		return err
	}
	e.logln("Plugins applied.")
	return nil
}

func (e *Clinic) backendRunning() (bool, error) {
	ids, err := e.captureLines("docker", "compose", "ps", "--status", "running", "--quiet", "backend")
	if err != nil {
		return false, err
	}
	return len(ids) > 0, nil
}
