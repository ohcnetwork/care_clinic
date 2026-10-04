package clinic

import (
	"encoding/json"
	"fmt"

	"github.com/ohcnetwork/care_desktop/app/internal/compose"
	"github.com/ohcnetwork/care_desktop/app/internal/plugins"
)

const frontendPluginsEnv = "CARE_DESKTOP_FRONTEND_PLUGINS"

const frontendPluginsScript = `import json, os
from django.db import transaction
from django.core.cache import cache
from care.users.models import PlugConfig
wanted = json.loads(os.environ["` + frontendPluginsEnv + `"])
with transaction.atomic():
    for slug, meta in wanted.items():
        PlugConfig.objects.update_or_create(slug=slug, defaults={"meta": meta})
    stale = PlugConfig.objects.filter(meta__` + plugins.ManagedKey + `="` + plugins.ManagedValue + `").exclude(slug__in=list(wanted))
    removed = [row.slug for row in stale]
    stale.delete()
cache.delete("care_plug_viewset_list")
print("Frontend plugins: %d registered, %d removed" % (len(wanted), len(removed)))
`

func (e *Clinic) snapshotPluginRows(list []plugins.Plugin) (json.RawMessage, error) {
	slugs := []string{}
	for _, p := range list {
		if p.Frontend != nil {
			slugs = append(slugs, p.Frontend.Slug)
		}
	}
	payload, err := json.Marshal(slugs)
	if err != nil {
		return nil, err
	}
	run := e.Runner()
	run.Env = append(run.Env, frontendPluginsEnv+"="+string(payload))
	out, err := run.Capture("docker", "compose", "exec", "-T", "-e", frontendPluginsEnv,
		"backend", "python", "manage.py", "shell", "--verbosity", "0", "-c", `import json, os
from django.db.models import Q
from care.users.models import PlugConfig
slugs = json.loads(os.environ["`+frontendPluginsEnv+`"])
rows = list(PlugConfig.objects.filter(Q(meta__`+plugins.ManagedKey+`="`+plugins.ManagedValue+`") | Q(slug__in=slugs)).values("slug", "meta"))
print(json.dumps({"slugs": slugs, "rows": rows}))
`)
	if err != nil {
		return nil, fmt.Errorf("could not preserve frontend plugin registrations: %w", err)
	}
	if !json.Valid([]byte(out)) {
		return nil, fmt.Errorf("could not read frontend plugin registrations")
	}
	return json.RawMessage(out), nil
}

func (e *Clinic) restorePluginRows(rows json.RawMessage) error {
	return e.run([]string{frontendPluginsEnv + "=" + string(rows)},
		"docker", "compose", "exec", "-T", "-e", frontendPluginsEnv,
		"backend", "python", "manage.py", "shell", "-c", `import json, os
from django.db import transaction
from django.db.models import Q
from django.core.cache import cache
from care.users.models import PlugConfig
saved = json.loads(os.environ["`+frontendPluginsEnv+`"])
with transaction.atomic():
    slugs = [row["slug"] for row in saved["rows"]]
    PlugConfig.objects.filter(Q(meta__`+plugins.ManagedKey+`="`+plugins.ManagedValue+`") | Q(slug__in=saved["slugs"])).exclude(slug__in=slugs).delete()
    for row in saved["rows"]:
        PlugConfig.objects.update_or_create(slug=row["slug"], defaults={"meta": row["meta"]})
cache.delete("care_plug_viewset_list")
`)
}

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

func (e *Clinic) applyPluginCandidate() error {
	current, err := e.Builder().BackendImageCurrent()
	if err != nil {
		return err
	}
	if current {
		if err := e.SyncFrontendPlugins(); err != nil {
			return err
		}
		return e.waitPluginRuntime()
	}
	if err := e.Builder().BuildBackend(); err != nil {
		return fmt.Errorf("plugin image build failed: %w", err)
	}
	// A queued backend update was built with the previous plugin inputs.
	if err := compose.ModifyLock(e.InstallDir, func(lock *compose.Lock) error {
		lock.Backend.Next, lock.Backend.Declined = "", ""
		return nil
	}); err != nil {
		return err
	}
	if err := e.stopWorkers(); err != nil {
		return err
	}
	if err := e.dc("up", "-d", "--no-build", "--pull", "never", "--wait", "--wait-timeout", "180", "db", "redis", "backend"); err != nil {
		return fmt.Errorf("plugin backend startup failed: %w", err)
	}
	if err := e.migrate(); err != nil {
		return err
	}
	if err := e.SyncFrontendPlugins(); err != nil {
		return err
	}
	if err := e.dc("up", "-d", "--no-build", "--pull", "never", "--wait", "--wait-timeout", "180"); err != nil {
		return fmt.Errorf("plugin services failed to start: %w", err)
	}
	return e.waitPluginRuntime()
}
