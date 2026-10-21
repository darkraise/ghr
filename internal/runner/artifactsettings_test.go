package runner

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
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
