package clinic

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func (f *lifecycle) env(name string) string {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.e.InstallDir, name+".env"))
	f.must(err)
	return string(data)
}

// envWrite notes what was true when a settings file was written.
type envWrite struct {
	file     string
	running  []string
	commands int
}

// recordEnvWrites notes, at every settings write, which containers were running
// and how many docker commands had run, then writes the file as usual.
func (f *lifecycle) recordEnvWrites() *[]envWrite {
	f.t.Helper()
	real := writeEnvFile
	var writes []envWrite
	writeEnvFile = func(path string, data []byte) error {
		writes = append(writes, envWrite{filepath.Base(path), f.running(), len(f.calls())})
		return real(path, data)
	}
	f.t.Cleanup(func() { writeEnvFile = real })
	return &writes
}

// failEnvWrites makes the writes for which fails(path, data) is true fail.
func (f *lifecycle) failEnvWrites(fails func(path string, data []byte) bool) {
	f.t.Helper()
	real := writeEnvFile
	writeEnvFile = func(path string, data []byte) error {
		if fails(path, data) {
			return errors.New("disk full")
		}
		return real(path, data)
	}
	f.t.Cleanup(func() { writeEnvFile = real })
}

func frontendKeyFor(commit, env string) string {
	return backendKey(commit) + "+env@" + fixtureHash(env)
}

func TestApplyEnvFilesChangesFilesOnlyAfterCareIsStopped(t *testing.T) {
	f := newLifecycle(t)
	oldBackend := f.env("backend")
	newBackend := oldBackend + "RETENTION_DAYS=30\n"
	writes := f.recordEnvWrites()
	var startedOn []string
	f.e.startReady = func() error {
		startedOn = append(startedOn, f.env("backend"))
		return nil
	}

	if err := f.e.ApplyEnvFiles([]EnvChange{{Name: "backend", Content: newBackend}}); err != nil {
		t.Fatal(err)
	}

	if len(*writes) != 1 {
		t.Fatalf("expected one write, got %+v", *writes)
	}
	write := (*writes)[0]
	if len(write.running) != 0 {
		t.Fatalf("the settings were written while CARE was running: %v", write.running)
	}
	calls := f.calls()
	if stop := calls.exact(stopStackCall); stop < 0 || stop >= write.commands {
		t.Fatalf("CARE was not stopped before the write (stop at %d, write after %d commands):\n%s",
			stop, write.commands, strings.Join(calls, "\n"))
	}
	if verify := calls.first("ps -aq"); verify < 0 || verify >= write.commands {
		t.Fatalf("the stop was not verified before the write:\n%s", strings.Join(calls, "\n"))
	}
	if up := calls.first("compose up"); up < write.commands {
		t.Fatalf("CARE was started before the new settings were saved:\n%s", strings.Join(calls, "\n"))
	}
	if len(startedOn) != 1 || startedOn[0] != newBackend {
		t.Fatalf("CARE did not start on the new settings: %q", startedOn)
	}
	if got := f.env("backend"); got != newBackend {
		t.Fatalf("backend.env = %q", got)
	}
	if info, err := os.Stat(filepath.Join(f.e.InstallDir, "backend.env")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("backend.env is not private: %v, %v", info, err)
	}
	if n := calls.count("build "); n != 0 {
		t.Fatalf("a backend-only change built %d image(s)", n)
	}
	if running := f.running(); len(running) != 4 {
		t.Fatalf("CARE is not running: %v", running)
	}
	assertDataKept(t, calls, false)
}

func TestApplyEnvFilesRebuildsTheFrontendForFrontendSettings(t *testing.T) {
	f := newLifecycle(t)
	const newFrontend = "REACT_TITLE=North clinic\n"

	if err := f.e.ApplyEnvFiles([]EnvChange{{Name: "frontend", Content: newFrontend}}); err != nil {
		t.Fatal(err)
	}

	label, ok := f.imageLabel("fixture-frontend")
	if !ok || label != frontendKeyFor(oldCommit, newFrontend) {
		t.Fatalf("the frontend image was not rebuilt with the new settings: %q", label)
	}
	if n := f.calls().count("build -t fixture-backend"); n != 0 {
		t.Fatalf("the backend was rebuilt for a frontend setting (%d)", n)
	}
	if got := f.env("frontend"); got != newFrontend {
		t.Fatalf("frontend.env = %q", got)
	}
	if running := f.running(); len(running) != 4 {
		t.Fatalf("CARE is not running: %v", running)
	}
}

func TestApplyEnvFilesAppliesSettingsThatAreAlreadySaved(t *testing.T) {
	f := newLifecycle(t)

	if err := f.e.ApplyEnvFiles([]EnvChange{{Name: "backend", Content: f.env("backend")}}); err != nil {
		t.Fatal(err)
	}

	calls := f.calls()
	if calls.exact(stopStackCall) < 0 || calls.first("compose up") < 0 {
		t.Fatalf("saved settings were not applied by a stop and a start:\n%s", strings.Join(calls, "\n"))
	}
}

func TestApplyEnvFilesRejectsABadRequestBeforeStoppingCare(t *testing.T) {
	backend := func(f *lifecycle) string { return f.env("backend") + "A=1\n" }
	cases := []struct {
		name    string
		prepare func(f *lifecycle)
		changes func(f *lifecycle) []EnvChange
		want    string
	}{
		{"nothing to apply", nil, func(*lifecycle) []EnvChange { return nil }, "no settings to apply"},
		{"unknown file", nil, func(*lifecycle) []EnvChange { return []EnvChange{{Name: "caddy", Content: "A=1\n"}} }, "unknown env file: caddy"},
		{"a file given twice", nil, func(f *lifecycle) []EnvChange {
			return []EnvChange{{Name: "backend", Content: backend(f)}, {Name: "backend", Content: backend(f)}}
		}, "backend settings were given twice"},
		{"invalid syntax", nil, func(*lifecycle) []EnvChange {
			return []EnvChange{{Name: "frontend", Content: "REACT_TITLE=\"unbalanced\n"}}
		}, "frontend settings have invalid syntax"},
		{"invalid second file", nil, func(f *lifecycle) []EnvChange {
			return []EnvChange{{Name: "backend", Content: backend(f)}, {Name: "frontend", Content: "A=\"unbalanced\n"}}
		}, "frontend settings have invalid syntax"},
		{"missing current file", func(f *lifecycle) {
			f.must(os.Remove(filepath.Join(f.e.InstallDir, "frontend.env")))
		}, func(*lifecycle) []EnvChange { return []EnvChange{{Name: "frontend", Content: "A=1\n"}} }, "couldn't read the current frontend settings"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newLifecycle(t)
			if tc.prepare != nil {
				tc.prepare(f)
			}
			before := f.env("backend")
			writes := f.recordEnvWrites()

			checked := f.e.CheckEnvFiles(tc.changes(f))
			err := f.e.ApplyEnvFiles(tc.changes(f))

			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want one containing %q", err, tc.want)
			}
			if checked == nil || checked.Error() != err.Error() {
				t.Fatalf("the check said %v, applying said %v", checked, err)
			}
			if calls := f.calls(); len(calls) != 0 {
				t.Fatalf("docker was used for a request that cannot work:\n%s", strings.Join(calls, "\n"))
			}
			if len(*writes) != 0 || f.env("backend") != before {
				t.Fatalf("a file changed: %+v", *writes)
			}
			if running := f.running(); len(running) != 4 {
				t.Fatalf("CARE was stopped: %v", running)
			}
		})
	}
}

func TestCheckEnvFilesAcceptsAGoodRequestWithoutTouchingAnything(t *testing.T) {
	f := newLifecycle(t)
	before := f.env("backend")
	writes := f.recordEnvWrites()

	err := f.e.CheckEnvFiles([]EnvChange{{Name: "backend", Content: before + "A=1\n"}, {Name: "frontend", Content: "B=2\n"}})

	if err != nil {
		t.Fatalf("a good request was refused: %v", err)
	}
	if calls := f.calls(); len(calls) != 0 {
		t.Fatalf("checking used docker:\n%s", strings.Join(calls, "\n"))
	}
	if len(*writes) != 0 || f.env("backend") != before {
		t.Fatalf("checking changed a file: %+v", *writes)
	}
	if running := f.running(); len(running) != 4 {
		t.Fatalf("checking stopped CARE: %v", running)
	}
}

func TestApplyEnvFilesChangesNothingWhenCareCannotBeStopped(t *testing.T) {
	f := newLifecycle(t)
	f.flag("stubborn-c-backend")
	f.fail("compose-stop")
	before := f.env("backend")
	writes := f.recordEnvWrites()

	err := f.e.ApplyEnvFiles([]EnvChange{{Name: "backend", Content: before + "A=1\n"}})

	if err == nil || !strings.Contains(err.Error(), "nothing was changed") || !strings.Contains(err.Error(), "did not stop completely") {
		t.Fatalf("a failed stop was not reported as such: %v", err)
	}
	if len(*writes) != 0 || f.env("backend") != before {
		t.Fatalf("settings changed under a running CARE: %+v", *writes)
	}
	calls := f.calls()
	for _, prefix := range []string{"compose up", "build ", "compose down"} {
		if n := calls.count(prefix); n != 0 {
			t.Fatalf("%d %q command(s) ran against a stack that was still running", n, prefix)
		}
	}
}

func TestApplyEnvFilesPutsTheOldSettingsBackWhenCareDoesNotStartOnTheNewOnes(t *testing.T) {
	f := newLifecycle(t)
	oldBackend, oldFrontend := f.env("backend"), f.env("frontend")
	newBackend, newFrontend := oldBackend+"BROKEN=1\n", "REACT_TITLE=North clinic\n"
	writes := f.recordEnvWrites()
	starts := 0
	f.e.startReady = func() error {
		starts++
		if strings.Contains(f.env("backend"), "BROKEN=1") {
			return errors.New("CARE did not become healthy")
		}
		return nil
	}

	err := f.e.ApplyEnvFiles([]EnvChange{{Name: "backend", Content: newBackend}, {Name: "frontend", Content: newFrontend}})

	if err == nil {
		t.Fatal("applying settings CARE cannot start on reported success")
	}
	for _, want := range []string{"were not applied", "running on the previous ones", "did not become healthy"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error does not say %q: %v", want, err)
		}
	}
	if f.env("backend") != oldBackend || f.env("frontend") != oldFrontend {
		t.Fatalf("the previous settings were not put back: %q / %q", f.env("backend"), f.env("frontend"))
	}
	if starts != 2 {
		t.Fatalf("CARE was started %d times, want the failed start and one on the previous settings", starts)
	}
	if running := f.running(); len(running) != 4 {
		t.Fatalf("CARE is not running on the previous settings: %v", running)
	}
	if label, _ := f.imageLabel("fixture-frontend"); label != frontendKeyFor(oldCommit, oldFrontend) {
		t.Fatalf("the frontend image was not rebuilt from the previous settings: %q", label)
	}
	if len(*writes) != 4 {
		t.Fatalf("expected the two new files and the two old ones to be written: %+v", *writes)
	}
	for _, restore := range (*writes)[2:] {
		if len(restore.running) != 0 {
			t.Fatalf("the previous %s was put back while CARE was running: %v", restore.file, restore.running)
		}
	}
	assertDataKept(t, f.calls(), false)
}

func TestApplyEnvFilesPutsBackFilesAlreadyWrittenWhenALaterOneCannotBeSaved(t *testing.T) {
	f := newLifecycle(t)
	oldBackend := f.env("backend")
	f.failEnvWrites(func(path string, _ []byte) bool { return filepath.Base(path) == "frontend.env" })

	err := f.e.ApplyEnvFiles([]EnvChange{
		{Name: "backend", Content: oldBackend + "A=1\n"},
		{Name: "frontend", Content: "REACT_TITLE=North clinic\n"},
	})

	if err == nil || !strings.Contains(err.Error(), "couldn't save the frontend settings") {
		t.Fatalf("a failed write was not reported: %v", err)
	}
	if got := f.env("backend"); got != oldBackend {
		t.Fatalf("the backend settings that were already written were kept: %q", got)
	}
}

func TestApplyEnvFilesStartsCareOnThePreviousSettingsWhenASaveFails(t *testing.T) {
	f := newLifecycle(t)
	oldBackend := f.env("backend")
	f.failEnvWrites(func(path string, _ []byte) bool { return filepath.Base(path) == "frontend.env" })

	err := f.e.ApplyEnvFiles([]EnvChange{
		{Name: "backend", Content: oldBackend + "A=1\n"},
		{Name: "frontend", Content: "REACT_TITLE=North clinic\n"},
	})

	if err == nil || !strings.Contains(err.Error(), "running on the previous ones") {
		t.Fatalf("CARE was not started again on the previous settings: %v", err)
	}
	if running := f.running(); len(running) != 4 {
		t.Fatalf("CARE is not running: %v", running)
	}
}

func TestApplyEnvFilesReportsAStartThatFailsOnThePreviousSettingsToo(t *testing.T) {
	f := newLifecycle(t)
	oldBackend := f.env("backend")
	f.e.startReady = func() error { return errors.New("CARE did not become healthy") }

	err := f.e.ApplyEnvFiles([]EnvChange{{Name: "backend", Content: oldBackend + "A=1\n"}})

	if err == nil || !strings.Contains(err.Error(), "could not start on them") {
		t.Fatalf("the failed second start was not reported: %v", err)
	}
	if got := f.env("backend"); got != oldBackend {
		t.Fatalf("the previous settings were not put back: %q", got)
	}
}

func TestApplyEnvFilesDoesNotStartAgainWhenThePreviousSettingsCannotBePutBack(t *testing.T) {
	f := newLifecycle(t)
	oldBackend := f.env("backend")
	starts := 0
	f.e.startReady = func() error {
		starts++
		return errors.New("CARE did not become healthy")
	}
	f.failEnvWrites(func(_ string, data []byte) bool { return string(data) == oldBackend })

	err := f.e.ApplyEnvFiles([]EnvChange{{Name: "backend", Content: oldBackend + "A=1\n"}})

	if err == nil || !strings.Contains(err.Error(), "couldn't put back the backend settings") ||
		!strings.Contains(err.Error(), "did not become healthy") {
		t.Fatalf("both failures should be reported: %v", err)
	}
	if starts != 1 {
		t.Fatalf("CARE was started on settings that could not be restored (%d starts)", starts)
	}
}

func TestApplyEnvFilesStillPutsTheOldSettingsBackWhenCareCannotBeStoppedToRevert(t *testing.T) {
	f := newLifecycle(t)
	oldBackend := f.env("backend")
	starts := 0
	f.e.startReady = func() error {
		starts++
		f.flag("orphan-c-backend")
		f.flag("stubborn-c-backend")
		return errors.New("CARE did not become healthy")
	}

	err := f.e.ApplyEnvFiles([]EnvChange{{Name: "backend", Content: oldBackend + "A=1\n"}})

	if err == nil || !strings.Contains(err.Error(), "could not be stopped to start it on them") {
		t.Fatalf("the failed stop was not reported: %v", err)
	}
	if got := f.env("backend"); got != oldBackend {
		t.Fatalf("the previous settings were not put back: %q", got)
	}
	if starts != 1 {
		t.Fatalf("CARE was started while a container could not be stopped (%d starts)", starts)
	}
}
