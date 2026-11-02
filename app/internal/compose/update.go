package compose

import (
	"fmt"

	"github.com/ohcnetwork/care_desktop/app/internal/release"
)

type Update struct {
	Backend  string `json:"backend"`
	Frontend string `json:"frontend"`
}

func (u Update) Any() bool { return u.Backend != "" || u.Frontend != "" }

func Short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// DiscoverUpdate looks up the newest commit on each tracked branch and records
// any that the clinic is not running as Next. It only reads Git metadata: nothing
// is downloaded or built until the user installs the update with the clinic stopped.
func (b *Builder) DiscoverUpdate() (Update, error) {
	if b.stopped() {
		return Update{}, nil
	}
	var u Update
	for _, s := range []struct {
		service    string
		repo       string
		configured string
		dst        *string
	}{
		{Backend, b.set.BeRepo, b.set.BeRef, &u.Backend},
		{Frontend, b.set.FeRepo, b.set.FeRef, &u.Frontend},
	} {
		if release.IsCommitRef(s.configured) {
			continue
		}
		sha := head(b.run, s.repo, s.configured)
		if b.stopped() {
			return Update{}, nil
		}
		if sha == "" {
			b.logln("Couldn't reach the CARE " + s.service + " repository to check for an update.")
			continue
		}
		err := b.updateLock(s.service, func(c *Channel) {
			switch {
			case sha == c.Current:
				c.Next, c.Declined = "", ""
			case wanted(*c, sha):
				c.Next = sha
				*s.dst = sha
			}
		})
		if err != nil {
			return Update{}, err
		}
	}
	return u, nil
}

func wanted(c Channel, sha string) bool {
	return sha != "" && sha != c.Current && sha != c.Declined
}

func (b *Builder) Waiting() Update {
	lock := ReadLock(b.dir)
	return Update{Backend: lock.Backend.Next, Frontend: lock.Frontend.Next}
}

func (b *Builder) ApplyPending() (Update, error) {
	var applied Update
	err := ModifyLock(b.dir, func(lock *Lock) error {
		return b.applyPending(lock, &applied)
	})
	return applied, err
}

func (b *Builder) applyPending(lock *Lock, applied *Update) error {
	type staged struct {
		service string
		image   string
		dst     *string
	}
	var ready []staged
	for _, s := range []staged{
		{Backend, b.set.BackendImage, &applied.Backend},
		{Frontend, b.set.FrontendImage, &applied.Frontend},
	} {
		if lock.Get(s.service).Next == "" {
			continue
		}
		_, ok, err := b.builtFrom(s.image + "-next")
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("the staged CARE %s image %s-next is missing, so the update cannot be applied", s.service, s.image)
		}
		ready = append(ready, s)
	}
	for _, s := range ready {
		c := lock.Get(s.service)
		if err := b.run.Run("docker", "image", "tag", s.image+"-next", s.image); err != nil {
			return err
		}
		b.logln("CARE " + s.service + " updated to " + Short(c.Next) + ".")
		*s.dst = c.Next
		c.Current, c.Next, c.Declined = c.Next, "", ""
		lock.Set(s.service, c)
	}
	return nil
}

func (b *Builder) DeclineUpdate() error {
	return ModifyLock(b.dir, func(lock *Lock) error {
		for _, service := range []string{Backend, Frontend} {
			c := lock.Get(service)
			if c.Next != "" {
				c.Declined = c.Next
			}
			lock.Set(service, c)
		}
		return nil
	})
}

func (b *Builder) PruneDangling() { b.pruneDangling() }
