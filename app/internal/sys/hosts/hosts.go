package hosts

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/elevate"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

const marker = "# care-clinic"

func path() string {
	if runtime.GOOS != "windows" {
		return "/etc/hosts"
	}
	win := os.Getenv("WINDIR")
	if win == "" {
		win = `C:\Windows`
	}
	return filepath.Join(win, "System32", "drivers", "etc", "hosts")
}

func hasEntry(data, host string) bool {
	found := false
	for _, ln := range strings.Split(data, "\n") {
		line := strings.TrimSpace(ln)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i] // drop any trailing comment
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		for _, field := range fields[1:] {
			if strings.EqualFold(field, host) {
				if !net.ParseIP(fields[0]).IsLoopback() {
					return false
				}
				found = true
			}
		}
	}
	return found
}

func line(host string) string { return "127.0.0.1 " + host + " " + marker }

func addSh(host string) string {
	p := elevate.ShQuote(path())
	return "printf '\\n%s\\n' " + elevate.ShQuote(line(host)) + " >> " + p
}

func addPS(host string) string {
	return `Add-Content -LiteralPath "$env:WINDIR\System32\drivers\etc\hosts" -Value ` +
		elevate.PSQuote(line(host))
}

func Step(log func(string), host string) (elevate.Step, bool) {
	if HasEntry(host) {
		return elevate.Step{}, false
	}
	_ = addUnprivileged(host)
	if HasEntry(host) {
		logln(log, "Added a hosts entry so https://"+host+"/ opens on this computer.")
		return elevate.Step{}, false
	}
	return elevate.Step{
		What: "add " + host + " to this computer's hosts file, so the name works here",
		Sh:   addSh(host),
		PS:   addPS(host),
	}, true
}

func HasEntry(host string) bool {
	data, err := os.ReadFile(path())
	return err == nil && hasEntry(string(data), host)
}

func addUnprivileged(host string) error {
	if runtime.GOOS == "windows" {
		return proc.Command("powershell", "-NoProfile", "-Command", addPS(host)).Run()
	}
	return proc.Command("sh", "-c", addSh(host)).Run()
}

func RemoveStepWindows(host string) (step elevate.Step, cleanup func(), need bool) {
	step, cleanup, need, err := replaceStep(path(), withoutMarker, "remove "+host+" from this computer's hosts file")
	if err != nil {
		return elevate.Step{}, cleanup, false
	}
	return step, cleanup, need
}

func Remove(log func(string), confirm func(string, string) bool, host string) string {
	if runtime.GOOS == "windows" {
		step, cleanup, need := RemoveStepWindows(host)
		defer cleanup()
		if !need {
			return ""
		}
		logln(log, "Removing the "+host+" hosts entry...")
		_ = elevate.Steps([]elevate.Step{step})
		return Leftover(host)
	}
	data, err := os.ReadFile(path())
	if os.IsNotExist(err) || err == nil && !strings.Contains(string(data), marker) {
		return ""
	}
	logln(log, "Removing the "+host+" hosts entry...")
	return removeUnix(confirm, host, path(), elevate.Run)
}

func removeUnix(confirm func(string, string) bool, host, p string, run func(string, bool) error) string {
	step, cleanup, need, err := replaceStep(p, withoutMarker, "remove "+host+" from this computer's hosts file")
	defer cleanup()
	if err != nil {
		return leftover(host, p)
	}
	if !need {
		return ""
	}
	_ = run(step.Sh, false)
	if leftover(host, p) == "" {
		return ""
	}
	if confirm == nil || confirm("Remove CARE's saved address?",
		"Remove "+host+" from this computer. Approve the system permission prompt to continue.") {
		_ = run(step.Sh, true)
	}
	return leftover(host, p)
}

func Leftover(host string) string {
	return leftover(host, path())
}

func leftover(host, p string) string {
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		return "Could not check CARE's hosts entry in " + p + ": " + err.Error()
	}
	if !strings.Contains(string(data), marker) {
		return ""
	}
	return "The line \"" + line(host) + "\" is still in " + p +
		". Until it is removed this computer resolves " + host + " to itself."
}

func logln(log func(string), s string) {
	if log != nil {
		log(s)
	}
}

func Present() bool {
	present, err := Inspect()
	return present || err != nil
}

func Inspect() (bool, error) {
	return inspect(path())
}

func inspect(p string) (bool, error) {
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("could not inspect %s: %w", p, err)
	}
	return strings.Contains(string(data), marker), nil
}

func RemoveHost(log func(string), host string) error {
	step, cleanup, need, err := RemoveHostStep(host)
	defer cleanup()
	if err != nil || !need {
		return err
	}
	logln(log, "Removing "+host+" and any other CARE entries from this computer's hosts file "+
		"so the clinic is found on the network...")
	if err := elevate.Steps([]elevate.Step{step}); err != nil {
		return fmt.Errorf("could not remove %s from this computer's hosts file: %w", host, err)
	}
	return VerifyHostRemoved(log, host)
}

// RemoveHostStep stages the clinic-connect hosts cleanup without running it, so
// the caller can put it in the same elevated batch as the certificate work and
// ask for administrator approval once.
func RemoveHostStep(host string) (step elevate.Step, cleanup func(), need bool, err error) {
	p := path()
	step, cleanup, need, err = replaceStep(p, keepForClient(host), "remove old CARE entries from this computer's hosts file")
	if err != nil {
		return elevate.Step{}, cleanup, false, fmt.Errorf("could not check this computer's hosts file for %s: %w", host, err)
	}
	return step, cleanup, need, nil
}

// VerifyHostRemoved re-reads the file, because an approved prompt is not proof
// that the write happened.
func VerifyHostRemoved(log func(string), host string) error {
	p := path()
	after, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("could not check this computer's hosts file for %s: %w", host, err)
	}
	if _, still := keepForClient(host)(string(after)); still {
		return fmt.Errorf("could not remove %s from this computer's hosts file: it is still listed in %s", host, p)
	}
	logln(log, "Removed "+host+" and CARE's own entries from the hosts file. "+
		"A copy of the old file was saved as "+p+".care-backup.")
	return nil
}

// A client connecting to a clinic wants both halves gone in one approval: the
// name it is about to open, whoever added it, and every line CARE added for any
// name, which an earlier server setup on this computer leaves behind.
func keepForClient(host string) func(string) (string, bool) {
	return func(data string) (string, bool) { return withoutHostOrMarker(data, host) }
}

func withoutHostOrMarker(data, host string) (string, bool) {
	cleaned, removedHost := withoutHost(data, host)
	cleaned, removedMarked := withoutMarker(cleaned)
	return cleaned, removedHost || removedMarked
}

func replaceStep(p string, keep func(string) (string, bool), what string) (step elevate.Step, cleanup func(), need bool, err error) {
	cleanup = func() {}
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return elevate.Step{}, cleanup, false, nil
	}
	if err != nil {
		return elevate.Step{}, cleanup, false, err
	}
	cleaned, changed := keep(string(data))
	if !changed {
		return elevate.Step{}, cleanup, false, nil
	}
	f, err := os.CreateTemp("", "care-hosts-*")
	if err != nil {
		return elevate.Step{}, cleanup, false, err
	}
	cleanup = func() { _ = os.Remove(f.Name()) }
	if _, err := f.WriteString(cleaned); err != nil {
		_ = f.Close()
		return elevate.Step{}, cleanup, false, err
	}
	if err := f.Close(); err != nil {
		return elevate.Step{}, cleanup, false, err
	}
	if err := os.Chmod(f.Name(), 0o644); err != nil {
		return elevate.Step{}, cleanup, false, err
	}
	return elevate.Step{What: what, Sh: replaceSh(f.Name(), p), PS: replacePS(f.Name(), p)}, cleanup, true, nil
}

func withoutMarker(data string) (string, bool) {
	lines := strings.SplitAfter(data, "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		if !strings.Contains(ln, marker) {
			out = append(out, ln)
		}
	}
	return strings.Join(out, ""), len(out) != len(lines)
}

func replaceSh(src, dst string) string {
	d := elevate.ShQuote(dst)
	return "cp " + d + " " + elevate.ShQuote(dst+".care-backup") + " && cat " + elevate.ShQuote(src) + " > " + d +
		" && { dscacheutil -flushcache 2>/dev/null; killall -HUP mDNSResponder 2>/dev/null; resolvectl flush-caches 2>/dev/null; true; }"
}

func replacePS(src, dst string) string {
	return "Copy-Item -LiteralPath " + elevate.PSQuote(dst) + " -Destination " + elevate.PSQuote(dst+".care-backup") + " -Force; " +
		"Copy-Item -LiteralPath " + elevate.PSQuote(src) + " -Destination " + elevate.PSQuote(dst) + " -Force; " +
		"ipconfig /flushdns | Out-Null"
}

func withoutHost(data, host string) (string, bool) {
	lines := strings.Split(data, "\n")
	out := make([]string, 0, len(lines))
	changed := false
	for _, raw := range lines {
		body := strings.TrimSuffix(raw, "\r")
		eol := raw[len(body):]
		entry, comment := body, ""
		if i := strings.IndexByte(body, '#'); i >= 0 {
			entry, comment = body[:i], body[i:]
		}
		fields := strings.Fields(entry)
		if len(fields) < 2 {
			out = append(out, raw)
			continue
		}
		kept := []string{fields[0]}
		for _, name := range fields[1:] {
			if !strings.EqualFold(name, host) {
				kept = append(kept, name)
			}
		}
		if len(kept) == len(fields) {
			out = append(out, raw)
			continue
		}
		changed = true
		if len(kept) == 1 {
			continue
		}
		rebuilt := strings.Join(kept, " ")
		if comment != "" {
			rebuilt += " " + comment
		}
		out = append(out, rebuilt+eol)
	}
	return strings.Join(out, "\n"), changed
}
