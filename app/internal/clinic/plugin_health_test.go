package clinic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/health"
)

func TestPluginContainerFormatHandlesMissingDockerHealth(t *testing.T) {
	format, err := template.New("docker-inspect").Option("missingkey=error").Funcs(template.FuncMap{
		"json": func(value any) (string, error) {
			data, err := json.Marshal(value)
			return string(data), err
		},
	}).Parse(pluginContainerFormat)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"", "healthy", "starting", "unhealthy"} {
		t.Run("health="+status, func(t *testing.T) {
			state := map[string]any{"Status": "running", "StartedAt": "2026-10-03T16:14:12Z"}
			if status != "" {
				state["Health"] = map[string]any{"Status": status}
			}
			container := map[string]any{
				"Id": "container-id", "Image": "sha256:example", "RestartCount": 0,
				"Config": map[string]any{"Labels": map[string]string{"com.docker.compose.service": "backup"}},
				"State":  state,
			}
			var out bytes.Buffer
			if err := format.Execute(&out, container); err != nil {
				t.Fatalf("container metadata with health %q cannot be inspected: %v", status, err)
			}
			var got pluginContainer
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Health != status || got.Service != "backup" || got.Status != "running" {
				t.Fatalf("unexpected container metadata: %+v", got)
			}
		})
	}
}

func TestPluginReadinessRejectsFailedContainersAndUnreachableClinic(t *testing.T) {
	for _, failure := range []string{"restarting", "exited", "dead", "unhealthy", "missing", "ping", "inspection", "restart-count", "replacement", "slow-health"} {
		t.Run(failure, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()
			calls := 0
			inspect := func() ([]pluginContainer, error) {
				calls++
				c := pluginContainer{ID: "backend", Service: "backend", Status: "running", Health: "healthy", StartedAt: "initial"}
				switch failure {
				case "restarting", "exited", "dead":
					c.Status = failure
				case "unhealthy":
					c.Health = failure
				case "slow-health":
					c.Health = "starting"
				case "missing":
					return nil, nil
				case "inspection":
					return nil, errors.New("Docker unavailable")
				case "restart-count":
					if calls > 1 {
						c.RestartCount = 1
					}
				case "replacement":
					if calls > 1 {
						c.ID = "replacement"
					}
				}
				return []pluginContainer{c}, nil
			}
			ping := func() health.Health { return health.Health{Active: failure != "ping", Detail: "HTTP 502"} }
			err := waitPluginHealth(ctx, []string{"backend"}, nil, 15*time.Millisecond, time.Millisecond, inspect, ping)
			if err == nil {
				t.Fatal("failed clinic was marked healthy")
			}
			if calls > 1 && failure == "inspection" {
				t.Fatal("ignored Docker inspection failure")
			}
		})
	}
}

func TestPluginReadinessRequiresContinuousStability(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	started := time.Now()
	calls := 0
	baseline := []pluginContainer{{ID: "backend", Service: "backend", Status: "running", RestartCount: 3}}
	err := waitPluginHealth(ctx, []string{"backend"}, baseline, 20*time.Millisecond, time.Millisecond,
		func() ([]pluginContainer, error) { return baseline, nil },
		func() health.Health {
			calls++
			return health.Health{Active: calls != 10}
		})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(started) < 29*time.Millisecond {
		t.Fatal("readiness did not reset the stability window after ping failed")
	}
}

func TestPluginReadinessDetectsCrashBeforeFirstSample(t *testing.T) {
	c := pluginContainer{ID: "new", Service: "worker", Status: "running", RestartCount: 1}
	err := waitPluginHealth(context.Background(), []string{"worker"}, nil, 0, 0,
		func() ([]pluginContainer, error) { return []pluginContainer{c}, nil },
		func() health.Health { return health.Health{Active: true} })
	if err == nil || !strings.Contains(err.Error(), "restarted") {
		t.Fatalf("a crash before the first sample was missed: %v", err)
	}
}
