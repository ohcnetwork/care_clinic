package clinic

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/compose"
)

const (
	oldCommit = "1111111111111111111111111111111111111111"
	newCommit = "2222222222222222222222222222222222222222"
)

// fakeDocker keeps containers and images as files under $CARE_FAKE_STATE, so a
// test can assert what is running and which image carries which build instead
// of only reading a list of commands. CARE_FAKE_FAIL lists the steps that fail.
const fakeDocker = `#!/bin/sh
printf '%s\n' "$*" >> "$CARE_MIGRATION_TRACE"
st="$CARE_FAKE_STATE"
failing() { case " $CARE_FAKE_FAIL " in *" $1 "*) return 0 ;; esac; return 1; }
set_status() {
  read -r name _ < "$st/c/$1"
  printf '%s %s\n' "$name" "$2" > "$st/c/$1"
}
start_services() {
  for service do printf 'care-clinic-%s-1 running\n' "$service" > "$st/c/c-$service"; done
}
case "$1 $2" in
  'compose stop')
    if [ $# -gt 2 ]; then failing stop-workers && exit 1; exit 0; fi
    failing compose-stop && { echo 'compose stop failed' >&2; exit 1; }
    for f in "$st"/c/*; do
      [ -e "$f" ] || continue
      [ ! -e "$st/orphan-${f##*/}" ] || continue
      case "$(cat "$f")" in *' removing') continue ;; esac
      set_status "${f##*/}" exited
    done
    exit 0 ;;
  'compose ps')
    case "$*" in *--services*) printf 'caddy\n' ;; esac
    exit 0 ;;
  'compose down')
    failing down && exit 1
    for f in "$st"/c/*; do
      [ -e "$f" ] || continue
      [ -e "$st/orphan-${f##*/}" ] || rm -f "$f"
    done
    exit 0 ;;
  'compose up')
    case "$*" in
      *' db redis backend') failing up-backend && exit 1; start_services db redis backend; exit 0 ;;
      *--force-recreate*) failing up-proxy && exit 1; start_services caddy; exit 0 ;;
      *) failing up-all && exit 1; start_services db redis backend caddy; exit 0 ;;
    esac ;;
  'compose run') failing backup && exit 1; exit 0 ;;
  'compose exec')
    case "$*" in
      *' manage.py migrate --noinput') failing migrate && exit 1; exit 0 ;;
      *' manage.py createsuperuser '*|*' manage.py shell '*) exit 0 ;;
    esac
    exit 99 ;;
  'image ls')
    for last do :; done
    tag=${last#reference=}
    [ -f "$st/img/$tag" ] && printf 'sha256:%s\n' "$tag"
    exit 0 ;;
  'image inspect')
    for last do :; done
    cat "$st/img/$last" || exit 1
    exit 0 ;;
  'image tag')
    failing tag && exit 1
    cp "$st/img/$3" "$st/img/$4" || exit 1
    exit 0 ;;
  'image prune') exit 0 ;;
  'ps -aq')
    failing ps && { echo 'Cannot connect to the Docker daemon' >&2; exit 1; }
    for f in "$st"/c/*; do [ -e "$f" ] && printf '%s\n' "${f##*/}"; done
    exit 0 ;;
esac
case "$1" in
  inspect)
    failing inspect && { echo 'inspect failed' >&2; exit 1; }
    format=$3
    shift 3
    for id do
      read -r name status < "$st/c/$id" || exit 1
      if [ "$status" = removing ] && [ -e "$st/settle-$id" ]; then
        read -r left < "$st/settle-$id"
        if [ "$left" -le 0 ]; then set_status "$id" exited; status=exited
        else echo $((left - 1)) > "$st/settle-$id"; fi
      fi
      case "$format" in
        *'{{.Id}}'*) printf '%s /%s %s\n' "$id" "$name" "$status" ;;
        *) printf '%s\n' "$status" ;;
      esac
    done
    exit 0 ;;
  unpause)
    shift
    for id do
      read -r name status < "$st/c/$id" || exit 1
      [ "$status" != paused ] || set_status "$id" running
    done
    exit 0 ;;
  stop)
    shift 3
    for id do
      case "$(cat "$st/c/$id")" in *' removing') continue ;; esac
      [ -e "$st/stubborn-$id" ] || set_status "$id" exited
    done
    exit 0 ;;
  build)
    tag=; label=
    while [ $# -gt 0 ]; do
      case "$1" in
        -t) tag=$2; shift ;;
        --label) label=${2#*=}; shift ;;
      esac
      shift
    done
    case "$tag" in
      *-next) failing build-next && exit 1 ;;
      *) failing build && exit 1 ;;
    esac
    printf '%s\n' "$label" > "$st/img/$tag"
    exit 0 ;;
esac
exit 99
`

type logSink struct {
	mu    sync.Mutex
	lines []string
}

func (l *logSink) add(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, s)
}

func (l *logSink) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

// lifecycle is a clinic whose "docker" is fakeDocker, with CARE running on
// oldCommit and no update offered.
type lifecycle struct {
	t     *testing.T
	e     *Clinic
	state string
	trace string
	logs  *logSink
}

// fastStopSettle shortens the pause StopStack takes between its rounds.
func fastStopSettle(t *testing.T) {
	t.Helper()
	old := stopSettle
	stopSettle = time.Millisecond
	t.Cleanup(func() { stopSettle = old })
}

func fixtureHash(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])[:12]
}

func backendKey(commit string) string {
	return "fixture+" + commit + "+repo@" + fixtureHash("fixture-repo")
}

func frontendKey(commit string) string { return backendKey(commit) + "+env@" + fixtureHash("") }

func newLifecycle(t *testing.T) *lifecycle {
	t.Helper()
	e, _, _ := migrationFixture(t)
	root := e.InstallDir
	f := &lifecycle{t: t, e: e, state: filepath.Join(root, "state"), trace: filepath.Join(root, "calls"), logs: &logSink{}}
	for _, dir := range []string{"c", "img"} {
		if err := os.MkdirAll(filepath.Join(f.state, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "docker"), []byte(fakeDocker), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CARE_FAKE_STATE", f.state)
	t.Setenv("CARE_FAKE_FAIL", "")

	e.Pins.BeRef, e.Pins.FeRef = "develop", "develop"
	e.Log = f.logs.add
	e.localSetupOffered = true
	e.startReady = func() error { return nil }
	fastStopSettle(t)

	f.image("fixture-backend", backendKey(oldCommit))
	f.image("fixture-frontend", frontendKey(oldCommit))
	f.image("fixture-backup", os.Getenv("CARE_BACKUP_FINGERPRINT"))
	f.image("fixture-caddy", os.Getenv("CARE_CADDY_FINGERPRINT"))
	for _, service := range []string{"db", "redis", "backend", "caddy"} {
		f.container("c-"+service, "care-clinic-"+service+"-1", "running")
	}
	f.must(compose.WriteLock(root, compose.Lock{
		Backend:  compose.Channel{Ref: "develop", Current: oldCommit},
		Frontend: compose.Channel{Ref: "develop", Current: oldCommit},
	}))
	// Builds reuse a downloaded source, so no test needs git or the network.
	for dir, commit := range map[string]string{
		"backend": oldCommit, "frontend": oldCommit, "backend-next": newCommit, "frontend-next": newCommit,
	} {
		src := filepath.Join(root, "src", dir)
		f.must(os.MkdirAll(src, 0o700))
		f.must(os.WriteFile(filepath.Join(src, ".care-source"), []byte(backendKey(commit)), 0o600))
	}
	return f
}

func (f *lifecycle) must(err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *lifecycle) container(id, name, status string) {
	f.t.Helper()
	f.must(os.WriteFile(filepath.Join(f.state, "c", id), []byte(name+" "+status+"\n"), 0o600))
}

func (f *lifecycle) flag(name string) {
	f.t.Helper()
	f.must(os.WriteFile(filepath.Join(f.state, name), []byte("1\n"), 0o600))
}

func (f *lifecycle) setting(name, value string) {
	f.t.Helper()
	f.must(os.WriteFile(filepath.Join(f.state, name), []byte(value+"\n"), 0o600))
}

func (f *lifecycle) remove(id string) {
	f.t.Helper()
	f.must(os.Remove(filepath.Join(f.state, "c", id)))
}

func (f *lifecycle) image(tag, label string) {
	f.t.Helper()
	f.must(os.WriteFile(filepath.Join(f.state, "img", tag), []byte(label+"\n"), 0o600))
}

func (f *lifecycle) imageLabel(tag string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(f.state, "img", tag))
	return strings.TrimSpace(string(data)), err == nil
}

func (f *lifecycle) fail(steps ...string) {
	f.t.Setenv("CARE_FAKE_FAIL", strings.Join(steps, " "))
}

func (f *lifecycle) status(id string) string {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.state, "c", id))
	if err != nil {
		f.t.Fatal(err)
	}
	return strings.Fields(string(data))[1]
}

// running lists the containers that are running, by id.
func (f *lifecycle) running() []string {
	f.t.Helper()
	entries, err := os.ReadDir(filepath.Join(f.state, "c"))
	if err != nil {
		f.t.Fatal(err)
	}
	var ids []string
	for _, entry := range entries {
		if f.status(entry.Name()) == "running" {
			ids = append(ids, entry.Name())
		}
	}
	return ids
}

func (f *lifecycle) offer(backend, frontend string) {
	f.t.Helper()
	lock := compose.ReadLock(f.e.InstallDir)
	lock.Backend.Next, lock.Frontend.Next = backend, frontend
	f.must(compose.WriteLock(f.e.InstallDir, lock))
}

func (f *lifecycle) lock() compose.Lock { return compose.ReadLock(f.e.InstallDir) }

func (f *lifecycle) encryptBackups() {
	f.t.Helper()
	keys := filepath.Join(f.e.InstallDir, "keys")
	f.must(os.MkdirAll(keys, 0o700))
	f.must(os.WriteFile(filepath.Join(keys, "backup-cert.pem"), []byte("certificate"), 0o600))
}

// calls returns every docker command run so far, one per element.
func (f *lifecycle) calls() callLog {
	f.t.Helper()
	data, err := os.ReadFile(f.trace)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		f.t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

type callLog []string

// first returns the position of the first command that starts with prefix, or -1.
func (c callLog) first(prefix string) int {
	for i, call := range c {
		if strings.HasPrefix(call, prefix) {
			return i
		}
	}
	return -1
}

func (c callLog) count(prefix string) int {
	n := 0
	for _, call := range c {
		if strings.HasPrefix(call, prefix) {
			n++
		}
	}
	return n
}

// exact returns the position of the first command equal to call, or -1.
func (c callLog) exact(call string) int {
	for i, got := range c {
		if got == call {
			return i
		}
	}
	return -1
}

// positions returns where every command equal to call ran.
func (c callLog) positions(call string) []int {
	var at []int
	for i, got := range c {
		if got == call {
			at = append(at, i)
		}
	}
	return at
}

func (c callLog) after(prefix string, position int) int {
	for i := position + 1; i < len(c); i++ {
		if strings.HasPrefix(c[i], prefix) {
			return i
		}
	}
	return -1
}

// assertDataKept fails when a command could delete the clinic's data or images.
func assertDataKept(t *testing.T, calls []string, allowDown bool) {
	t.Helper()
	for _, call := range calls {
		switch {
		case strings.Contains(call, "--volumes"), strings.HasPrefix(call, "volume "),
			strings.HasPrefix(call, "rm "), strings.HasPrefix(call, "container rm"),
			strings.HasPrefix(call, "image rm"), strings.HasPrefix(call, "system "),
			strings.HasPrefix(call, "network "), strings.HasPrefix(call, "kill "):
			t.Fatalf("a command that can delete data or images ran: %s", call)
		case strings.HasPrefix(call, "compose down") && !allowDown:
			t.Fatalf("the stack was torn down instead of stopped: %s", call)
		}
	}
}
