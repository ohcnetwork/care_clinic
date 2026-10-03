package clinic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/health"
)

type pluginContainer struct {
	ID           string
	Service      string
	Image        string
	Status       string
	Health       string
	StartedAt    string
	RestartCount int
}

// Docker uses missingkey=error; services without a health check omit Health.
const pluginContainerFormat = `{"ID":{{json .Id}},"Service":{{json (index .Config.Labels "com.docker.compose.service")}},"Image":{{json .Image}},"Status":{{json .State.Status}},"Health":{{with index .State "Health"}}{{json .Status}}{{else}}""{{end}},"StartedAt":{{json .State.StartedAt}},"RestartCount":{{.RestartCount}}}`

func (e *Clinic) pluginContainers() ([]pluginContainer, error) {
	ids, err := e.captureLines("docker", "compose", "ps", "--all", "--quiet")
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	// Inspect only operational metadata; container environment contains secrets.
	lines, err := e.captureLines("docker", append([]string{"inspect", "--format", pluginContainerFormat}, ids...)...)
	if err != nil {
		return nil, err
	}
	if len(lines) != len(ids) {
		return nil, errors.New("could not inspect every clinic container")
	}
	containers := make([]pluginContainer, len(lines))
	for i, line := range lines {
		if err := json.Unmarshal([]byte(line), &containers[i]); err != nil {
			return nil, fmt.Errorf("invalid clinic container status: %w", err)
		}
	}
	return containers, nil
}

func (e *Clinic) waitPluginRuntime() error {
	if e.pluginReady != nil {
		return e.pluginReady()
	}
	out, err := e.capture("docker", "compose", "config", "--services")
	if err != nil {
		return err
	}
	services := strings.Fields(out)
	if len(services) == 0 {
		return errors.New("the clinic has no configured services")
	}
	parent := e.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 3*time.Minute)
	defer cancel()
	previousContext := e.ctx
	e.ctx = ctx
	defer func() { e.ctx = previousContext }()
	e.logln("Checking every clinic container and /ping/ for 30 seconds of stable health...")
	return waitPluginHealth(ctx, services, e.pluginBaseline, 30*time.Second, 2*time.Second, e.pluginContainers, health.Ping)
}

func waitPluginHealth(ctx context.Context, services []string, baseline []pluginContainer, stableFor, interval time.Duration,
	inspect func() ([]pluginContainer, error), ping func() health.Health) error {
	var stableSince time.Time
	previous := map[string]pluginContainer{}
	original := map[string]pluginContainer{}
	for _, c := range baseline {
		original[c.ID] = c
	}
	last := "waiting for clinic services"
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("clinic readiness failed (%s): %w", last, err)
		}
		containers, err := inspect()
		if err != nil {
			return fmt.Errorf("could not verify clinic containers: %w", err)
		}
		ready := true
		found := map[string]bool{}
		for _, c := range containers {
			found[c.Service] = true
			switch c.Status {
			case "restarting", "exited", "dead", "removing":
				return fmt.Errorf("clinic service %s is %s", c.Service, c.Status)
			}
			if c.Health == "unhealthy" {
				return fmt.Errorf("clinic service %s is unhealthy", c.Service)
			}
			old, seen := previous[c.Service]
			if seen && (old.ID != c.ID || old.StartedAt != c.StartedAt || old.RestartCount != c.RestartCount) {
				return fmt.Errorf("clinic service %s restarted during readiness checks", c.Service)
			}
			if !seen && c.RestartCount > 0 && original[c.ID].RestartCount != c.RestartCount {
				return fmt.Errorf("clinic service %s restarted before readiness checks", c.Service)
			}
			previous[c.Service] = c
			if c.Status != "running" || (c.Health != "" && c.Health != "healthy") {
				ready = false
				last = "waiting for service " + c.Service
			}
		}
		for _, service := range services {
			if !found[service] {
				ready = false
				last = "missing service " + service
			}
		}
		if h := ping(); !h.Active {
			ready = false
			last = h.Detail
		}
		if ready {
			if stableSince.IsZero() {
				stableSince = time.Now()
			}
			if time.Since(stableSince) >= stableFor {
				return nil
			}
			last = "observing stable clinic health"
		} else {
			stableSince = time.Time{}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("clinic readiness failed (%s): %w", last, ctx.Err())
		case <-time.After(interval):
		}
	}
}
