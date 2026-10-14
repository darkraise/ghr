package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/darkraise/ghr/internal/api"
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
	inst      ghrInstall
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
	in := b.ghr.inst
	g.Installing, g.Step, g.RestartPending, g.LastOutcome, g.LastError = in.running, in.step, in.restartPending, in.lastOutcome, in.lastError
	return g
}

// How long a ghr install stages, and how often it looks again for the work
// it waits for; tests shorten the poll.
var (
	ghrStageTimeout = 10 * time.Minute
	ghrWaitPoll     = 15 * time.Second
)

// ghrInstall is the state of the ghr install, running or last run.
type ghrInstall struct {
	running bool
	tag     string
	step    string
	// swapped is set once the files are swapped; the install can no longer
	// be cancelled.
	swapped        bool
	cancel         context.CancelFunc
	restartPending string
	lastOutcome    string
	lastError      string
}

// ghrRecord is the file an install leaves for the next start.
type ghrRecord struct {
	Tag string `json:"tag"`
}

// StartGhrUpdate installs the newest ghr release in the background: it
// stages it, waits until no work a restart would cut runs, swaps it in and
// restarts ghr.service into it.
func (b *Backend) StartGhrUpdate() error {
	if b.Installer == nil || !b.RestartAvailable {
		return api.Conflict("ghr is not running as ghr.service; upgrade with setup.sh")
	}
	_, release := selfupdate.ParseVersion(b.Version)
	b.ghrMu.Lock()
	g := &b.ghr
	var refusal string
	switch {
	case g.inst.running:
		refusal = "a ghr update is already running"
	case g.inst.restartPending != "":
		// A second swap would copy the new binary over ghr.previous.
		refusal = "ghr " + g.inst.restartPending + " is installed; restart ghr to run it"
	case !release:
		refusal = "this ghr is a development build; install a release with setup.sh"
	case g.latest == "":
		refusal = "no ghr release check has succeeded yet"
	case !newerThan(g.latest, b.Version):
		refusal = "ghr " + b.Version + " is the newest release"
	}
	if refusal != "" {
		b.ghrMu.Unlock()
		return api.Conflict(refusal)
	}
	tag := g.latest
	ctx, cancel := context.WithCancel(b.background())
	g.inst = ghrInstall{running: true, tag: tag, cancel: cancel}
	b.bg.Add(1)
	b.ghrMu.Unlock()
	b.Events.Add("info", "", "installing ghr %s", tag)
	go func() {
		defer b.bg.Done()
		b.installGhr(ctx, tag)
	}()
	return nil
}

// CancelGhrUpdate stops a ghr install before its swap.
func (b *Backend) CancelGhrUpdate() error {
	b.ghrMu.Lock()
	defer b.ghrMu.Unlock()
	in := &b.ghr.inst
	switch {
	case !in.running:
		return api.Conflict("no ghr update is running")
	case in.swapped || in.step == "swap" || in.step == "restart":
		return api.Conflict("already installing; it cannot be cancelled now")
	}
	in.cancel()
	return nil
}

// installGhr runs the install of tag. Until the swap begins, ctx ending
// (a cancel or a shutdown) stops it; from the swap on it runs to its end.
func (b *Backend) installGhr(ctx context.Context, tag string) {
	stageCtx, cancel := context.WithTimeout(ctx, ghrStageTimeout)
	staged, err := b.Installer.Stage(stageCtx, tag, func(step string) { b.setGhrStep(tag, step) })
	cancel()
	if err != nil {
		b.failGhrInstall(ctx, err)
		return
	}
	b.setGhrStep(tag, "waiting")
	if err := b.waitForWork(ctx); err != nil {
		staged.Discard()
		b.failGhrInstall(ctx, err)
		return
	}
	// Checked under the lock CancelGhrUpdate holds, so a cancel lands
	// either before the swap or not at all.
	b.ghrMu.Lock()
	if err := ctx.Err(); err != nil {
		b.ghrMu.Unlock()
		staged.Discard()
		b.failGhrInstall(ctx, err)
		return
	}
	b.ghr.inst.step = "swap"
	b.ghrMu.Unlock()
	b.Events.Add("info", "", "ghr %s: swap", tag)
	err = b.Installer.Swap(staged)
	staged.Discard()
	if err != nil {
		b.endGhrInstall("failed", (&selfupdate.StepError{Step: "swap", Err: fmt.Errorf("%w; run the install again or upgrade with setup.sh", err)}).Error())
		return
	}
	prev := filepath.Join(b.Installer.Opt, "ghr.previous")
	b.Events.Add("info", "", "installed ghr %s at %s; to roll back: install -m 0755 %s %s && systemctl restart ghr", tag, b.Installer.Binary, prev, b.Installer.Binary)
	if err := b.writeGhrRecord(tag); err != nil {
		b.Events.Add("warn", "", "ghr update: cannot record the install for the next start: %v", err)
	}
	b.ghrMu.Lock()
	b.ghr.inst.swapped, b.ghr.inst.restartPending = true, tag
	b.ghrMu.Unlock()
	b.setGhrStep(tag, "restart")
	b.restartIntoGhr(ctx, tag)
}

// restartIntoGhr asks systemd to restart into the swapped binary. Work that
// started since the wait sends the install back to waiting. A shutdown ends
// it without a restart: the next start runs the new binary.
func (b *Backend) restartIntoGhr(ctx context.Context, tag string) {
	for {
		if ctx.Err() != nil {
			return
		}
		err := b.restartNow(context.WithoutCancel(ctx))
		var blocked restartBlocked
		switch {
		case errors.As(err, &blocked):
			b.setGhrStep(tag, "waiting")
			if b.waitForWork(ctx) != nil {
				return
			}
			b.setGhrStep(tag, "restart")
			continue
		case err != nil:
			b.endGhrInstall("failed", "restart: "+err.Error())
			return
		}
		b.Events.Add("info", "", "restarting into ghr %s", tag)
		// A restart this install found blocked earlier is not a failure now.
		b.setRestartFailure("")
		// The install is done; restart_pending stays until the new daemon runs.
		b.ghrMu.Lock()
		b.ghr.inst.running, b.ghr.inst.step = false, ""
		b.ghr.inst.cancel()
		b.ghrMu.Unlock()
		return
	}
}

// waitForWork returns once no work a restart would cut runs, or with ctx's
// error.
func (b *Backend) waitForWork(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(b.workBlocking(b.M.Status())) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
		case <-time.After(ghrWaitPoll):
		}
	}
}

func (b *Backend) setGhrStep(tag, step string) {
	b.ghrMu.Lock()
	b.ghr.inst.step = step
	b.ghrMu.Unlock()
	b.Events.Add("info", "", "ghr %s: %s", tag, step)
}

// failGhrInstall ends an install stopped before its swap: as cancelled when
// ctx ended, else as failed with err, which names the step.
func (b *Backend) failGhrInstall(ctx context.Context, err error) {
	if ctx.Err() != nil {
		b.endGhrInstall("cancelled", "")
		return
	}
	b.endGhrInstall("failed", err.Error())
}

func (b *Backend) endGhrInstall(outcome, why string) {
	b.ghrMu.Lock()
	in := &b.ghr.inst
	tag := in.tag
	in.cancel()
	in.running, in.step, in.lastOutcome, in.lastError = false, "", outcome, why
	b.ghrMu.Unlock()
	if outcome == "cancelled" {
		b.Events.Add("warn", "", "ghr %s update cancelled", tag)
		return
	}
	b.Events.Add("error", "", "ghr %s update failed: %s", tag, why)
}

// ghrInstalling reports an install in a step a restart would cut.
func (b *Backend) ghrInstalling() bool {
	b.ghrMu.Lock()
	defer b.ghrMu.Unlock()
	switch b.ghr.inst.step {
	case "download", "verify", "smoke", "swap":
		return b.ghr.inst.running
	}
	return false
}

func (b *Backend) writeGhrRecord(tag string) error {
	if b.UpdateRecordPath == "" {
		return nil
	}
	data, err := json.Marshal(ghrRecord{Tag: tag})
	if err != nil {
		return err
	}
	return os.WriteFile(b.UpdateRecordPath, data, 0o600)
}

// LoadGhrUpdateRecord reads and removes the record an install left before
// it restarted into a new ghr. When this daemon runs the recorded tag, the
// install succeeded; another version reports nothing.
func (b *Backend) LoadGhrUpdateRecord() {
	if b.UpdateRecordPath == "" {
		return
	}
	data, err := os.ReadFile(b.UpdateRecordPath)
	if err != nil {
		return
	}
	os.Remove(b.UpdateRecordPath)
	var r ghrRecord
	if json.Unmarshal(data, &r) != nil || r.Tag != b.Version {
		return
	}
	b.ghrMu.Lock()
	b.ghr.inst.lastOutcome = "ok"
	b.ghrMu.Unlock()
	b.Events.Add("info", "", "ghr updated to %s", r.Tag)
}
