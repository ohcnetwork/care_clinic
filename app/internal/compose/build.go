package compose

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/compose-spec/compose-go/v2/dotenv"
	"github.com/ohcnetwork/care_desktop/app/internal/lines"
	"github.com/ohcnetwork/care_desktop/app/internal/plugins"
	"github.com/ohcnetwork/care_desktop/app/internal/release"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

const (
	backendPluginsDockerfile  = "backend-plugins.Dockerfile"
	frontendRebuildDockerfile = "frontend-rebuild.Dockerfile"
	// PlaceholderPrefix marks a frontend setting the image fills in at start.
	// It is URL-shaped so CARE's build-time validation accepts it for settings
	// that must be URLs; images/frontend/configure matches the same pattern.
	PlaceholderPrefix = "https://care-clinic.invalid/__CARE_CLINIC_"
)

type Builder struct {
	Log  func(string)
	Stop func() bool

	dir string // install dir: holds the lock, manifest cache and Dockerfiles
	set *release.Pins
	run proc.Runner

	pending bool
	staged  map[string]Release

	owner   *Builder
	groups  atomic.Int32
	rebuilt atomic.Bool
}

func NewBuilder(run proc.Runner, dir string, set *release.Pins, log func(string)) *Builder {
	return &Builder{Log: log, dir: dir, set: set, run: run}
}

// Pending returns a builder for the "-next" images of a staged update.
func (b *Builder) Pending() *Builder {
	next := NewBuilder(b.run, b.dir, b.set, b.Log)
	next.pending = true
	next.Stop = b.Stop
	return next
}

// staging returns a Pending builder that builds the given releases.
func (b *Builder) staging(releases map[string]Release) *Builder {
	next := b.Pending()
	next.staged = releases
	return next
}

func (b *Builder) suffixed(name string) string {
	if b.pending {
		return name + "-next"
	}
	return name
}

func (b *Builder) stopped() bool { return b.Stop != nil && b.Stop() }

func (b *Builder) logln(s string) {
	if b.Log != nil {
		b.Log(s)
	}
}

func (b *Builder) linesSource() string { return lines.Source(b.set.LinesURL) }

// LineID is the release line this clinic follows.
func (b *Builder) LineID() string {
	if id := ReadLock(b.dir).Line; id != "" {
		return id
	}
	return b.set.DefaultLine
}

// target picks the release a service should run, and the line it came from
// when it was not already recorded in the lock.
func (b *Builder) target(service string) (Release, string, error) {
	if rel, ok := b.staged[service]; ok {
		return rel, "", nil
	}
	c := ReadLock(b.dir).Get(service)
	if b.pending {
		if c.Next == nil {
			return Release{}, "", fmt.Errorf("no CARE %s update is staged", service)
		}
		return *c.Next, "", nil
	}
	if c.Current.Image != "" {
		return c.Current, "", nil
	}
	id := b.LineID()
	m, err := lines.Load(b.dir, b.linesSource())
	if err != nil {
		return Release{}, "", fmt.Errorf("couldn't download the list of CARE releases; check the network connection: %w", err)
	}
	l, ok := m.Line(id)
	if !ok {
		return Release{}, "", fmt.Errorf("CARE release line %q is not published", id)
	}
	if err := l.SupportsApp(b.set.AppVersion); err != nil {
		return Release{}, "", err
	}
	if service == Frontend {
		return l.Frontend, id, nil
	}
	return l.Backend, id, nil
}

func (b *Builder) recordCurrent(service string, rel Release, line string) error {
	if b.pending {
		return nil
	}
	err := b.updateLock(func(l *Lock) {
		c := l.Get(service)
		c.Current = rel
		if c.Next != nil && c.Next.Image == rel.Image {
			c.Next, c.Declined = nil, ""
		}
		l.Set(service, c)
		if l.Line == "" && line != "" {
			l.Line = line
		}
	})
	if err == nil {
		removeLegacySource(b.dir)
	}
	return err
}

// BackendRef is the CARE backend commit this builder targets.
func (b *Builder) BackendRef() string {
	rel, _, _ := b.target(Backend)
	return rel.Commit
}

// FrontendRef is the CARE frontend commit this builder targets.
func (b *Builder) FrontendRef() string {
	rel, _, _ := b.target(Frontend)
	return rel.Commit
}

type plan struct {
	service string
	rel     Release
	line    string
	tag     string
	key     string
	build   func() error
}

func (b *Builder) backendPlan() (plan, error) {
	plugs, err := plugins.New(b.dir).AdditionalPlugs()
	if err != nil {
		return plan{}, err
	}
	rel, line, err := b.target(Backend)
	if err != nil {
		return plan{}, err
	}
	p := plan{service: Backend, rel: rel, line: line, tag: b.suffixed(b.set.BackendImage), key: rel.Image}
	if plugs == "" {
		p.build = func() error { return b.relabel(rel.Image, p.tag, p.key, "backend") }
		return p, nil
	}
	p.key += "+plugs@" + shortHash([]byte(plugs))
	p.build = func() error { return b.buildBackendPlugins(rel, plugs, p.tag, p.key) }
	return p, nil
}

func (b *Builder) buildBackendPlugins(rel Release, plugs, tag, key string) error {
	if err := b.pull(rel.Builder); err != nil {
		return err
	}
	if err := b.pull(rel.Image); err != nil {
		return err
	}
	ctx, cleanup, err := emptyBuildContext(b.dir)
	if err != nil {
		return err
	}
	defer cleanup()
	b.logln("Adding plugins to the backend image (" + tag + ")... (a few minutes)")
	return b.run.Run("docker", "build", "-f", filepath.Join(b.dir, backendPluginsDockerfile),
		"--build-arg", "BACKEND_BUILDER="+rel.Builder,
		"--build-arg", "BACKEND_IMAGE="+rel.Image,
		"--build-arg", "ADDITIONAL_PLUGS="+plugs,
		"--label", builtFromLabel+"="+key,
		"-t", tag, ctx)
}

func (b *Builder) rebuild(p plan, err error) error {
	if err != nil {
		return err
	}
	if err := p.build(); err != nil {
		return err
	}
	return b.recordCurrent(p.service, p.rel, p.line)
}

func (b *Builder) ensurePlan(p plan, err error) error {
	if err != nil {
		return err
	}
	if err := b.ensure(p.tag, p.key, p.service, p.build); err != nil {
		return err
	}
	return b.recordCurrent(p.service, p.rel, p.line)
}

func (b *Builder) current(p plan, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	got, ok, err := b.builtFrom(p.tag)
	if err != nil {
		return false, err
	}
	return ok && got == p.key, nil
}

// BuildBackend recreates the backend image even when it looks current.
func (b *Builder) BuildBackend() error { return b.rebuild(b.backendPlan()) }

func (b *Builder) EnsureBackendImage() error { return b.ensurePlan(b.backendPlan()) }

func (b *Builder) BackendImageCurrent() (bool, error) { return b.current(b.backendPlan()) }

func (b *Builder) frontendPlan() (plan, error) {
	data, err := os.ReadFile(filepath.Join(b.dir, "frontend.env"))
	if err != nil {
		return plan{}, err
	}
	env, err := dotenv.Parse(bytes.NewReader(data))
	if err != nil {
		return plan{}, fmt.Errorf("frontend.env: %w", err)
	}
	rel, line, err := b.target(Frontend)
	if err != nil {
		return plan{}, err
	}
	p := plan{service: Frontend, rel: rel, line: line, tag: b.suffixed(b.set.FrontendImage), key: rel.Image}
	merged, changed := BuildSettings(rel, env)
	if !changed {
		p.build = func() error { return b.relabel(rel.Image, p.tag, p.key, "frontend") }
		return p, nil
	}
	p.key += "+env@" + shortHash([]byte(renderEnv(merged)))
	p.build = func() error { return b.buildFrontend(rel, merged, p.tag, p.key) }
	return p, nil
}

// BuildSettings merges the clinic's compile-time frontend settings into the
// release's, reporting whether they differ so the bundle must be rebuilt.
// Settings the image applies at start, and blank values, never force a rebuild.
func BuildSettings(rel Release, env map[string]string) (map[string]string, bool) {
	merged := make(map[string]string, len(rel.BuildEnv))
	for k, v := range rel.BuildEnv {
		merged[k] = v
	}
	changed := false
	for k, v := range env {
		if v == "" || slices.Contains(rel.RuntimeKeys, k) {
			continue
		}
		if cur, ok := merged[k]; !ok || cur != v {
			merged[k] = v
			changed = true
		}
	}
	return merged, changed
}

func (b *Builder) buildFrontend(rel Release, merged map[string]string, tag, key string) error {
	if err := b.pull(rel.Builder); err != nil {
		return err
	}
	if err := b.pull(rel.Image); err != nil {
		return err
	}
	ctx, cleanup, err := emptyBuildContext(b.dir)
	if err != nil {
		return err
	}
	defer cleanup()
	env := make(map[string]string, len(merged)+len(rel.RuntimeKeys))
	for k, v := range merged {
		env[k] = v
	}
	for _, k := range rel.RuntimeKeys {
		env[k] = PlaceholderPrefix + k + "__"
	}
	if err := os.WriteFile(filepath.Join(ctx, "clinic.env"), []byte(renderEnv(env)), 0o644); err != nil {
		return err
	}
	b.logln("Rebuilding the frontend with this clinic's build settings (" + tag + ")... (a few minutes)")
	return b.run.Run("docker", "build", "-f", filepath.Join(b.dir, frontendRebuildDockerfile),
		"--build-arg", "FRONTEND_BUILDER="+rel.Builder,
		"--build-arg", "FRONTEND_IMAGE="+rel.Image,
		"--label", builtFromLabel+"="+key,
		"-t", tag, ctx)
}

// renderEnv writes a dotenv file with stable ordering.
func renderEnv(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out strings.Builder
	for _, k := range keys {
		out.WriteString(k + "=" + quoteEnv(env[k]) + "\n")
	}
	return out.String()
}

func quoteEnv(v string) string {
	v = strings.NewReplacer("\r", "", "\n", " ").Replace(v)
	switch {
	case !strings.Contains(v, "'"):
		return "'" + v + "'"
	case !strings.Contains(v, `"`):
		return `"` + strings.ReplaceAll(v, `\`, `\\`) + `"`
	}
	return "`" + strings.ReplaceAll(v, "`", "") + "`"
}

// BuildFrontend recreates the frontend image even when it looks current.
func (b *Builder) BuildFrontend() error { return b.rebuild(b.frontendPlan()) }

func (b *Builder) EnsureFrontendImage() error { return b.ensurePlan(b.frontendPlan()) }

func (b *Builder) FrontendImageCurrent() (bool, error) { return b.current(b.frontendPlan()) }

func emptyBuildContext(parent string) (dir string, cleanup func(), err error) {
	dir, err = os.MkdirTemp(parent, ".care-buildctx-")
	if err != nil {
		return "", nil, err
	}
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}

const relabelDockerfile = "ARG IMAGE\nFROM ${IMAGE}\n"

// relabel tags a published image locally, recording where it came from.
func (b *Builder) relabel(src, tag, key, label string) error {
	if err := b.pull(src); err != nil {
		return err
	}
	ctx, cleanup, err := emptyBuildContext(b.dir)
	if err != nil {
		return err
	}
	defer cleanup()
	df := filepath.Join(ctx, "Dockerfile")
	if err := os.WriteFile(df, []byte(relabelDockerfile), 0o644); err != nil {
		return err
	}
	b.logln("Preparing the " + label + " image (" + tag + ")...")
	return b.run.Run("docker", "build", "-f", df,
		"--build-arg", "IMAGE="+src,
		"--label", builtFromLabel+"="+key,
		"-t", tag, ctx)
}

// pull downloads a published image unless it is already on this computer.
func (b *Builder) pull(ref string) error {
	if _, err := b.run.Capture("docker", "image", "inspect", "--format", "{{.Id}}", ref); err == nil {
		return nil
	}
	b.logln("Downloading " + ref + "...")
	if err := b.run.Run("docker", "pull", ref); err != nil {
		return fmt.Errorf("couldn't download %s; check the network connection: %w", ref, err)
	}
	return nil
}

func (b *Builder) BuildBackup() error {
	if src := b.set.BackupPrebuilt; src != "" {
		return b.relabel(src, b.set.BackupImage, src, "backup")
	}
	key, err := b.backupBuiltFrom()
	if err != nil {
		return err
	}
	b.logln("Building the backup image (" + b.set.BackupImage + ")...")
	df := filepath.Join(b.dir, "backup.Dockerfile")
	buildCtx, cleanup, err := emptyBuildContext(b.dir)
	if err != nil {
		return err
	}
	defer cleanup()
	return b.run.Run("docker", "build", "-f", df,
		"--build-arg", "POSTGRES_IMAGE="+b.set.PostgresImage,
		"--label", builtFromLabel+"="+key,
		"-t", b.set.BackupImage, buildCtx)
}

func (b *Builder) EnsureBackupImage() error {
	key, err := b.backupBuiltFrom()
	if err != nil {
		return err
	}
	return b.ensure(b.set.BackupImage, key, "backup", b.BuildBackup)
}

func (b *Builder) backupBuiltFrom() (string, error) {
	if b.set.BackupPrebuilt != "" {
		return b.set.BackupPrebuilt, nil
	}
	df, err := os.ReadFile(filepath.Join(b.dir, "backup.Dockerfile"))
	if err != nil {
		return "", err
	}
	return b.set.PostgresImage + "+dockerfile@" + shortHash(df), nil
}

func (b *Builder) BuildCaddy() error {
	if src := b.set.CaddyWafPrebuilt; src != "" {
		return b.relabel(src, b.set.CaddyWafImage, src, "Caddy + WAF")
	}
	key, err := b.caddyBuiltFrom()
	if err != nil {
		return err
	}
	b.logln("Building the Caddy + WAF image (" + b.set.CaddyWafImage + ")... (compiles Caddy; a few minutes)")
	df := filepath.Join(b.dir, "caddy.Dockerfile")
	buildCtx, cleanup, err := emptyBuildContext(b.dir)
	if err != nil {
		return err
	}
	defer cleanup()
	return b.run.Run("docker", "build", "-f", df,
		"--build-arg", "CADDY_IMAGE="+b.set.CaddyImage,
		"--build-arg", "CORAZA_VERSION="+b.set.CorazaVersion,
		"--label", builtFromLabel+"="+key,
		"-t", b.set.CaddyWafImage, buildCtx)
}

func (b *Builder) caddyBuiltFrom() (string, error) {
	if b.set.CaddyWafPrebuilt != "" {
		return b.set.CaddyWafPrebuilt, nil
	}
	df, err := os.ReadFile(filepath.Join(b.dir, "caddy.Dockerfile"))
	if err != nil {
		return "", err
	}
	return b.set.CaddyImage + "+coraza@" + b.set.CorazaVersion + "+dockerfile@" + shortHash(df), nil
}

func (b *Builder) EnsureCaddyImage() error {
	key, err := b.caddyBuiltFrom()
	if err != nil {
		return err
	}
	return b.ensure(b.set.CaddyWafImage, key, "caddy", b.BuildCaddy)
}

func shortHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:12]
}

const builtFromLabel = "org.opencontainers.image.base.name"

func (b *Builder) ensure(tag, want, label string, build func() error) error {
	got, ok, err := b.builtFrom(tag)
	if err != nil {
		return err
	}
	switch {
	case !ok:
		return build()
	case got == want:
		return nil
	}
	b.logln("The " + label + " image came from " + quoteOrUnknown(got) +
		", but this release expects " + want + " - replacing it.")
	if err := build(); err != nil {
		return err
	}
	b.afterRebuild()
	return nil
}

func (b *Builder) afterRebuild() {
	if b.owner != nil {
		b.owner.rebuilt.Store(true)
		return
	}
	b.pruneDangling()
}

func (b *Builder) pruneDangling() {
	cmd := proc.Command("docker", "image", "prune", "-f")
	cmd.Env = b.run.Env
	if err := cmd.Run(); err == nil {
		b.logln("Cleared out images left behind by the rebuild.")
	}
}

func (b *Builder) builtFrom(tag string) (value string, ok bool, err error) {
	ids, err := b.run.Capture("docker", "image", "ls", "--quiet", "--filter", "reference="+tag)
	if err != nil {
		return "", false, fmt.Errorf("inspect local images: %w", err)
	}
	if ids == "" {
		return "", false, nil
	}
	out, err := b.run.Capture("docker", "image", "inspect", "-f",
		"{{index .Config.Labels \""+builtFromLabel+"\"}}", tag)
	if err != nil {
		return "", false, fmt.Errorf("inspect image %s: %w", tag, err)
	}
	v := strings.TrimSpace(out)
	if v == "<no value>" {
		v = ""
	}
	return v, true, nil
}

func quoteOrUnknown(s string) string {
	if s == "" {
		return "an unrecorded version"
	}
	return s
}
