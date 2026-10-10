package compose

import (
	"errors"
	"fmt"

	"github.com/ohcnetwork/care_desktop/app/internal/lines"
)

type Update struct {
	Backend  string `json:"backend"`
	Frontend string `json:"frontend"`
	// Line is set when the update moves the clinic to a newer release line.
	Line string `json:"line,omitempty"`
	// NewerLine names the newest release line this clinic could upgrade to.
	NewerLine string `json:"newer_line,omitempty"`
}

func (u Update) Any() bool { return u.Backend != "" || u.Frontend != "" }

func Short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// NewerLine is the newest published line this app can run that is newer than
// the clinic's line, or "".
func NewerLine(m *lines.Manifest, current, appVersion string) string {
	newer := m.Newer(current)
	for i := len(newer) - 1; i >= 0; i-- {
		if newer[i].SupportsApp(appVersion) == nil {
			return newer[i].ID
		}
	}
	return ""
}

// PrepareUpdate checks the clinic's release line for newer images and
// downloads them as "-next" images, ready to apply.
func (b *Builder) PrepareUpdate() (Update, error) {
	if b.stopped() {
		return Update{}, nil
	}
	lock := ReadLock(b.dir)
	if lock.Backend.Current.Image == "" || lock.Frontend.Current.Image == "" || lock.NextLine != "" {
		return Update{}, nil
	}
	m, err := lines.Fetch(b.dir, b.linesSource())
	if err != nil {
		return Update{}, err
	}
	id := b.LineID()
	u := Update{NewerLine: NewerLine(m, id, b.set.AppVersion)}
	l, ok := m.Line(id)
	if !ok {
		b.logln("CARE release line " + id + " is no longer published; choose a newer line to keep receiving updates.")
		return u, nil
	}
	if err := l.SupportsApp(b.set.AppVersion); err != nil {
		b.logln("CARE updates were not downloaded: " + err.Error())
		return u, nil
	}
	staged := map[string]Release{}
	if wanted(lock.Backend, l.Backend) {
		staged[Backend] = l.Backend
	}
	if wanted(lock.Frontend, l.Frontend) {
		staged[Frontend] = l.Frontend
	}
	got, err := b.stage(staged, "")
	got.NewerLine = u.NewerLine
	return got, err
}

// StageLine downloads both CARE images of a newer release line, ready for
// ApplyPending to switch the clinic over.
func (b *Builder) StageLine(id string) (Update, error) {
	m, err := lines.Fetch(b.dir, b.linesSource())
	if err != nil {
		return Update{}, err
	}
	current := b.LineID()
	if !m.IsNewer(current, id) {
		return Update{}, fmt.Errorf("CARE release line %q is not newer than %q", id, current)
	}
	l, _ := m.Line(id)
	if err := l.SupportsApp(b.set.AppVersion); err != nil {
		return Update{}, err
	}
	u, err := b.stage(map[string]Release{Backend: l.Backend, Frontend: l.Frontend}, id)
	if err != nil {
		return Update{}, err
	}
	if b.stopped() {
		return Update{}, errors.New("the line upgrade was interrupted")
	}
	return u, nil
}

func (b *Builder) stage(staged map[string]Release, line string) (Update, error) {
	var u Update
	next := b.staging(staged)
	for _, s := range []struct {
		service string
		ensure  func() error
		dst     *string
	}{
		{Frontend, next.EnsureFrontendImage, &u.Frontend},
		{Backend, next.EnsureBackendImage, &u.Backend},
	} {
		rel, ok := staged[s.service]
		if !ok {
			continue
		}
		if next.stopped() {
			return Update{}, nil
		}
		b.logln("A newer CARE " + s.service + " is available (" + Short(rel.Commit) + ") - downloading it in the background.")
		if err := s.ensure(); err != nil {
			return Update{}, err
		}
		if next.stopped() {
			return Update{}, nil
		}
		if err := b.updateLock(func(l *Lock) {
			c := l.Get(s.service)
			r := rel
			c.Next = &r
			l.Set(s.service, c)
		}); err != nil {
			return Update{}, err
		}
		*s.dst = rel.Commit
	}
	if line != "" && u.Any() {
		u.Line = line
		if err := b.updateLock(func(l *Lock) { l.NextLine = line }); err != nil {
			return Update{}, err
		}
	}
	return u, nil
}

func wanted(c Channel, rel Release) bool {
	return rel.Commit != "" && rel.Image != c.Current.Image && rel.Commit != c.Declined
}

func (b *Builder) Waiting() Update {
	lock := ReadLock(b.dir)
	u := Update{Line: lock.NextLine}
	if lock.Backend.Next != nil {
		u.Backend = lock.Backend.Next.Commit
	}
	if lock.Frontend.Next != nil {
		u.Frontend = lock.Frontend.Next.Commit
	}
	return u
}

func (b *Builder) ApplyPending() (Update, error) {
	var applied Update
	err := ModifyLock(b.dir, func(lock *Lock) error {
		return b.applyPending(lock, &applied)
	})
	return applied, err
}

func (b *Builder) applyPending(lock *Lock, applied *Update) error {
	for _, s := range []struct {
		service string
		image   string
		dst     *string
	}{
		{Backend, b.set.BackendImage, &applied.Backend},
		{Frontend, b.set.FrontendImage, &applied.Frontend},
	} {
		c := lock.Get(s.service)
		if c.Next == nil {
			continue
		}
		staged := s.image + "-next"
		_, ok, err := b.builtFrom(staged)
		if err != nil {
			return err
		}
		if !ok {
			c.Next, c.Declined = nil, ""
			lock.Set(s.service, c)
			continue
		}
		if err := b.run.Run("docker", "image", "tag", staged, s.image); err != nil {
			return err
		}
		b.logln("CARE " + s.service + " updated to " + Short(c.Next.Commit) + ".")
		*s.dst = c.Next.Commit
		c.Current, c.Next, c.Declined = *c.Next, nil, ""
		lock.Set(s.service, c)
	}
	if lock.NextLine != "" {
		if applied.Any() {
			lock.Line = lock.NextLine
			applied.Line = lock.NextLine
			b.logln("This clinic now follows CARE release line " + lock.Line + ".")
		}
		lock.NextLine = ""
	}
	return nil
}

func (b *Builder) DeclineUpdate() error {
	return ModifyLock(b.dir, func(lock *Lock) error {
		lineUpgrade := lock.NextLine != ""
		lock.NextLine = ""
		for _, service := range []string{Backend, Frontend} {
			c := lock.Get(service)
			switch {
			case c.Next == nil:
			case lineUpgrade:
				c.Next = nil
			default:
				c.Declined = c.Next.Commit
			}
			lock.Set(service, c)
		}
		return nil
	})
}

func (b *Builder) PruneDangling() { b.pruneDangling() }
