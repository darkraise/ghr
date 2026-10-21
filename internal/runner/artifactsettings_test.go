package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/darkraise/ghr/internal/sched"
)

const testSettings = `{"v":1,"endpoint":"https://s3.example.test","bucket":"ci","region":"us-east-1","key_id":"GHRKEY1","secret":"s3cr3t-job-key"}`

// fakeSettings stands in for the daemon's settings function.
type fakeSettings struct {
	b     []byte
	ok    bool
	calls int
}

func (f *fakeSettings) get(string) ([]byte, bool) {
	f.calls++
	return f.b, f.ok
}

func TestArtifactSettingsForAsksOnlyForAPrivateRepository(t *testing.T) {
	for _, tc := range []struct {
		name      string
		set       bool // the function is set
		recorded  bool // visibility has an answer
		public    bool
		wantCalls int
		wantOK    bool
	}{
		{name: "no function", recorded: true},
		{name: "visibility unread", set: true},
		{name: "public", set: true, recorded: true, public: true},
		{name: "private", set: true, recorded: true, wantCalls: 1, wantOK: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			f := &fakeSettings{b: []byte(testSettings), ok: true}
			if tc.set {
				h.m.ArtifactSettings = f.get
			}
			if tc.recorded {
				h.m.RecordVisibility("darkmem", tc.public)
			}
			b, ok := h.m.artifactSettingsFor("darkmem")
			if ok != tc.wantOK || f.calls != tc.wantCalls {
				t.Fatalf("ok %v after %d calls, want %v after %d", ok, f.calls, tc.wantOK, tc.wantCalls)
			}
			if ok && string(b) != testSettings {
				t.Fatalf("settings %q", b)
			}
		})
	}
}

// checkSettingsFile fails unless dir holds the settings file with want and
// the modes it is written with, and no temporary file.
func checkSettingsFile(t *testing.T, dir, want string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(dir, ArtifactSettingsFile))
	if err != nil || string(got) != want {
		t.Fatalf("settings file %q, %v; want %q", got, err, want)
	}
	if _, err := os.Lstat(filepath.Join(dir, artifactSettingsTemp)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("temporary file left: %v", err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	for name, mode := range map[string]fs.FileMode{"_work": 0o755, "_work/_ghr": 0o700, ArtifactSettingsFile: 0o600} {
		fi, err := os.Lstat(filepath.Join(dir, name))
		if err != nil || fi.Mode().Perm() != mode {
			t.Fatalf("%s: %v, %v; want mode %v", name, fi.Mode(), err, mode)
		}
	}
}

func TestWriteArtifactSettingsWritesTheFileWithItsModes(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	if err := h.m.writeArtifactSettings(dir, []byte(testSettings)); err != nil {
		t.Fatal(err)
	}
	checkSettingsFile(t, dir, testSettings)
}

// A directory left with another mode gets the right one, a leftover temporary
// file is replaced, and the settings file is replaced whole.
func TestWriteArtifactSettingsReplacesWhatIsThere(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "_work", "_ghr"), 0o777); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{artifactSettingsTemp, ArtifactSettingsFile} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("old and longer than the new settings file is going to be"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.m.writeArtifactSettings(dir, []byte(testSettings)); err != nil {
		t.Fatal(err)
	}
	checkSettingsFile(t, dir, testSettings)
}

// failingFile is the real temporary file, closed when Write or Close fails.
type failingFile struct {
	f                  *os.File
	writeErr, closeErr error
}

func (w *failingFile) Write(b []byte) (int, error) {
	if w.writeErr != nil {
		w.f.Close()
		return 0, w.writeErr
	}
	return w.f.Write(b)
}

func (w *failingFile) Close() error {
	err := w.f.Close()
	if w.closeErr != nil {
		return w.closeErr
	}
	return err
}

// failSettingsAt makes the temporary settings file fail at "create", "write"
// or "close". The file is created on disk in each case, so the test sees
// whether the failure removes it.
func failSettingsAt(at string) func(root *os.Root, name string) (io.WriteCloser, error) {
	return func(root *os.Root, name string) (io.WriteCloser, error) {
		f, err := openSettingsTemp(root, name)
		if err != nil {
			return nil, err
		}
		w := &failingFile{f: f.(*os.File)}
		switch at {
		case "create":
			w.f.Close()
			return nil, errors.New("disk on fire at create")
		case "write":
			w.writeErr = errors.New("disk on fire at write")
		case "close":
			w.closeErr = errors.New("disk on fire at close")
		}
		return w, nil
	}
}

func TestWriteArtifactSettingsFailureLeavesTheFileAsItWas(t *testing.T) {
	for _, at := range []string{"create", "write", "close"} {
		for _, before := range []string{"", `{"v":1,"secret":"old"}`} {
			t.Run(fmt.Sprintf("%s/replace=%v", at, before != ""), func(t *testing.T) {
				h := newHarness(t)
				h.m.openSettingsTemp = failSettingsAt(at)
				dir := t.TempDir()
				if before != "" {
					if err := os.MkdirAll(filepath.Join(dir, "_work", "_ghr"), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, ArtifactSettingsFile), []byte(before), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				err := h.m.writeArtifactSettings(dir, []byte(testSettings))
				if err == nil || err.Error() != "disk on fire at "+at {
					t.Fatalf("error %v", err)
				}
				if _, err := os.Lstat(filepath.Join(dir, artifactSettingsTemp)); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("temporary file left: %v", err)
				}
				got, err := os.ReadFile(filepath.Join(dir, ArtifactSettingsFile))
				if before == "" && !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("settings file %q, %v; want none", got, err)
				}
				if before != "" && string(got) != before {
					t.Fatalf("settings file %q, %v; want %q", got, err, before)
				}
			})
		}
	}
}

// A job can leave _work as a link to anywhere; the write must not follow it.
func TestWriteArtifactSettingsFollowsNoLinkOutOfTheInstance(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating a symlink needs a privilege on Windows")
	}
	h := newHarness(t)
	dir, elsewhere := t.TempDir(), t.TempDir()
	if err := os.Symlink(elsewhere, filepath.Join(dir, "_work")); err != nil {
		t.Fatal(err)
	}
	if err := h.m.writeArtifactSettings(dir, []byte(testSettings)); err == nil {
		t.Fatal("wrote through a link out of the instance")
	}
	if ents, err := os.ReadDir(elsewhere); err != nil || len(ents) != 0 {
		t.Fatalf("outside the instance: %v, %v", ents, err)
	}
}

// The settings file is in place when the spawn hands the instance directory
// to the runner user, so the chown gives it to that user with the rest.
func TestSpawnWritesTheArtifactSettingsBeforeTheChown(t *testing.T) {
	h := newHarness(t)
	f := &fakeSettings{b: []byte(testSettings), ok: true}
	h.m.ArtifactSettings = f.get
	h.m.RecordVisibility("darkmem", false)
	dir := h.m.instanceDir("aaaaaa")
	var atChown []byte
	h.host.onChown = func(path string) {
		atChown, _ = os.ReadFile(filepath.Join(path, ArtifactSettingsFile))
	}
	if err := h.m.spawn(context.Background(), h.cfg, "darkmem"); err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 || string(atChown) != testSettings {
		t.Fatalf("%d calls; settings file at the chown %q", f.calls, atChown)
	}
	if !reflect.DeepEqual(h.host.chowned, []string{dir + ":" + RunnerUser}) {
		t.Fatalf("chown %v", h.host.chowned)
	}
	checkSettingsFile(t, dir, testSettings)
	if len(h.sd.started) != 1 {
		t.Fatalf("started %d units", len(h.sd.started))
	}
}

func TestSpawnWritesNoArtifactSettingsUnlessThePrivateRepositoryGetsSome(t *testing.T) {
	for _, tc := range []struct {
		name      string
		set       bool // the function is set
		ok        bool // what it answers
		vis       func(h *harness)
		wantCalls int
	}{
		{name: "no function", vis: func(h *harness) { h.m.RecordVisibility("darkmem", false) }},
		{name: "the function says no", set: true, vis: func(h *harness) { h.m.RecordVisibility("darkmem", false) }, wantCalls: 1},
		{name: "public with allow_public", set: true, ok: true, vis: func(h *harness) {
			allowPublic(h, "darkmem")
			h.m.RecordVisibility("darkmem", true)
		}},
		{name: "visibility unread", set: true, ok: true, vis: func(*harness) {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			f := &fakeSettings{b: []byte(testSettings), ok: tc.ok}
			if tc.set {
				h.m.ArtifactSettings = f.get
			}
			tc.vis(h)
			if err := h.m.spawn(context.Background(), h.cfg, "darkmem"); err != nil {
				t.Fatal(err)
			}
			if f.calls != tc.wantCalls {
				t.Fatalf("%d calls, want %d", f.calls, tc.wantCalls)
			}
			if _, err := os.Lstat(filepath.Join(h.m.instanceDir("aaaaaa"), "_work")); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("_work: %v; want none", err)
			}
			if len(h.sd.started) != 1 {
				t.Fatalf("started %d units", len(h.sd.started))
			}
		})
	}
}

func TestSpawnStartsTheRunnerWhenTheSettingsWriteFails(t *testing.T) {
	for _, at := range []string{"create", "write", "close"} {
		t.Run(at, func(t *testing.T) {
			h := newHarness(t)
			h.m.ArtifactSettings = (&fakeSettings{b: []byte(testSettings), ok: true}).get
			h.m.RecordVisibility("darkmem", false)
			h.m.openSettingsTemp = failSettingsAt(at)
			if err := h.m.spawn(context.Background(), h.cfg, "darkmem"); err != nil {
				t.Fatal(err)
			}
			if len(h.sd.started) != 1 || h.state("aaaaaa") != "starting" {
				t.Fatalf("started %d units, state %s", len(h.sd.started), h.state("aaaaaa"))
			}
			dir := h.m.instanceDir("aaaaaa")
			for _, name := range []string{ArtifactSettingsFile, artifactSettingsTemp} {
				if _, err := os.Lstat(filepath.Join(dir, name)); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("%s: %v; want none", name, err)
				}
			}
			ev := h.eventText()
			want := "warn darkmem spawn aaaaaa: cannot write the artifact store settings: disk on fire at " + at + "; its job uses GitHub's storage\n"
			if !strings.Contains(ev, want) || strings.Contains(ev, "s3cr3t-job-key") {
				t.Fatalf("events:\n%s\nwant %q and no secret", ev, want)
			}
		})
	}
}

const newSettings = `{"v":1,"endpoint":"https://s3.example.test","bucket":"ci","region":"us-east-1","key_id":"GHRKEY2","secret":"n3w-job-key"}`

// settingsRunner spawns runner aaaaaa of the private darkmem with the
// settings file and puts it in state. It returns the settings function, for
// the test to change, and the file's path.
func settingsRunner(t *testing.T, h *harness, state sched.State) (*fakeSettings, string) {
	t.Helper()
	f := &fakeSettings{b: []byte(testSettings), ok: true}
	h.m.ArtifactSettings = f.get
	h.m.RecordVisibility("darkmem", false)
	if err := h.m.spawn(context.Background(), h.cfg, "darkmem"); err != nil {
		t.Fatal(err)
	}
	h.m.mu.Lock()
	h.m.insts["aaaaaa"].State = state
	h.m.mu.Unlock()
	return f, filepath.Join(h.m.instanceDir("aaaaaa"), ArtifactSettingsFile)
}

const removedEvent = "info darkmem artifact store settings removed from aaaaaa: its repository no longer uses self-hosted storage\n"

func TestSyncArtifactSettingsRemovesAFileNoLongerWanted(t *testing.T) {
	for _, tc := range []struct {
		name   string
		state  sched.State
		change func(h *harness, f *fakeSettings)
	}{
		{"idle, the function says no", sched.Idle, func(_ *harness, f *fakeSettings) { f.ok = false }},
		{"idle, the repository turned public", sched.Idle, func(h *harness, _ *fakeSettings) { h.m.RecordVisibility("darkmem", true) }},
		{"starting, the function says no", sched.Starting, func(_ *harness, f *fakeSettings) { f.ok = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			f, path := settingsRunner(t, h, tc.state)
			tc.change(h, f)
			h.m.syncArtifactSettings()
			if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("settings file: %v; want none", err)
			}
			if !strings.Contains(h.eventText(), removedEvent) {
				t.Fatalf("events:\n%s", h.eventText())
			}
		})
	}
}

// After a restart the manager has not read the repository's visibility
// yet; the file stays until it has, whatever the daemon answers.
func TestSyncArtifactSettingsKeepsAFileWhileVisibilityIsUnread(t *testing.T) {
	h := newHarness(t)
	f, path := settingsRunner(t, h, sched.Idle)
	f.ok = false
	h.m.mu.Lock()
	delete(h.m.vis, "darkmem")
	h.m.mu.Unlock()
	h.m.syncArtifactSettings()
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("settings file removed while visibility is unread: %v", err)
	}
	if strings.Contains(h.eventText(), "artifact store settings") {
		t.Fatalf("events:\n%s", h.eventText())
	}
}

// New settings, such as a replaced job key, are written over the old ones
// once; the same settings are left alone.
func TestSyncArtifactSettingsRewritesChangedSettings(t *testing.T) {
	h := newHarness(t)
	f, path := settingsRunner(t, h, sched.Idle)
	f.b = []byte(newSettings)
	h.m.syncArtifactSettings()
	checkSettingsFile(t, h.m.instanceDir("aaaaaa"), newSettings)
	const updated = "info darkmem artifact store settings of aaaaaa updated\n"
	if strings.Count(h.eventText(), updated) != 1 {
		t.Fatalf("events:\n%s", h.eventText())
	}
	before, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	h.m.syncArtifactSettings()
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("equal settings were written again: %v", err)
	}
	if strings.Count(h.eventText(), updated) != 1 {
		t.Fatalf("events:\n%s", h.eventText())
	}
	if strings.Contains(h.eventText(), "n3w-job-key") {
		t.Fatal("an event holds the secret")
	}
}

// The rewrite keeps the file the runner user's, as the spawn's chown left it.
func TestWriteArtifactSettingsKeepsTheOwnerOfTheFileItReplaces(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("giving the file another owner needs root")
	}
	h := newHarness(t)
	dir := t.TempDir()
	if err := h.m.writeArtifactSettings(dir, []byte(testSettings)); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ArtifactSettingsFile)
	if err := os.Lchown(path, 65534, 65534); err != nil {
		t.Fatal(err)
	}
	if err := h.m.writeArtifactSettings(dir, []byte(newSettings)); err != nil {
		t.Fatal(err)
	}
	checkSettingsFile(t, dir, newSettings)
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if uid, gid, ok := fileOwner(fi); !ok || uid != 65534 || gid != 65534 {
		t.Fatalf("owner %d:%d (%v), want 65534:65534", uid, gid, ok)
	}
}

func TestSyncArtifactSettingsLeavesSomeRunnersAlone(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state sched.State
		set   func(i *instance)
		file  bool // the runner has a settings file
		want  string
	}{
		{name: "busy", state: sched.Busy, file: true, want: testSettings},
		{name: "finishing", state: sched.Idle, set: func(i *instance) { i.finishing = true }, file: true, want: testSettings},
		{name: "stopping", state: sched.Idle, set: func(i *instance) { i.stopping = true }, file: true, want: testSettings},
		{name: "idle without a file", state: sched.Idle},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			f, path := settingsRunner(t, h, tc.state)
			if tc.set != nil {
				h.m.mu.Lock()
				tc.set(h.m.insts["aaaaaa"])
				h.m.mu.Unlock()
			}
			if tc.file {
				f.ok = false
			} else {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				f.b = []byte(newSettings)
			}
			h.m.syncArtifactSettings()
			got, err := os.ReadFile(path)
			if tc.want == "" && !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("settings file %q, %v; want none", got, err)
			}
			if tc.want != "" && string(got) != tc.want {
				t.Fatalf("settings file %q, %v; want %q", got, err, tc.want)
			}
			if strings.Contains(h.eventText(), "artifact store settings") {
				t.Fatalf("events:\n%s", h.eventText())
			}
		})
	}
}

func TestSyncArtifactSettingsHandlesAnAdoptedRunner(t *testing.T) {
	h := newHarness(t)
	makeInstance(t, h, "aaaaaa", "darkmem", 1, false)
	h.sd.active["ghr-runner-aaaaaa"] = true
	if err := h.m.Adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.m.mu.Lock()
	adopted := h.m.insts["aaaaaa"] != nil && h.m.insts["aaaaaa"].adopted
	h.m.mu.Unlock()
	if !adopted {
		t.Fatal("aaaaaa was not adopted")
	}
	dir := h.m.instanceDir("aaaaaa")
	if err := h.m.writeArtifactSettings(dir, []byte(testSettings)); err != nil {
		t.Fatal(err)
	}
	h.m.ArtifactSettings = (&fakeSettings{}).get
	h.m.RecordVisibility("darkmem", false)
	h.m.syncArtifactSettings()
	if _, err := os.Lstat(filepath.Join(dir, ArtifactSettingsFile)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("settings file: %v; want none", err)
	}
	if !strings.Contains(h.eventText(), removedEvent) {
		t.Fatalf("events:\n%s", h.eventText())
	}
}

func TestSyncArtifactSettingsWarnsOncePerFailure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(t *testing.T, h *harness, f *fakeSettings, path string)
		want   string
	}{
		{"update", func(_ *testing.T, h *harness, f *fakeSettings, _ string) {
			f.b = []byte(newSettings)
			h.m.openSettingsTemp = failSettingsAt("create")
		}, "warn darkmem cannot update the artifact store settings of aaaaaa: disk on fire at create\n"},
		// A directory that is not empty cannot be removed.
		{"remove", func(t *testing.T, _ *harness, f *fakeSettings, path string) {
			f.ok = false
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(path, "x"), 0o700); err != nil {
				t.Fatal(err)
			}
		}, "warn darkmem cannot remove the artifact store settings from aaaaaa: "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			f, path := settingsRunner(t, h, sched.Idle)
			tc.change(t, h, f, path)
			h.m.syncArtifactSettings()
			h.m.syncArtifactSettings()
			if n := strings.Count(h.eventText(), tc.want); n != 1 {
				t.Fatalf("%d warnings, want 1; events:\n%s", n, h.eventText())
			}
		})
	}
}

func TestTickKeepsArtifactSettingsInStep(t *testing.T) {
	h := newHarness(t)
	f, path := settingsRunner(t, h, sched.Starting)
	f.ok = false
	h.m.Tick(context.Background())
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("settings file: %v; want none", err)
	}
	if !strings.Contains(h.eventText(), removedEvent) {
		t.Fatalf("events:\n%s", h.eventText())
	}
}

func TestSyncArtifactSettingsLeavesARunnerWithAJobFileAlone(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(f *fakeSettings)
	}{
		{"remove", func(f *fakeSettings) { f.ok = false }},
		{"rewrite", func(f *fakeSettings) { f.b = []byte(newSettings) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			f, path := settingsRunner(t, h, sched.Idle)
			if err := os.WriteFile(filepath.Join(h.m.instanceDir("aaaaaa"), JobFile), []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
			tc.change(f)
			h.m.syncArtifactSettings()
			if got, err := os.ReadFile(path); err != nil || string(got) != testSettings {
				t.Fatalf("settings file %q, %v; want %q", got, err, testSettings)
			}
			if strings.Contains(h.eventText(), "artifact store settings") {
				t.Fatalf("events:\n%s", h.eventText())
			}
		})
	}
}
