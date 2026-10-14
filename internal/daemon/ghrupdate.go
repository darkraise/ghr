package daemon

import (
	"context"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/selfupdate"
)

// ghrState is what the daemon knows of ghr's own releases; b.ghrMu guards it.
type ghrState struct {
	latest    string // the newest release tag the last good check read
	checkedAt time.Time
	checkErr  string
	announced string // the newest tag an event has announced
}

// background is the context of the daemon's own goroutines, which
// StopBackground ends.
func (b *Backend) background() context.Context {
	b.bgOnce.Do(func() { b.bgCtx, b.bgCancel = context.WithCancel(context.Background()) })
	return b.bgCtx
}

// StopBackground ends the ghr update check and a ghr install that has not
// begun its swap; the daemon calls it on shutdown, then Wait.
func (b *Backend) StopBackground() {
	b.background()
	b.bgCancel()
}

// Wait waits for the goroutines StopBackground ends.
func (b *Backend) Wait() { b.bg.Wait() }

// StartGhrChecks runs the ghr_update_check task until StopBackground: when
// its timer fires, and at once on Run now or a config change.
func (b *Backend) StartGhrChecks() {
	ctx := b.background()
	wake := make(chan struct{}, 1)
	b.Jobs.Register(config.JobGhrUpdateCheck, func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	})
	b.bg.Add(1)
	go func() {
		defer b.bg.Done()
		timer := time.NewTimer(b.Jobs.Until(config.JobGhrUpdateCheck, b.now()))
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-wake:
			case <-timer.C:
			}
			if b.Jobs.Due(config.JobGhrUpdateCheck, b.now()) {
				b.checkGhr(ctx)
			}
			timer.Reset(b.Jobs.Until(config.JobGhrUpdateCheck, b.now()))
		}
	}()
}

// checkGhr asks GitHub for ghr's newest release. A failure keeps the last
// good tag; the task's schedule sets the next try.
func (b *Backend) checkGhr(ctx context.Context) {
	b.Jobs.Started(config.JobGhrUpdateCheck, b.now())
	tag, err := selfupdate.Latest(ctx, b.HTTP, b.ReleaseBase)
	now := b.now()
	announce := false
	b.ghrMu.Lock()
	if err != nil {
		b.ghr.checkErr = err.Error()
	} else {
		b.ghr.latest, b.ghr.checkedAt, b.ghr.checkErr = tag, now, ""
		if newerThan(tag, b.Version) && b.ghr.announced != tag {
			b.ghr.announced, announce = tag, true
		}
	}
	b.ghrMu.Unlock()
	b.Jobs.Finished(config.JobGhrUpdateCheck, now, err, nil)
	if announce {
		b.Events.Add("info", "", "ghr %s is available (running %s)", tag, b.Version)
	}
}

// newerThan reports whether tag is a newer release than running; never for
// a development build, whose version is not a release tag.
func newerThan(tag, running string) bool {
	t, ok := selfupdate.ParseVersion(tag)
	r, release := selfupdate.ParseVersion(running)
	return ok && release && r.Less(t)
}

// ghrStatus is the status's ghr_update block.
func (b *Backend) ghrStatus() model.GhrUpdate {
	_, release := selfupdate.ParseVersion(b.Version)
	b.ghrMu.Lock()
	defer b.ghrMu.Unlock()
	g := model.GhrUpdate{
		Installed: b.Version, Development: !release, Newer: newerThan(b.ghr.latest, b.Version),
		Latest: b.ghr.latest, CheckError: b.ghr.checkErr,
	}
	if !b.ghr.checkedAt.IsZero() {
		at := b.ghr.checkedAt
		g.CheckedAt = &at
	}
	return g
}
