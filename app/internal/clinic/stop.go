package clinic

func (e *Clinic) Stop() error { return e.StopStack() }

func (e *Clinic) Restart() error {
	e.logln("Restarting CARE...")
	return e.Start()
}
