package webui

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
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

// claim sets the first password with the current setup code.
func claim(a *Auth, password string) (string, error) {
	code, err := a.SetupCode()
	if err != nil {
		return "", err
	}
	return a.Setup(code, password)
}

func TestSetupOnce(t *testing.T) {
	a, _ := newTestAuth(t)
	if req, err := a.SetupRequired(); !req || err != nil {
		t.Fatalf("fresh: required %v err %v", req, err)
	}
	code, err := a.SetupCode()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Setup(code, "short"); !errors.Is(err, ErrPasswordLength) {
		t.Fatalf("short: %v", err)
	}
	if _, err := a.Setup(code, strings.Repeat("x", MaxPasswordLen+1)); !errors.Is(err, ErrPasswordLength) {
		t.Fatalf("long: %v", err)
	}
	if _, err := a.Login("10.0.0.1", pw); !errors.Is(err, ErrSetupRequired) {
		t.Fatalf("login before setup: %v", err)
	}
	id, err := a.Setup(code, pw)
	if err != nil || !a.Valid(id) {
		t.Fatalf("setup: id valid %v err %v", a.Valid(id), err)
	}
	if req, _ := a.SetupRequired(); req {
		t.Fatal("setup still required after setup")
	}
	if _, err := a.Setup(code, pw); !errors.Is(err, ErrPasswordSet) {
		t.Fatalf("second setup: %v", err)
	}
}

func TestConcurrentSetupHasOneWinner(t *testing.T) {
	a, _ := newTestAuth(t)
	code, err := a.SetupCode()
	if err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := a.Setup(code, pw)
			errs <- err
		}()
	}
	e1, e2 := <-errs, <-errs
	if (e1 == nil) == (e2 == nil) || !(errors.Is(e1, ErrPasswordSet) || errors.Is(e2, ErrPasswordSet)) {
		t.Fatalf("want one success and one ErrPasswordSet, got %v and %v", e1, e2)
	}
}

var setupCodeFormat = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{5}(-[0-9A-HJKMNP-TV-Z]{5}){3}$`)

func TestSetupNeedsTheCode(t *testing.T) {
	a, _ := newTestAuth(t)
	if _, err := a.Setup("", pw); !errors.Is(err, ErrSetupCode) {
		t.Fatalf("before any code was issued: %v", err)
	}
	old, err := a.SetupCode()
	if err != nil || !setupCodeFormat.MatchString(old) {
		t.Fatalf("code %q err %v", old, err)
	}
	if again, _ := a.SetupCode(); again != old {
		t.Fatalf("the code changed between calls: %q then %q", old, again)
	}
	if err := a.Reset(); err != nil {
		t.Fatal(err)
	}
	code, err := a.SetupCode()
	if err != nil || code == old {
		t.Fatalf("after reset: code %q (was %q) err %v", code, old, err)
	}
	for _, c := range []string{"", "WRONG-CODE0-WRONG-CODE0", old, code[:len(code)-1]} {
		if _, err := a.Setup(c, pw); !errors.Is(err, ErrSetupCode) || !strings.Contains(err.Error(), "ghr code") {
			t.Errorf("code %q: %v", c, err)
		}
	}
	if _, err := os.Stat(a.path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a refused setup wrote the password file: %v", err)
	}
	if _, err := a.Setup(strings.ToLower(code), pw); err != nil {
		t.Fatalf("lower case: %v", err)
	}
	os.Remove(a.path)
	if _, err := a.Setup(code, pw); !errors.Is(err, ErrSetupCode) {
		t.Fatalf("a used code worked again: %v", err)
	}

	for _, form := range []func(string) string{
		func(c string) string { return strings.ReplaceAll(c, "-", "") },
		func(c string) string { return " " + strings.ReplaceAll(c, "-", " ") + " " },
	} {
		a, _ := newTestAuth(t)
		code, err := a.SetupCode()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.Setup(form(code), pw); err != nil {
			t.Errorf("code %q: %v", form(code), err)
		}
	}
}

func TestSetupCodeIsRefusedOnceAPasswordIsSet(t *testing.T) {
	a, _ := newTestAuth(t)
	if _, err := claim(a, pw); err != nil {
		t.Fatal(err)
	}
	if code, err := a.SetupCode(); !errors.Is(err, ErrPasswordSet) || code != "" {
		t.Fatalf("code %q err %v", code, err)
	}
}

func TestSetEndsTheSetupCode(t *testing.T) {
	a, _ := newTestAuth(t)
	code, err := a.SetupCode()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Set(pw); err != nil {
		t.Fatal(err)
	}
	// A password file removed by hand, not by Reset, must not revive the code.
	os.Remove(a.path)
	if _, err := a.Setup(code, pw); !errors.Is(err, ErrSetupCode) {
		t.Fatalf("setup with the code from before set-password: %v", err)
	}
}

func TestLoginThrottle(t *testing.T) {
	a, c := newTestAuth(t)
	if _, err := claim(a, pw); err != nil {
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

// These figures decide how many passwords a client may try, so a change to
// any of them must be a deliberate one.
func TestThrottleFigures(t *testing.T) {
	if maxFailures != 5 || maxPrefixFailures != 50 || failWindow != 15*time.Minute || lockout != 15*time.Minute {
		t.Fatalf("limits %d per address and %d per /64, window %v, lockout %v", maxFailures, maxPrefixFailures, failWindow, lockout)
	}
}

func TestFailuresUseARollingWindow(t *testing.T) {
	for _, tc := range []struct {
		name  string
		key   func(i int) string
		limit int
	}{
		{"IPv4 address", func(int) string { return "10.0.0.1" }, maxFailures},
		{"IPv6 address", func(int) string { return v6(1) }, maxFailures},
		{"IPv6 /64", func(i int) string { return v6(i + 1) }, maxPrefixFailures},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, c := newTestAuth(t)
			if _, err := claim(a, pw); err != nil {
				t.Fatal(err)
			}
			done := 0
			fail := func(n int) {
				t.Helper()
				for range n {
					if _, err := a.Login(tc.key(done), "wrong password!"); !errors.Is(err, ErrWrongPassword) {
						t.Fatalf("failure %d at %v: %v", done+1, c.now(), err)
					}
					done++
				}
			}
			fail(1)
			c.add(14 * time.Minute)
			fail(tc.limit - 2)
			c.add(2 * time.Minute)
			// The first failure has left the window, so the lock lands on the
			// second of these two and not on the first.
			fail(2)
			var te *ThrottledError
			if _, err := a.Login(tc.key(0), pw); !errors.As(err, &te) {
				t.Fatalf("%d failures within the last 15 minutes did not lock the client out: %v", tc.limit, err)
			}
		})
	}
}

func TestBurstCannotExceedTheLimit(t *testing.T) {
	for _, tc := range []struct {
		name  string
		n     int
		key   func(i int) string
		wrong int
	}{
		{"one address", 20, func(int) string { return "10.0.0.1" }, maxFailures},
		{"one /64", 60, func(i int) string { return v6(i + 1) }, maxPrefixFailures},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := newTestAuth(t)
			if _, err := claim(a, pw); err != nil {
				t.Fatal(err)
			}
			start := make(chan struct{})
			results := make(chan error, tc.n)
			for i := range tc.n {
				go func() {
					<-start
					_, err := a.Login(tc.key(i), "wrong password!")
					results <- err
				}()
			}
			close(start)
			wrong, throttled := 0, 0
			for range tc.n {
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
			if wrong != tc.wrong || throttled != tc.n-tc.wrong {
				t.Fatalf("%d wrong-password answers and %d throttled, want %d and %d", wrong, throttled, tc.wrong, tc.n-tc.wrong)
			}
			var te *ThrottledError
			if _, err := a.Login(tc.key(0), pw); !errors.As(err, &te) {
				t.Fatalf("the right password after the burst: %v", err)
			}
		})
	}
}

func TestSuccessClearsFailures(t *testing.T) {
	a, _ := newTestAuth(t)
	if _, err := claim(a, pw); err != nil {
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

// v6 is the throttle key of host n in the /64 v6Prefix, as the handler
// derives it.
func v6(n int) string { return ClientKey(fmt.Sprintf("[2001:db8:1:2::%x]:5555", n)) }

const v6Prefix = "2001:db8:1:2::/64"

func failureCounters(a *Auth) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.fails)
}

// A dual-stack listener reports an IPv4 client as ::ffff:a.b.c.d. That is
// the client a.b.c.d: one counter, locked at maxFailures.
func TestIPv4MappedAddressIsItsIPv4Client(t *testing.T) {
	a, _ := newTestAuth(t)
	if _, err := claim(a, pw); err != nil {
		t.Fatal(err)
	}
	keys := []string{ClientKey("10.0.0.1:5555"), ClientKey("[::ffff:10.0.0.1]:5555")}
	for i := range maxFailures {
		if _, err := a.Login(keys[i%2], "wrong password!"); !errors.Is(err, ErrWrongPassword) {
			t.Fatalf("failure %d: %v", i+1, err)
		}
	}
	for _, key := range keys {
		var te *ThrottledError
		if _, err := a.Login(key, pw); !errors.As(err, &te) {
			t.Fatalf("%s after %d failures: %v", key, maxFailures, err)
		}
	}
	if n := failureCounters(a); n != 1 {
		t.Fatalf("%d failure counters, want 1", n)
	}
}

func TestOnlyAnIPv6AddressHasAPrefixCounter(t *testing.T) {
	for _, tc := range []struct {
		key  string
		want int
	}{
		{"10.0.0.1", 1},
		{"::ffff:10.0.0.1", 1},
		{"not an address", 1},
		{v6(1), 2},
	} {
		a, _ := newTestAuth(t)
		if _, err := claim(a, pw); err != nil {
			t.Fatal(err)
		}
		a.Login(tc.key, "wrong password!")
		if n := failureCounters(a); n != tc.want {
			t.Errorf("%s: %d failure counters after one failure, want %d", tc.key, n, tc.want)
		}
	}
}

// With every derivation slot taken and its context already done, a login
// that gets as far as the password check returns the context's error. A
// locked client must be refused before that: no derivation runs for it and
// no failure is counted, whatever password it sends, until the lockout ends.
func TestLockedClientIsRefusedBeforeThePasswordCheck(t *testing.T) {
	for _, tc := range []struct {
		name     string
		failures int
		key      func(i int) string
		refused  string
		admitted string
	}{
		{"IPv4 address", maxFailures, func(int) string { return "10.0.0.1" }, "10.0.0.1", "10.0.0.2"},
		{"IPv6 address", maxFailures, func(int) string { return v6(1) }, v6(1), v6(2)},
		{"IPv6 /64, an address never seen", maxPrefixFailures, func(i int) string { return v6(i + 1) }, v6(1000), ClientKey("[2001:db8:1:3::1]:5555")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, c := newTestAuth(t)
			if _, err := claim(a, pw); err != nil {
				t.Fatal(err)
			}
			for i := range tc.failures {
				if _, err := a.Login(tc.key(i), "wrong password!"); !errors.Is(err, ErrWrongPassword) {
					t.Fatalf("failure %d: %v", i+1, err)
				}
			}
			until := c.now().Add(lockout)
			counters := failureCounters(a)
			fillSlots(a)
			defer func() { <-a.sem; <-a.sem }()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			refused := func(when string) {
				t.Helper()
				for _, password := range []string{pw, "wrong password!"} {
					var te *ThrottledError
					if _, err := a.login(ctx, tc.refused, password); !errors.As(err, &te) || !te.RetryAt.Equal(until) {
						t.Fatalf("%s %s: %v", tc.refused, when, err)
					}
				}
			}
			refused("at once")
			if _, err := a.login(ctx, tc.admitted, pw); !errors.Is(err, context.Canceled) {
				t.Fatalf("%s, which is not locked out: %v", tc.admitted, err)
			}
			if n := failureCounters(a); n != counters {
				t.Fatalf("%d failure counters after the refusals, want %d", n, counters)
			}
			c.add(lockout - time.Nanosecond)
			refused("just before the lockout ends")
			c.add(time.Nanosecond)
			if _, err := a.login(ctx, tc.refused, pw); !errors.Is(err, context.Canceled) {
				t.Fatalf("%s after the lockout: %v", tc.refused, err)
			}
			if n := failureCounters(a); n != 0 {
				t.Fatalf("%d failure counters left after the lockout", n)
			}
		})
	}
}

// login checks the lockout three times: on arrival, after waiting for a
// derivation slot, and after the derivation. A lock that lands before any of
// them must refuse the login and stay in place, whichever key it is on and
// even when the password is right. login reads the clock under a.mu right
// before each check, which is where this test plants the lock.
func TestALockLandingMidLoginRefusesIt(t *testing.T) {
	for _, tc := range []struct{ name, locked string }{{"address", v6(1)}, {"prefix", v6Prefix}} {
		for check := 1; check <= 3; check++ {
			t.Run(fmt.Sprintf("%s before check %d", tc.name, check), func(t *testing.T) {
				a, c := newTestAuth(t)
				if _, err := claim(a, pw); err != nil {
					t.Fatal(err)
				}
				until := c.now().Add(lockout)
				reads := 0
				a.now = func() time.Time {
					if reads++; reads == check {
						a.fails[tc.locked] = &failures{times: []time.Time{c.now()}, lockedUntil: until}
					}
					return c.now()
				}
				var te *ThrottledError
				if id, err := a.Login(v6(1), pw); !errors.As(err, &te) || !te.RetryAt.Equal(until) || id != "" {
					t.Fatalf("login: id %q err %v", id, err)
				}
				if reads != check {
					t.Fatalf("login went on to clock read %d after the lock landed before check %d", reads, check)
				}
				if f := a.fails[tc.locked]; f == nil || !f.lockedUntil.Equal(until) || len(a.sessions) != 1 {
					t.Fatalf("the refused login changed the lock or started a session: %+v, %d sessions", f, len(a.sessions))
				}
			})
		}
	}
}

func TestRefusalNamesTheLaterRetryTime(t *testing.T) {
	a, c := newTestAuth(t)
	now := c.now()
	for _, tc := range []struct {
		name            string
		address, prefix time.Duration // how long after now each lock ends; 0 is no counter
		want            time.Duration // 0 is admitted
	}{
		{"no counter", 0, 0, 0},
		{"address locked", 3 * time.Minute, 0, 3 * time.Minute},
		{"/64 locked", 0, 4 * time.Minute, 4 * time.Minute},
		{"both locked, /64 for longer", 3 * time.Minute, 4 * time.Minute, 4 * time.Minute},
		{"both locked, address for longer", 4 * time.Minute, 3 * time.Minute, 4 * time.Minute},
		{"address lock ended, /64 locked", -time.Minute, 4 * time.Minute, 4 * time.Minute},
		{"/64 lock ended, address locked", 3 * time.Minute, -time.Minute, 3 * time.Minute},
		{"both locks ended", -time.Minute, -time.Second, 0},
	} {
		a.mu.Lock()
		clear(a.fails)
		for key, d := range map[string]time.Duration{v6(1): tc.address, v6Prefix: tc.prefix} {
			if d != 0 {
				a.fails[key] = &failures{times: []time.Time{now}, lockedUntil: now.Add(d)}
			}
		}
		err := a.admitLocked(v6(1), now)
		a.mu.Unlock()
		var te *ThrottledError
		switch {
		case tc.want == 0 && err != nil:
			t.Errorf("%s: refused: %v", tc.name, err)
		case tc.want != 0 && (!errors.As(err, &te) || !te.RetryAt.Equal(now.Add(tc.want))):
			t.Errorf("%s: %v, want a retry at %v", tc.name, err, now.Add(tc.want))
		}
	}
}

// On a LAN every IPv6 host shares one /64, so one host's wrong passwords
// must lock that host's address and nobody else's.
func TestIPv6AddressLocksAlone(t *testing.T) {
	a, c := newTestAuth(t)
	if _, err := claim(a, pw); err != nil {
		t.Fatal(err)
	}
	for i := range maxFailures {
		if _, err := a.Login(v6(1), "wrong password!"); !errors.Is(err, ErrWrongPassword) {
			t.Fatalf("failure %d: %v", i+1, err)
		}
	}
	var te *ThrottledError
	if _, err := a.Login(v6(1), pw); !errors.As(err, &te) || !te.RetryAt.Equal(c.now().Add(lockout)) {
		t.Fatalf("sixth attempt: %v", err)
	}
	if _, err := a.Login(v6(2), pw); err != nil {
		t.Fatalf("another address in the /64 was throttled: %v", err)
	}
	a.Login("10.0.0.1", "wrong password!")
	// The IPv6 address, its /64, and the IPv4 address, which has no second key.
	if n := failureCounters(a); n != 3 {
		t.Fatalf("%d failure counters, want 3", n)
	}
}

// A host that takes a new address inside its /64 for every attempt is held to
// maxPrefixFailures a window, and adds nothing to the table once the /64 is
// locked.
func TestIPv6PrefixLocksAtFifty(t *testing.T) {
	a, c := newTestAuth(t)
	if _, err := claim(a, pw); err != nil {
		t.Fatal(err)
	}
	fail := func(key string) {
		t.Helper()
		if _, err := a.Login(key, "wrong password!"); !errors.Is(err, ErrWrongPassword) {
			t.Fatalf("%s: %v", key, err)
		}
	}
	retryAt := func(key, password string) time.Time {
		t.Helper()
		var te *ThrottledError
		if _, err := a.Login(key, password); !errors.As(err, &te) {
			t.Fatalf("%s was not refused: %v", key, err)
		}
		return te.RetryAt
	}
	start := c.now()
	for range maxFailures {
		fail(v6(1))
	}
	c.add(5 * time.Minute)
	others := maxPrefixFailures - maxFailures
	for n := range others {
		fail(v6(n + 2))
	}
	// v6(1) is locked on its own until start+lockout; the /64's lock ends later.
	prefixUntil := start.Add(5*time.Minute + lockout)
	if got := retryAt(v6(1), pw); !got.Equal(prefixUntil) {
		t.Fatalf("an address locked before its /64: retry at %v, want %v", got, prefixUntil)
	}
	for n := range 10 {
		if got := retryAt(v6(1000+n), "wrong password!"); !got.Equal(prefixUntil) {
			t.Fatalf("a fresh address in a locked /64: retry at %v, want %v", got, prefixUntil)
		}
	}
	if n, want := failureCounters(a), others+2; n != want {
		t.Fatalf("%d failure counters while the /64 is locked, want %d", n, want)
	}
	if _, err := a.Login(ClientKey("[2001:db8:1:3::1]:5555"), pw); err != nil {
		t.Fatalf("another /64 was throttled: %v", err)
	}
	if _, err := a.Login("10.0.0.1", pw); err != nil {
		t.Fatalf("an IPv4 client was throttled: %v", err)
	}
	c.add(10 * time.Minute)
	if got := retryAt(v6(1), pw); !got.Equal(prefixUntil) {
		t.Fatalf("after the address's own lockout: retry at %v, want %v", got, prefixUntil)
	}
	c.add(5 * time.Minute)
	if _, err := a.Login(v6(1000), pw); err != nil {
		t.Fatalf("after the /64's lockout: %v", err)
	}
	if n := failureCounters(a); n != 0 {
		t.Fatalf("%d failure counters left after the lockout", n)
	}
}

// The owner logging in from one address must not hand a guesser beside it a
// fresh count for the /64.
func TestSuccessClearsTheAddressNotThePrefix(t *testing.T) {
	a, _ := newTestAuth(t)
	if _, err := claim(a, pw); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		for range maxFailures - 1 {
			a.Login(v6(1), "wrong password!")
		}
		if _, err := a.Login(v6(1), pw); err != nil {
			t.Fatalf("failures before a success still counted for the address: %v", err)
		}
	}
	a.mu.Lock()
	_, address := a.fails[v6(1)]
	counted := 0
	if f := a.fails[v6Prefix]; f != nil {
		counted = len(f.times)
	}
	a.mu.Unlock()
	if want := 2 * (maxFailures - 1); address || counted != want {
		t.Fatalf("after two successes: address counter kept %v, %d failures counted for the /64, want %d", address, counted, want)
	}
	for n := range maxPrefixFailures - 2*(maxFailures-1) {
		if _, err := a.Login(v6(n+2), "wrong password!"); !errors.Is(err, ErrWrongPassword) {
			t.Fatalf("address %d: %v", n+2, err)
		}
	}
	var te *ThrottledError
	if _, err := a.Login(v6(1), pw); !errors.As(err, &te) {
		t.Fatalf("the successes cleared the /64's failures: %v", err)
	}
}

func TestFailuresExpireAndAreSwept(t *testing.T) {
	a, c := newTestAuth(t)
	if _, err := claim(a, pw); err != nil {
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
	id1, err := claim(a, pw)
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
	if _, err := claim(a, pw); err != nil {
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
	keep, err := claim(a, pw)
	if err != nil {
		t.Fatal(err)
	}
	other, err := a.Login("10.0.0.1", pw)
	if err != nil {
		t.Fatal(err)
	}
	if id, err := a.ChangePassword("wrong password!", "another long secret"); !errors.Is(err, ErrWrongPassword) || id != "" {
		t.Fatalf("wrong current: id %q err %v", id, err)
	}
	if !a.Valid(keep) || !a.Valid(other) {
		t.Fatal("a refused change ended sessions")
	}
	if id, err := a.ChangePassword(pw, "short"); !errors.Is(err, ErrPasswordLength) || id != "" {
		t.Fatalf("short new: id %q err %v", id, err)
	}
	id, err := a.ChangePassword(pw, "another long secret")
	if err != nil {
		t.Fatal(err)
	}
	if a.Valid(keep) || a.Valid(other) || !a.Valid(id) || id == keep {
		t.Fatalf("after change: old %v other %v new %v", a.Valid(keep), a.Valid(other), a.Valid(id))
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
	id, err := claim(a, pw)
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
	if _, err := claim(a, pw); err != nil {
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
	if _, err := claim(a, pw); err != nil {
		t.Fatal(err)
	}
	fillSlots(a)
	type change struct {
		id  string
		err error
	}
	changed := make(chan change, 1)
	go func() {
		id, err := a.ChangePassword(pw, "another long secret")
		changed <- change{id, err}
	}()
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
	ch := <-changed
	if ch.err != nil {
		t.Fatal(ch.err)
	}
	r := <-done
	if r.err == nil || a.Valid(r.id) {
		t.Fatalf("a login that read the old password outlived the change: id valid %v err %v", a.Valid(r.id), r.err)
	}
	if !a.Valid(ch.id) {
		t.Fatal("the change's new session was ended")
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
	if _, err := a.SetupCode(); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("setup code: %v", err)
	}
	if _, err := a.Setup("", pw); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("setup: %v", err)
	}
}

func TestDerivationsAreBounded(t *testing.T) {
	a, _ := newTestAuth(t)
	if _, err := claim(a, pw); err != nil {
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
	if _, err := claim(a, pw); err != nil {
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
		"[2001:db8:1:2:aaaa::1]:5555": "2001:db8:1:2:aaaa::1",
		"[2001:db8:1:2:bbbb::9]:6000": "2001:db8:1:2:bbbb::9",
		"[::ffff:10.0.0.1]:80":        "10.0.0.1",
		"[fe80::1%eth0]:80":           "fe80::1",
		"not an address":              "not an address",
	} {
		if got := ClientKey(in); got != want {
			t.Errorf("ClientKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSetReplacesThePassword(t *testing.T) {
	a, _ := newTestAuth(t)
	// Lengths count characters: eleven two-byte characters are too short
	// although they make 22 bytes.
	for _, bad := range []string{strings.Repeat("\u00e9", MinPasswordLen-1), strings.Repeat("x", MaxPasswordLen+1)} {
		if err := a.Set(bad); !errors.Is(err, ErrPasswordLength) {
			t.Fatalf("%d characters: %v", utf8.RuneCountInString(bad), err)
		}
	}
	if _, err := os.Stat(a.path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a refused password wrote the file: %v", err)
	}
	if err := a.Set(pw); err != nil {
		t.Fatal(err)
	}
	if req, err := a.SetupRequired(); req || err != nil {
		t.Fatalf("after the first set: required %v err %v", req, err)
	}
	if err := a.Set("short"); !errors.Is(err, ErrPasswordLength) {
		t.Fatalf("short over an existing password: %v", err)
	}
	id, err := a.Login("10.0.0.1", pw)
	if err != nil {
		t.Fatal(err)
	}
	const next = "another long password"
	if err := a.Set(next); err != nil {
		t.Fatal(err)
	}
	if a.Valid(id) {
		t.Fatal("a session survived the new password")
	}
	if _, err := a.Login("10.0.0.2", pw); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("old password: %v", err)
	}
	if _, err := a.Login("10.0.0.3", next); err != nil {
		t.Fatalf("new password: %v", err)
	}
}

func TestSetRepairsAnUnreadableFile(t *testing.T) {
	a, _ := newTestAuth(t)
	os.WriteFile(a.path, []byte("garbage\n"), 0o600)
	if !strings.Contains(a.Check().Error(), "ghr web set-password") {
		t.Fatalf("the unreadable-file error should point at set-password: %v", a.Check())
	}
	if err := a.Set(pw); err != nil {
		t.Fatal(err)
	}
	if err := a.Check(); err != nil {
		t.Fatalf("after set: %v", err)
	}
	if _, err := a.Login("10.0.0.1", pw); err != nil {
		t.Fatal(err)
	}
}

// As with a password change, Set holds the derivation slot until the new
// gen is published, so a login queued behind it cannot keep a session.
func TestLoginRacingASetCreatesNoSession(t *testing.T) {
	a, _ := newTestAuth(t)
	if _, err := claim(a, pw); err != nil {
		t.Fatal(err)
	}
	fillSlots(a)
	set := make(chan error, 1)
	go func() { set <- a.Set("another long secret") }()
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
	if err := <-set; err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.err == nil || a.Valid(r.id) {
		t.Fatalf("a login that read the old password outlived the set: id valid %v err %v", a.Valid(r.id), r.err)
	}
	<-a.sem
}
