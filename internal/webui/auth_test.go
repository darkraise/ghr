package webui

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const pw = "correct horse battery"

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newTestAuth(t *testing.T) (*Auth, *clock) {
	t.Helper()
	c := &clock{t: time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)}
	a := NewAuth(filepath.Join(t.TempDir(), "web-password"), c.now, rand.Reader)
	a.iter = 1000
	return a, c
}

func TestSetupOnce(t *testing.T) {
	a, _ := newTestAuth(t)
	if req, err := a.SetupRequired(); !req || err != nil {
		t.Fatalf("fresh: required %v err %v", req, err)
	}
	if _, err := a.Setup("short"); !errors.Is(err, ErrPasswordLength) {
		t.Fatalf("short: %v", err)
	}
	if _, err := a.Setup(strings.Repeat("x", MaxPasswordLen+1)); !errors.Is(err, ErrPasswordLength) {
		t.Fatalf("long: %v", err)
	}
	if _, err := a.Login("10.0.0.1", pw); !errors.Is(err, ErrSetupRequired) {
		t.Fatalf("login before setup: %v", err)
	}
	id, err := a.Setup(pw)
	if err != nil || !a.Valid(id) {
		t.Fatalf("setup: id valid %v err %v", a.Valid(id), err)
	}
	if req, _ := a.SetupRequired(); req {
		t.Fatal("setup still required after setup")
	}
	if _, err := a.Setup(pw); !errors.Is(err, ErrPasswordSet) {
		t.Fatalf("second setup: %v", err)
	}
}

func TestConcurrentSetupHasOneWinner(t *testing.T) {
	a, _ := newTestAuth(t)
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := a.Setup(pw)
			errs <- err
		}()
	}
	e1, e2 := <-errs, <-errs
	if (e1 == nil) == (e2 == nil) || !(errors.Is(e1, ErrPasswordSet) || errors.Is(e2, ErrPasswordSet)) {
		t.Fatalf("want one success and one ErrPasswordSet, got %v and %v", e1, e2)
	}
}

func TestLoginThrottle(t *testing.T) {
	a, c := newTestAuth(t)
	if _, err := a.Setup(pw); err != nil {
		t.Fatal(err)
	}
	for i := range maxFailures {
		if _, err := a.Login("10.0.0.1", "wrong password!"); !errors.Is(err, ErrWrongPassword) {
			t.Fatalf("failure %d: %v", i+1, err)
		}
	}
	var te *ThrottledError
	if _, err := a.Login("10.0.0.1", pw); !errors.As(err, &te) || !te.RetryAt.Equal(c.now().Add(lockout)) {
		t.Fatalf("sixth attempt: %v", err)
	}
	if _, err := a.Login("10.0.0.2", pw); err != nil {
		t.Fatalf("another client was throttled: %v", err)
	}
	c.add(lockout)
	if id, err := a.Login("10.0.0.1", pw); err != nil || !a.Valid(id) {
		t.Fatalf("after the lockout: %v", err)
	}
}

func TestFailuresUseARollingWindow(t *testing.T) {
	a, c := newTestAuth(t)
	if _, err := a.Setup(pw); err != nil {
		t.Fatal(err)
	}
	fail := func(n int) {
		for range n {
			if _, err := a.Login("10.0.0.1", "wrong password!"); !errors.Is(err, ErrWrongPassword) {
				t.Fatalf("at %v: %v", c.now(), err)
			}
		}
	}
	fail(1)
	c.add(14 * time.Minute)
	fail(3)
	c.add(2 * time.Minute)
	fail(2)
	var te *ThrottledError
	if _, err := a.Login("10.0.0.1", pw); !errors.As(err, &te) {
		t.Fatalf("five failures within the last 15 minutes did not lock the client out: %v", err)
	}
}

func TestBurstCannotExceedTheLimit(t *testing.T) {
	a, _ := newTestAuth(t)
	if _, err := a.Setup(pw); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 20)
	for range 20 {
		go func() {
			<-start
			_, err := a.Login("10.0.0.1", "wrong password!")
			results <- err
		}()
	}
	close(start)
	wrong, throttled := 0, 0
	for range 20 {
		var te *ThrottledError
		switch err := <-results; {
		case errors.Is(err, ErrWrongPassword):
			wrong++
		case errors.As(err, &te):
			throttled++
		default:
			t.Fatalf("unexpected %v", err)
		}
	}
	if wrong != maxFailures || throttled != 20-maxFailures {
		t.Fatalf("%d wrong-password answers and %d throttled, want %d and %d", wrong, throttled, maxFailures, 20-maxFailures)
	}
	var te *ThrottledError
	if _, err := a.Login("10.0.0.1", pw); !errors.As(err, &te) {
		t.Fatalf("the right password after the burst: %v", err)
	}
}

func TestSuccessClearsFailures(t *testing.T) {
	a, _ := newTestAuth(t)
	if _, err := a.Setup(pw); err != nil {
		t.Fatal(err)
	}
	for range maxFailures - 1 {
		a.Login("10.0.0.1", "wrong password!")
	}
	if _, err := a.Login("10.0.0.1", pw); err != nil {
		t.Fatal(err)
	}
	for range maxFailures - 1 {
		a.Login("10.0.0.1", "wrong password!")
	}
	if _, err := a.Login("10.0.0.1", pw); err != nil {
		t.Fatalf("failures before the success still counted: %v", err)
	}
}

func TestFailuresExpireAndAreSwept(t *testing.T) {
	a, c := newTestAuth(t)
	if _, err := a.Setup(pw); err != nil {
		t.Fatal(err)
	}
	for range maxFailures - 1 {
		a.Login("10.0.0.1", "wrong password!")
	}
	c.add(failWindow)
	a.Login("10.0.0.9", "wrong password!")
	a.mu.Lock()
	_, stale := a.fails["10.0.0.1"]
	a.mu.Unlock()
	if stale {
		t.Fatal("an expired failure counter was not swept")
	}
	for range maxFailures - 1 {
		a.Login("10.0.0.1", "wrong password!")
	}
	if _, err := a.Login("10.0.0.1", pw); err != nil {
		t.Fatalf("expired failures still counted: %v", err)
	}
}

func TestSessionsExpireAndEnd(t *testing.T) {
	a, c := newTestAuth(t)
	id1, err := a.Setup(pw)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := a.Login("10.0.0.1", pw)
	if err != nil {
		t.Fatal(err)
	}
	if id1 == id2 || len(id1) != 43 {
		t.Fatalf("session ids %q %q", id1, id2)
	}
	a.Logout(id1)
	if a.Valid(id1) || !a.Valid(id2) {
		t.Fatalf("logout: id1 %v id2 %v", a.Valid(id1), a.Valid(id2))
	}
	if a.Valid("") || a.Valid("made-up") {
		t.Fatal("an unknown id is valid")
	}
	c.add(sessionTTL)
	if a.Valid(id2) {
		t.Fatal("a session outlived 30 days")
	}
}

func TestFailedLoginSweepsExpiredSessions(t *testing.T) {
	a, c := newTestAuth(t)
	if _, err := a.Setup(pw); err != nil {
		t.Fatal(err)
	}
	c.add(sessionTTL)
	a.Login("10.0.0.1", "wrong password!")
	a.mu.Lock()
	n := len(a.sessions)
	a.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d expired sessions left after a failed login", n)
	}
}

func TestChangePassword(t *testing.T) {
	a, _ := newTestAuth(t)
	keep, err := a.Setup(pw)
	if err != nil {
		t.Fatal(err)
	}
	other, err := a.Login("10.0.0.1", pw)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ChangePassword(keep, "wrong password!", "another long secret"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("wrong current: %v", err)
	}
	if !a.Valid(keep) || !a.Valid(other) {
		t.Fatal("a refused change ended sessions")
	}
	if err := a.ChangePassword(keep, pw, "short"); !errors.Is(err, ErrPasswordLength) {
		t.Fatalf("short new: %v", err)
	}
	if err := a.ChangePassword(keep, pw, "another long secret"); err != nil {
		t.Fatal(err)
	}
	if !a.Valid(keep) || a.Valid(other) {
		t.Fatalf("after change: keep %v other %v", a.Valid(keep), a.Valid(other))
	}
	if _, err := a.Login("10.0.0.2", pw); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("old password: %v", err)
	}
	if _, err := a.Login("10.0.0.2", "another long secret"); err != nil {
		t.Fatalf("new password: %v", err)
	}
}

func TestResetEndsEverything(t *testing.T) {
	a, _ := newTestAuth(t)
	id, err := a.Setup(pw)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Reset(); err != nil {
		t.Fatal(err)
	}
	if a.Valid(id) {
		t.Fatal("a session survived the reset")
	}
	if req, err := a.SetupRequired(); !req || err != nil {
		t.Fatalf("after reset: required %v err %v", req, err)
	}
	if err := a.Reset(); err != nil {
		t.Fatalf("reset without a password: %v", err)
	}
}

// fillSlots takes every derivation slot, so the next login stops just before
// PBKDF2, after it has read the password file.
func fillSlots(a *Auth) {
	for range maxDerivations {
		a.sem <- struct{}{}
	}
}

func TestLoginRacingAResetCreatesNoSession(t *testing.T) {
	a, _ := newTestAuth(t)
	if _, err := a.Setup(pw); err != nil {
		t.Fatal(err)
	}
	fillSlots(a)
	type result struct {
		id  string
		err error
	}
	done := make(chan result, 1)
	go func() {
		id, err := a.Login("10.0.0.1", pw)
		done <- result{id, err}
	}()
	time.Sleep(50 * time.Millisecond)
	if err := a.Reset(); err != nil {
		t.Fatal(err)
	}
	<-a.sem
	r := <-done
	if !errors.Is(r.err, ErrSetupRequired) || a.Valid(r.id) {
		t.Fatalf("a login that read the old password outlived the reset: id valid %v err %v", a.Valid(r.id), r.err)
	}
	<-a.sem
}

// Waiting senders on a channel are served first come, first served, so the
// change, queued first, takes the freed slot and publishes before releasing
// it to the login.
func TestLoginRacingAPasswordChangeCreatesNoSession(t *testing.T) {
	a, _ := newTestAuth(t)
	keep, err := a.Setup(pw)
	if err != nil {
		t.Fatal(err)
	}
	fillSlots(a)
	changed := make(chan error, 1)
	go func() { changed <- a.ChangePassword(keep, pw, "another long secret") }()
	time.Sleep(50 * time.Millisecond)
	type result struct {
		id  string
		err error
	}
	done := make(chan result, 1)
	go func() {
		id, err := a.Login("10.0.0.1", pw)
		done <- result{id, err}
	}()
	time.Sleep(50 * time.Millisecond)
	<-a.sem
	if err := <-changed; err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.err == nil || a.Valid(r.id) {
		t.Fatalf("a login that read the old password outlived the change: id valid %v err %v", a.Valid(r.id), r.err)
	}
	if !a.Valid(keep) {
		t.Fatal("the changing session was ended")
	}
	<-a.sem
}

func TestUnreadableFile(t *testing.T) {
	a, _ := newTestAuth(t)
	if err := a.Check(); err != nil {
		t.Fatalf("missing file: %v", err)
	}
	os.WriteFile(a.path, []byte("garbage\n"), 0o600)
	if err := a.Check(); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("check: %v", err)
	}
	if _, err := a.SetupRequired(); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("setup required: %v", err)
	}
	if _, err := a.Login("10.0.0.1", pw); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("login: %v", err)
	}
	if _, err := a.Setup(pw); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("setup: %v", err)
	}
}

func TestDerivationsAreBounded(t *testing.T) {
	a, _ := newTestAuth(t)
	if _, err := a.Setup(pw); err != nil {
		t.Fatal(err)
	}
	fillSlots(a)
	done := make(chan error, 1)
	go func() {
		_, err := a.Login("10.0.0.1", pw)
		done <- err
	}()
	select {
	case <-done:
		t.Fatal("a login derived a key while every slot was taken")
	case <-time.After(50 * time.Millisecond):
	}
	<-a.sem
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	<-a.sem
}

func TestLoginStopsWaitingWhenTheClientLeaves(t *testing.T) {
	a, _ := newTestAuth(t)
	if _, err := a.Setup(pw); err != nil {
		t.Fatal(err)
	}
	fillSlots(a)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := a.login(ctx, "10.0.0.1", "wrong password!")
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("login: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a login kept waiting for a slot after its client left")
	}
	if len(a.sem) != maxDerivations || len(a.fails) != 0 {
		t.Fatalf("slots taken %d, failures %v", len(a.sem), a.fails)
	}
	<-a.sem
	<-a.sem
}

func TestClientKey(t *testing.T) {
	for in, want := range map[string]string{
		"192.168.0.10:5555":           "192.168.0.10",
		"[2001:db8:1:2:aaaa::1]:5555": "2001:db8:1:2::/64",
		"[2001:db8:1:2:bbbb::9]:6000": "2001:db8:1:2::/64",
		"[::ffff:10.0.0.1]:80":        "10.0.0.1",
		"[fe80::1%eth0]:80":           "fe80::/64",
		"not an address":              "not an address",
	} {
		if got := ClientKey(in); got != want {
			t.Errorf("ClientKey(%q) = %q, want %q", in, got, want)
		}
	}
}
