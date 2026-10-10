package clinic

func (e *Clinic) Stop() error { e.logln("Stopping CARE (data kept)..."); return e.dc("stop") }

func (e *Clinic) Restart() error {
	e.logln("Restarting CARE...")
	if err := e.Stop(); err != nil {
		return err
	}
	return e.Start()
}

// RemoveContainers removes the clinic's containers but keeps its volumes, so
// the next Start recreates them with the engine's current settings.
func (e *Clinic) RemoveContainers() error {
	e.logln("Removing CARE's containers (data kept)...")
	return e.dc("down")
}
