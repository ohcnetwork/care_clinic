package clinic

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// stopRounds bounds how often StopStack inspects the project: every round but
	// the last stops what is still live, and the last only verifies.
	stopRounds = 4
	// stopGrace is how long Docker waits for a leftover container to exit before killing it.
	stopGrace = "30"
)

// stopSettle is the pause between rounds while Docker finishes stopping or
// removing a container.
var stopSettle = 2 * time.Second

type projectContainer struct {
	id, name, status string
}

// StopStack stops every container of the CARE compose project and verifies that
// none is left running. It keeps volumes, images and networks, succeeds when
// there is nothing to stop, and fails when a container cannot be stopped or
// Docker cannot be asked, so callers never change a stack that may still be live.
func (e *Clinic) StopStack() error {
	e.logln("Stopping CARE (data kept)...")
	if err := e.dc("stop"); err != nil {
		e.logln("docker compose could not stop everything (" + err.Error() + "); checking each CARE container directly...")
	}
	var problem error
	for round := 1; round <= stopRounds; round++ {
		containers, err := e.projectContainers()
		if err != nil {
			problem = err
		} else if live := stillLive(containers); len(live) == 0 {
			e.logln("CARE is stopped.")
			return nil
		} else {
			problem = fmt.Errorf("still active: %s", describeContainers(live))
			if round < stopRounds {
				e.stopContainers(live)
			}
		}
		if e.ctx != nil && e.ctx.Err() != nil {
			problem = errors.Join(problem, e.ctx.Err())
			break
		}
		if round < stopRounds {
			time.Sleep(stopSettle)
		}
	}
	return fmt.Errorf("CARE did not stop completely (%w). Make sure Docker is running, or stop the %q containers in Rancher Desktop, then try again",
		problem, composeProject)
}

// stillLive returns the containers that are not yet stopped. A container that
// is being removed has no process left, but it is waited for because starting
// CARE while Docker is still deleting it fails.
func stillLive(containers []projectContainer) []projectContainer {
	var live []projectContainer
	for _, c := range containers {
		switch c.status {
		case "created", "exited", "dead":
		default:
			live = append(live, c)
		}
	}
	return live
}

// stopContainers asks Docker to stop what docker compose did not. Failures are
// logged, not returned: StopStack inspects again and decides from the facts.
func (e *Clinic) stopContainers(live []projectContainer) {
	e.logln("Stopping " + describeContainers(live) + " that docker compose did not stop...")
	var stoppable, paused []projectContainer
	for _, c := range live {
		switch c.status {
		case "removing":
			continue
		case "paused":
			paused = append(paused, c)
		}
		stoppable = append(stoppable, c)
	}
	if len(paused) > 0 {
		// A paused container cannot be stopped until it is resumed.
		if _, err := e.capture("docker", append([]string{"unpause"}, containerIDs(paused)...)...); err != nil {
			e.logln("docker could not resume a paused container: " + err.Error())
		}
	}
	if len(stoppable) == 0 {
		return
	}
	if _, err := e.capture("docker", append([]string{"stop", "--time", stopGrace}, containerIDs(stoppable)...)...); err != nil {
		e.logln("docker could not stop every container: " + err.Error())
	}
}

// projectContainers lists every container labelled as part of the CARE compose
// project, including orphans and one-off runs that docker compose may not know.
func (e *Clinic) projectContainers() ([]projectContainer, error) {
	out, err := e.capture("docker", "ps", "-aq", "--no-trunc",
		"--filter", "label=com.docker.compose.project="+composeProject)
	if err != nil {
		return nil, fmt.Errorf("could not list CARE containers: %w", err)
	}
	ids := strings.Fields(out)
	if len(ids) == 0 {
		return nil, nil
	}
	out, err = e.capture("docker", append([]string{"inspect", "--format",
		"{{.Id}} {{.Name}} {{.State.Status}}"}, ids...)...)
	if err != nil {
		return nil, fmt.Errorf("could not inspect CARE containers: %w", err)
	}
	var containers []projectContainer
	for _, row := range strings.Split(out, "\n") {
		if strings.TrimSpace(row) == "" {
			continue
		}
		fields := strings.Fields(row)
		if len(fields) != 3 {
			return nil, fmt.Errorf("unexpected container state %q", row)
		}
		containers = append(containers, projectContainer{
			id: fields[0], name: strings.TrimPrefix(fields[1], "/"), status: fields[2],
		})
	}
	if len(containers) != len(ids) {
		return nil, fmt.Errorf("inspected %d of %d CARE containers", len(containers), len(ids))
	}
	return containers, nil
}

func containerIDs(cs []projectContainer) []string {
	ids := make([]string, 0, len(cs))
	for _, c := range cs {
		ids = append(ids, c.id)
	}
	return ids
}

func describeContainers(cs []projectContainer) string {
	names := make([]string, 0, len(cs))
	for _, c := range cs {
		names = append(names, c.name+" ("+c.status+")")
	}
	return strings.Join(names, ", ")
}
