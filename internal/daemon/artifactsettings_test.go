package daemon

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/darkraise/ghr/internal/artifactstore"
	"github.com/darkraise/ghr/internal/changes"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/events"
)

const settingsJSON = `{"v":1,"endpoint":"https://s3.example.test","bucket":"gha-artifacts","region":"us-east-1","key_id":"JOBKEY12345678","secret":"job-secret-value"}`

func testJobKey() *artifactstore.Key {
	return &artifactstore.Key{ID: "JOBKEY12345678", Secret: "job-secret-value"}
}

// Each of the four conditions alone keeps the settings from a runner.
func TestArtifactSettings(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, b *Backend, f *fakeAS)
		repo  string
		want  string
	}{
		{"all four hold", nil, "darkmem", settingsJSON},
		{"named in another case", nil, "DarkMem", settingsJSON},
		{"the store is not set up", func(t *testing.T, b *Backend, _ *fakeAS) {
			if err := b.update(func(c *config.Config) error {
				c.ArtifactStore.Endpoint, c.ArtifactStore.Bucket = "", ""
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}, "darkmem", ""},
		{"no job key", func(_ *testing.T, _ *Backend, f *fakeAS) { f.jobKey = nil }, "darkmem", ""},
		{"the entry is off", nil, "darkcloud", ""},
		{"a watched-only repository with an on entry", nil, "homelab", ""},
		{"an on entry for a repository ghr does not cover", nil, "oldrepo", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, _, _, f := newASBackend(t)
			withArtifactStore(t, b, map[string]config.ArtifactRepo{
				"darkmem": {Enabled: true}, "darkcloud": {Cleanup: "3d"}, "homelab": {Enabled: true}, "oldrepo": {Enabled: true},
			})
			f.jobKey = testJobKey()
			if tc.setup != nil {
				tc.setup(t, b, f)
			}
			got, ok := b.artifactSettings(tc.repo)
			if ok != (tc.want != "") || string(got) != tc.want {
				t.Fatalf("settings %s, %v; want %s", got, ok, tc.want)
			}
		})
	}
}

// The settings follow the config and the job key as they are at each call,
// not as they were when the backend was built.
func TestArtifactSettingsReadTheCurrentConfig(t *testing.T) {
	b, _, _, f := newASBackend(t)
	withArtifactStore(t, b, nil)
	f.jobKey = testJobKey()
	settings := b.artifactSettings
	if got, ok := settings("darkmem"); ok {
		t.Fatalf("settings %s for a repository switched off", got)
	}
	if err := b.update(func(c *config.Config) error {
		c.ArtifactStore.Repos = map[string]config.ArtifactRepo{"darkmem": {Enabled: true}}
		c.ArtifactStore.Bucket, c.ArtifactStore.Region = "ci-artifacts", "ap-southeast-1"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := `{"v":1,"endpoint":"https://s3.example.test","bucket":"ci-artifacts","region":"ap-southeast-1","key_id":"JOBKEY12345678","secret":"job-secret-value"}`
	if got, ok := settings("darkmem"); !ok || string(got) != want {
		t.Fatalf("settings %s, %v after switching on", got, ok)
	}
	f.jobKey = &artifactstore.Key{ID: "NEWJOBKEY0002", Secret: "rotated-secret"}
	want = `{"v":1,"endpoint":"https://s3.example.test","bucket":"ci-artifacts","region":"ap-southeast-1","key_id":"NEWJOBKEY0002","secret":"rotated-secret"}`
	if got, ok := settings("darkmem"); !ok || string(got) != want {
		t.Fatalf("settings %s, %v after a key replacement", got, ok)
	}
	if err := b.update(func(c *config.Config) error {
		c.Repos = slices.DeleteFunc(c.Repos, func(r config.Repo) bool { return r.Name == "darkmem" })
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got, ok := settings("darkmem"); ok {
		t.Fatalf("settings %s after darkmem left repos", got)
	}
}

func TestArtifactSettingsWithoutTheService(t *testing.T) {
	b, _, _ := newBackend(t)
	withArtifactStore(t, b, map[string]config.ArtifactRepo{"darkmem": {Enabled: true}})
	if got, ok := b.artifactSettings("darkmem"); ok || got != nil {
		t.Fatalf("settings %s, %v without an artifact store service", got, ok)
	}
}

// start hands the runner manager the backend's settings, which read the
// service's job key, before the first tick.
func TestStartHandsTheManagerTheArtifactSettings(t *testing.T) {
	cfg := cfgYAML + "artifact_store:\n  endpoint: https://s3.example.test\n  bucket: gha-artifacts\n  repos:\n    darkmem: { enabled: true }\n"
	o := testOptions(t, cfg)
	if err := artifactstore.SaveKeys(o.ArtifactKeysPath, artifactstore.Keys{Job: testJobKey()}); err != nil {
		t.Fatal(err)
	}
	store, _, err := OpenStore(o.ConfigPath, o.TokenPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	hub := changes.New(hubOptions)
	rt, err := start(ctx, o, store, events.New(), hub, "test", config.Web{}, nil, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		rt.b.StopBackground()
		hub.Close()
		rt.m.Close()
		rt.space.Close()
		rt.ghs.Close()
		rt.as.Close()
		rt.b.Close()
		<-rt.sampled
		rt.m.Wait()
		rt.space.Wait()
		rt.ghs.Wait()
		rt.as.Wait()
		rt.b.Wait()
	})
	if rt.m.ArtifactSettings == nil {
		t.Fatal("the manager has no artifact settings function")
	}
	if got, ok := rt.m.ArtifactSettings("darkmem"); !ok || string(got) != settingsJSON {
		t.Fatalf("settings %s, %v", got, ok)
	}
	if got, ok := rt.m.ArtifactSettings("darkcloud"); ok || strings.Contains(string(got), "job-secret-value") {
		t.Fatalf("settings %s, %v for a repository switched off", got, ok)
	}
}
