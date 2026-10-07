package webui

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/netip"
	"os"
	"sync"
	"time"
)

const (
	sessionTTL  = 30 * 24 * time.Hour
	maxFailures = 5
	failWindow  = 15 * time.Minute
	lockout     = 15 * time.Minute
	// maxDerivations bounds concurrent PBKDF2 runs: each costs about half a
	// second of CPU, and the daemon's tick loop shares the machine.
	maxDerivations = 2
)

var (
	ErrPasswordLength = errors.New("password must be 12 to 1024 bytes")
	ErrPasswordSet    = errors.New("a web password is already set")
	ErrSetupRequired  = errors.New("no web password is set yet; set one first")
	ErrWrongPassword  = errors.New("wrong password")
)

// ThrottledError refuses a login from a client with too many recent failures.
type ThrottledError struct{ RetryAt time.Time }

func (e *ThrottledError) Error() string {
	return "too many failed logins; retry after " + e.RetryAt.UTC().Format(time.RFC3339)
}

type failures struct {
	times       []time.Time // failures within the last failWindow, oldest first
	lockedUntil time.Time
}

// Auth owns the web password file, the login sessions and the login throttle.
// Sessions live in memory, so a daemon restart ends them.
type Auth struct {
	path string
	now  func() time.Time
	rand io.Reader
	iter int
	sem  chan struct{}

	fileMu sync.Mutex // serialises password file changes
	mu     sync.Mutex // guards the fields below
	// gen changes whenever the password changes or is reset; a login that
	// read the file under an older gen must not publish a session.
	gen      uint64
	sessions map[string]time.Time
	fails    map[string]*failures
}

func NewAuth(path string, now func() time.Time, rand io.Reader) *Auth {
	return &Auth{
		path: path, now: now, rand: rand, iter: DefaultIterations,
		sem:      make(chan struct{}, maxDerivations),
		sessions: map[string]time.Time{},
		fails:    map[string]*failures{},
	}
}

func (a *Auth) derive(fn func()) {
	a.sem <- struct{}{}
	defer func() { <-a.sem }()
	fn()
}

// Check reports a password file that exists but cannot be used.
func (a *Auth) Check() error {
	_, _, err := readHash(a.path)
	return err
}

// SetupRequired reports whether no password has been set yet.
func (a *Auth) SetupRequired() (bool, error) {
	_, ok, err := readHash(a.path)
	if err != nil {
		return false, err
	}
	return !ok, nil
}

func validLength(p string) bool { return len(p) >= MinPasswordLen && len(p) <= MaxPasswordLen }

// Setup sets the first password and starts a session for the caller.
func (a *Auth) Setup(password string) (string, error) {
	if !validLength(password) {
		return "", ErrPasswordLength
	}
	a.fileMu.Lock()
	defer a.fileMu.Unlock()
	a.mu.Lock()
	a.sweepLocked(a.now())
	a.mu.Unlock()
	_, ok, err := readHash(a.path)
	if err != nil {
		return "", err
	}
	if ok {
		return "", ErrPasswordSet
	}
	var h passwordHash
	a.derive(func() { h, err = newHash(password, a.rand, a.iter) })
	if err != nil {
		return "", err
	}
	if err := writeHash(a.path, h); err != nil {
		return "", err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.newSessionLocked(a.now())
}

// Login checks password for the client named by key (see ClientKey) and
// starts a session.
func (a *Auth) Login(key, password string) (string, error) {
	return a.login(context.Background(), key, password)
}

// login stops waiting for a derivation slot once ctx is done, so logins whose
// clients have gone do not keep the slots from the ones still waiting.
func (a *Auth) login(ctx context.Context, key, password string) (string, error) {
	a.mu.Lock()
	err := a.admitLocked(key, a.now())
	gen := a.gen
	a.mu.Unlock()
	if err != nil {
		return "", err
	}
	h, ok, err := readHash(a.path)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrSetupRequired
	}
	select {
	case a.sem <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-a.sem }()
	a.mu.Lock()
	err = a.admitLocked(key, a.now())
	a.mu.Unlock()
	if err != nil {
		return "", err
	}
	match := h.matches(password)
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	if err := a.admitLocked(key, now); err != nil {
		return "", err
	}
	if !match {
		f := a.fails[key]
		if f == nil {
			f = &failures{}
			a.fails[key] = f
		}
		f.times = append(f.times, now)
		if len(f.times) >= maxFailures {
			f.lockedUntil = now.Add(lockout)
		}
		return "", ErrWrongPassword
	}
	if a.gen != gen {
		if _, ok, _ := readHash(a.path); !ok {
			return "", ErrSetupRequired
		}
		return "", ErrWrongPassword
	}
	delete(a.fails, key)
	return a.newSessionLocked(now)
}

// admitLocked sweeps, then refuses a client that is locked out. a.mu is held.
func (a *Auth) admitLocked(key string, now time.Time) error {
	a.sweepLocked(now)
	if f := a.fails[key]; f != nil && now.Before(f.lockedUntil) {
		return &ThrottledError{RetryAt: f.lockedUntil}
	}
	return nil
}

// sweepLocked drops expired sessions, failures older than failWindow, and
// clients with neither recent failures nor a lockout. a.mu is held.
func (a *Auth) sweepLocked(now time.Time) {
	for k, exp := range a.sessions {
		if !now.Before(exp) {
			delete(a.sessions, k)
		}
	}
	for k, f := range a.fails {
		i := 0
		for i < len(f.times) && now.Sub(f.times[i]) >= failWindow {
			i++
		}
		f.times = f.times[i:]
		if len(f.times) == 0 && !now.Before(f.lockedUntil) {
			delete(a.fails, k)
		}
	}
}

// newSessionLocked starts a session expiring sessionTTL after now. a.mu is held.
func (a *Auth) newSessionLocked(now time.Time) (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(a.rand, b); err != nil {
		return "", err
	}
	id := base64.RawURLEncoding.EncodeToString(b)
	a.sessions[id] = now.Add(sessionTTL)
	return id, nil
}

// Valid reports whether id names a live session.
func (a *Auth) Valid(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	exp, ok := a.sessions[id]
	return ok && a.now().Before(exp)
}

func (a *Auth) Logout(id string) {
	a.mu.Lock()
	delete(a.sessions, id)
	a.mu.Unlock()
}

// ChangePassword replaces the password and ends every session except keep.
func (a *Auth) ChangePassword(keep, current, next string) error {
	if !validLength(next) {
		return ErrPasswordLength
	}
	a.fileMu.Lock()
	defer a.fileMu.Unlock()
	h, ok, err := readHash(a.path)
	if err != nil {
		return err
	}
	if !ok {
		return ErrSetupRequired
	}
	// The slot is held until the change is published, so a login queued
	// behind it sees the new gen.
	var match bool
	a.derive(func() {
		if match = h.matches(current); !match {
			return
		}
		var nh passwordHash
		if nh, err = newHash(next, a.rand, a.iter); err != nil {
			return
		}
		if err = writeHash(a.path, nh); err != nil {
			return
		}
		a.mu.Lock()
		a.gen++
		for k := range a.sessions {
			if k != keep {
				delete(a.sessions, k)
			}
		}
		a.mu.Unlock()
	})
	if !match {
		return ErrWrongPassword
	}
	return err
}

// Set replaces the password, or sets the first one, and ends every session.
// It never reads the old file, so it also replaces an unreadable one.
func (a *Auth) Set(password string) error {
	if !validLength(password) {
		return ErrPasswordLength
	}
	a.fileMu.Lock()
	defer a.fileMu.Unlock()
	var err error
	// The slot is held until the change is published, so a login queued
	// behind it sees the new gen.
	a.derive(func() {
		var h passwordHash
		if h, err = newHash(password, a.rand, a.iter); err != nil {
			return
		}
		if err = writeHash(a.path, h); err != nil {
			return
		}
		a.mu.Lock()
		a.gen++
		clear(a.sessions)
		a.mu.Unlock()
	})
	return err
}

// Reset forgets the password and ends every session; the next visitor sets a
// new password.
func (a *Auth) Reset() error {
	a.fileMu.Lock()
	defer a.fileMu.Unlock()
	if err := os.Remove(a.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	a.mu.Lock()
	a.gen++
	clear(a.sessions)
	a.mu.Unlock()
	return nil
}

// ClientKey is the throttle key for a request's remote address: the IPv4
// address, or the IPv6 /64, since one IPv6 host can use a whole /64.
func ClientKey(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	ip = ip.Unmap().WithZone("")
	if ip.Is6() {
		p, _ := ip.Prefix(64)
		return p.String()
	}
	return ip.String()
}
