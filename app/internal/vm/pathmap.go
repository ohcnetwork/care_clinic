package vm

import (
	"runtime"
	"strings"
)

// needsSync reports whether host folders must be copied into the VM because
// there is no folder sharing (Windows).
func needsSync() bool { return runtime.GOOS == "windows" }

// windowsToGuest maps C:\Users\a\x to /c/Users/a/x, the path the engine
// keeps a synced copy under. It is the same form compose uses with
// COMPOSE_CONVERT_WINDOWS_PATHS=1.
func windowsToGuest(p string) (string, bool) {
	if len(p) < 3 || p[1] != ':' || (p[2] != '\\' && p[2] != '/') {
		return "", false
	}
	d := p[0] | 0x20
	if d < 'a' || d > 'z' {
		return "", false
	}
	rest := strings.ReplaceAll(p[2:], `\`, "/")
	for strings.Contains(rest, "//") {
		rest = strings.ReplaceAll(rest, "//", "/")
	}
	return "/" + string(d) + strings.TrimSuffix(rest, "/"), true
}

// GuestPath is where a host path appears inside the engine.
func GuestPath(p string) string {
	if runtime.GOOS == "windows" {
		if g, ok := windowsToGuest(p); ok {
			return g
		}
	}
	return p
}

// RewriteDockerArgs maps Windows bind-mount sources in docker run/create
// arguments to their in-engine paths and returns the host paths that must be
// synced first. Other commands are returned unchanged.
func RewriteDockerArgs(args []string) ([]string, []string) {
	if !isRunLike(args) {
		return args, nil
	}
	out := make([]string, len(args))
	copy(out, args)
	var paths []string
	for i := 0; i < len(out); i++ {
		a := out[i]
		switch {
		case a == "-v" || a == "--volume":
			if i+1 < len(out) {
				out[i+1], paths = rewriteVolume(out[i+1], paths)
				i++
			}
		case strings.HasPrefix(a, "--volume="):
			v, p := rewriteVolume(strings.TrimPrefix(a, "--volume="), paths)
			out[i], paths = "--volume="+v, p
		case a == "--mount":
			if i+1 < len(out) {
				out[i+1], paths = rewriteMount(out[i+1], paths)
				i++
			}
		case strings.HasPrefix(a, "--mount="):
			v, p := rewriteMount(strings.TrimPrefix(a, "--mount="), paths)
			out[i], paths = "--mount="+v, p
		case a == "--":
			return out, paths
		}
	}
	return out, paths
}

func isRunLike(args []string) bool {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			// Global docker flags that take a separate value.
			switch a {
			case "-l", "--log-level", "-c", "--context", "-H", "--host", "--config":
				i++
			}
			continue
		}
		switch a {
		case "run", "create":
			return true
		case "container":
			return i+1 < len(args) && (args[i+1] == "run" || args[i+1] == "create")
		}
		return false
	}
	return false
}

func rewriteVolume(v string, paths []string) (string, []string) {
	if _, ok := windowsToGuest(v); !ok {
		return v, paths
	}
	// "C:\src:/dst[:opts]": the drive colon is part of the source.
	rest := v[2:]
	idx := strings.Index(rest, ":")
	if idx < 0 {
		return v, paths
	}
	src := v[:2+idx]
	g, _ := windowsToGuest(src)
	return g + rest[idx:], append(paths, src)
}

func rewriteMount(v string, paths []string) (string, []string) {
	fields := strings.Split(v, ",")
	bind := false
	for _, f := range fields {
		if f == "type=bind" {
			bind = true
		}
	}
	if !bind {
		return v, paths
	}
	for i, f := range fields {
		k, val, ok := strings.Cut(f, "=")
		if !ok || (k != "source" && k != "src") {
			continue
		}
		if g, ok := windowsToGuest(val); ok {
			fields[i] = k + "=" + g
			paths = append(paths, val)
		}
	}
	return strings.Join(fields, ","), paths
}

// MountSensitive reports whether a docker command may read or write bind
// mounts, so synced folders must be current before it and pulled back after.
func MountSensitive(args []string) bool {
	var words []string
	for i := 0; i < len(args) && len(words) < 2; i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			switch a {
			case "-l", "--log-level", "-c", "--context", "-H", "--host", "--config", "-f", "--file", "-p", "--project-name", "--env-file", "--profile", "--project-directory":
				i++
			}
			continue
		}
		words = append(words, a)
	}
	if len(words) == 0 {
		return false
	}
	switch words[0] {
	case "run", "create", "start", "exec":
		return true
	case "container":
		return len(words) > 1 && (words[1] == "run" || words[1] == "create" || words[1] == "start" || words[1] == "exec")
	case "compose":
		if len(words) < 2 {
			return false
		}
		switch words[1] {
		case "up", "run", "create", "start", "restart", "exec":
			return true
		}
	}
	return false
}
