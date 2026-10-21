package daemon

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

// withArtifactStore saves an artifact_store block with repos, and watches
// homelab so a watched repository is covered too.
func withArtifactStore(t *testing.T, b *Backend, repos map[string]config.ArtifactRepo) {
	t.Helper()
	if _, err := b.Store.Update(func(c *config.Config) error {
		c.WatchRepos = []string{"homelab"}
		c.ArtifactStore = config.ArtifactStore{Endpoint: "https://s3.example.test", Bucket: "gha-artifacts", Repos: repos}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// darkraiseRepos is ListUserRepos's answer for the owner's repositories,
// each name mapped to whether it is private.
func darkraiseRepos(private map[string]bool) []github.UserRepo {
	var out []github.UserRepo
	for name, p := range private {
		out = append(out, github.UserRepo{Name: name, Private: p, Owner: github.Account{Login: "darkraise"}})
	}
	return out
}

func withJobKey() model.ArtifactStore {
	return model.ArtifactStore{Configured: true, JobKey: &model.ArtifactKeyInfo{ID: "JOBK…5678"}}
}

func TestSetArtifactRepoSavesTheSwitchAndSyncs(t *testing.T) {
	on, off := true, false
	threeDays, none := "3d", ""
	for _, tc := range []struct {
		name  string
		start map[string]config.ArtifactRepo
		repo  string
		req   model.ArtifactRepoRequest
		want  map[string]config.ArtifactRepo
		sync  string
	}{
		{"on, named in another case", nil, "DarkMem", model.ArtifactRepoRequest{Enabled: &on},
			map[string]config.ArtifactRepo{"darkmem": {Enabled: true}}, "darkmem"},
		{"on for a watched repository", nil, "homelab", model.ArtifactRepoRequest{Enabled: &on},
			map[string]config.ArtifactRepo{"homelab": {Enabled: true}}, "homelab"},
		{"on keeps the cleanup", map[string]config.ArtifactRepo{"darkmem": {Cleanup: "3d"}}, "darkmem", model.ArtifactRepoRequest{Enabled: &on},
			map[string]config.ArtifactRepo{"darkmem": {Enabled: true, Cleanup: "3d"}}, "darkmem"},
		{"a cleanup override", nil, "darkmem", model.ArtifactRepoRequest{Cleanup: &threeDays},
			map[string]config.ArtifactRepo{"darkmem": {Cleanup: "3d"}}, "darkmem"},
		{"off keeps the cleanup", map[string]config.ArtifactRepo{"darkmem": {Enabled: true, Cleanup: "3d"}}, "darkmem", model.ArtifactRepoRequest{Enabled: &off},
			map[string]config.ArtifactRepo{"darkmem": {Cleanup: "3d"}}, "darkmem"},
		{"off with no override drops the entry", map[string]config.ArtifactRepo{"darkmem": {Enabled: true, Cleanup: "3d"}}, "darkmem",
			model.ArtifactRepoRequest{Enabled: &off, Cleanup: &none}, nil, "darkmem"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, _, gh, f := newASBackend(t)
			withArtifactStore(t, b, tc.start)
			f.snap = withJobKey()
			gh.userRepos = darkraiseRepos(map[string]bool{"darkmem": true, "homelab": true})
			row, err := b.SetArtifactRepo(context.Background(), tc.repo, tc.req)
			if err != nil || row.Name != tc.sync || row.State != "set" {
				t.Fatalf("row %+v, %v", row, err)
			}
			if got := b.Store.Config().ArtifactStore.Repos; !maps.Equal(got, tc.want) {
				t.Fatalf("repos %v, want %v", got, tc.want)
			}
			if got := f.recorded(); !slices.Equal(got, []string{"policy", "sync " + tc.sync}) {
				t.Fatalf("calls %q", got)
			}
		})
	}
}

// A refused request changes no config and reaches neither the service nor
// GitHub's writes.
func TestSetArtifactRepoRefusals(t *testing.T) {
	on := true
	tenMinutes, soon := "10m", "soon"
	update := func(t *testing.T, b *Backend, fn func(c *config.Config)) {
		t.Helper()
		if _, err := b.Store.Update(func(c *config.Config) error { fn(c); return nil }); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name   string
		setup  func(t *testing.T, b *Backend, m *fakeManager, gh *fakeGH, f *fakeAS)
		repo   string
		req    model.ArtifactRepoRequest
		status int
		msg    string
	}{
		{"not set up", func(t *testing.T, b *Backend, _ *fakeManager, _ *fakeGH, _ *fakeAS) {
			update(t, b, func(c *config.Config) { c.ArtifactStore = config.ArtifactStore{} })
		}, "darkmem", model.ArtifactRepoRequest{Enabled: &on}, 409, "self-hosted storage is not set up"},
		{"an uncovered repository", nil, "nope", model.ArtifactRepoRequest{Enabled: &on}, 404,
			"nope is not a covered repository; add it as a runner or watched repository first"},
		{"a repository being removed", func(t *testing.T, b *Backend, _ *fakeManager, _ *fakeGH, _ *fakeAS) {
			update(t, b, func(c *config.Config) { r := c.Repo("darkmem"); r.Paused, r.Removing = true, true })
		}, "darkmem", model.ArtifactRepoRequest{Enabled: &on}, 404, "darkmem is not a covered repository"},
		{"nothing to change", nil, "darkmem", model.ArtifactRepoRequest{}, 400, "give enabled or cleanup"},
		{"a cleanup under an hour", nil, "darkmem", model.ArtifactRepoRequest{Cleanup: &tenMinutes}, 400, "cleanup must be at least 1h, or off"},
		{"a cleanup that is not a duration", nil, "darkmem", model.ArtifactRepoRequest{Cleanup: &soon}, 400, "cleanup: "},
		{"no job key", func(_ *testing.T, _ *Backend, _ *fakeManager, _ *fakeGH, f *fakeAS) {
			f.snap.JobKey = nil
		}, "darkmem", model.ArtifactRepoRequest{Enabled: &on}, 409, "set the job key first"},
		{"a public repository", nil, "darkcloud", model.ArtifactRepoRequest{Enabled: &on}, 409,
			"darkcloud is public; self-hosted storage serves only private repositories"},
		{"a repository the token cannot see", nil, "homelab", model.ArtifactRepoRequest{Enabled: &on}, 400,
			"token cannot see darkraise/homelab: add it to the PAT's repository access first"},
		{"GitHub rejecting the token", func(_ *testing.T, _ *Backend, m *fakeManager, _ *fakeGH, _ *fakeAS) {
			m.degraded = "Bad credentials"
		}, "darkmem", model.ArtifactRepoRequest{Enabled: &on}, 503, "GitHub is rejecting the token: Bad credentials"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, m, gh, f := newASBackend(t)
			withArtifactStore(t, b, nil)
			f.snap = withJobKey()
			gh.userRepos = darkraiseRepos(map[string]bool{"darkmem": true, "darkcloud": false})
			if tc.setup != nil {
				tc.setup(t, b, m, gh, f)
			}
			before := b.Store.Config().ArtifactStore.Repos
			_, err := b.SetArtifactRepo(context.Background(), tc.repo, tc.req)
			if apiStatus(err) != tc.status || !strings.HasPrefix(apiMsg(err), tc.msg) {
				t.Fatalf("status %d message %q, want %d %q", apiStatus(err), apiMsg(err), tc.status, tc.msg)
			}
			if got := b.Store.Config().ArtifactStore.Repos; !maps.Equal(got, before) {
				t.Fatalf("repos changed to %v", got)
			}
			if got := f.recorded(); len(got) != 0 {
				t.Fatalf("calls %q", got)
			}
		})
	}
}

// Only a snapshot row that says private is trusted; a row from before the
// visibility was known is checked again at GitHub.
func TestSetArtifactRepoTrustsOnlyAPrivateRow(t *testing.T) {
	b, _, gh, f := newASBackend(t)
	withArtifactStore(t, b, nil)
	ctx := context.Background()
	on := true
	f.snap = withJobKey()
	f.snap.Repos = []model.ArtifactStoreRepo{{Name: "darkmem"}, {Name: "homelab", Private: true}}
	gh.userRepos = darkraiseRepos(map[string]bool{"darkmem": true})
	if _, err := b.SetArtifactRepo(ctx, "darkmem", model.ArtifactRepoRequest{Enabled: &on}); err != nil {
		t.Fatal(err)
	}
	gh.userRepos, gh.repoErr = nil, errors.New("ListUserRepos must not be called")
	if _, err := b.SetArtifactRepo(ctx, "homelab", model.ArtifactRepoRequest{Enabled: &on}); err != nil {
		t.Fatal(err)
	}
	if a := b.Store.Config().ArtifactStore; !a.Enabled("darkmem") || !a.Enabled("homelab") {
		t.Fatalf("repos %v", a.Repos)
	}
}

// GitHub refusing the sync names the missing permission; the switch stays
// saved, and each pass retries it.
func TestSetArtifactRepoKeepsTheSwitchWhenTheSyncFails(t *testing.T) {
	b, _, gh, f := newASBackend(t)
	withArtifactStore(t, b, nil)
	f.snap = withJobKey()
	gh.userRepos = darkraiseRepos(map[string]bool{"darkmem": true})
	f.syncErr = &github.APIError{Status: 403, Kind: github.ErrAuth, Message: "Resource not accessible by personal access token", Permissions: "secrets=write"}
	on := true
	_, err := b.SetArtifactRepo(context.Background(), "darkmem", model.ArtifactRepoRequest{Enabled: &on})
	if apiStatus(err) != 403 || apiMsg(err) != "token lacks secrets=write on darkraise/darkmem: grant it in the PAT settings" {
		t.Fatalf("status %d message %q", apiStatus(err), apiMsg(err))
	}
	if !b.Store.Config().ArtifactStore.Enabled("darkmem") {
		t.Fatal("the switch was not saved")
	}
}

func TestEnablePrivateArtifactRepos(t *testing.T) {
	b, _, gh, f := newASBackend(t)
	withArtifactStore(t, b, map[string]config.ArtifactRepo{"darkmem": {Cleanup: "3d"}})
	f.snap = withJobKey()
	gh.userRepos = append(darkraiseRepos(map[string]bool{"darkmem": true, "darkcloud": false, "homelab": true}),
		github.UserRepo{Name: "darkcloud", Private: true, Owner: github.Account{Login: "someone"}})
	rows, err := b.EnablePrivateArtifactRepos(context.Background())
	if err != nil || len(rows) != 2 || rows[0].Name != "darkmem" || rows[1].Name != "homelab" {
		t.Fatalf("rows %+v, %v", rows, err)
	}
	want := map[string]config.ArtifactRepo{"darkmem": {Enabled: true, Cleanup: "3d"}, "homelab": {Enabled: true}}
	if got := b.Store.Config().ArtifactStore.Repos; !maps.Equal(got, want) {
		t.Fatalf("repos %v", got)
	}
	if got := f.recorded(); !slices.Equal(got, []string{"policy", "sync darkmem", "sync homelab"}) {
		t.Fatalf("calls %q", got)
	}
}

func TestEnablePrivateArtifactReposRefusalsAndFailures(t *testing.T) {
	ctx := context.Background()
	t.Run("not set up", func(t *testing.T) {
		b, _, _, f := newASBackend(t)
		f.snap = withJobKey()
		if _, err := b.EnablePrivateArtifactRepos(ctx); apiStatus(err) != 409 || apiMsg(err) != "self-hosted storage is not set up" {
			t.Fatalf("%v", err)
		}
	})
	t.Run("no job key", func(t *testing.T) {
		b, _, _, f := newASBackend(t)
		withArtifactStore(t, b, nil)
		if _, err := b.EnablePrivateArtifactRepos(ctx); apiStatus(err) != 409 || apiMsg(err) != "set the job key first" {
			t.Fatalf("%v", err)
		}
		if got := f.recorded(); len(got) != 0 {
			t.Fatalf("calls %q", got)
		}
	})
	t.Run("no private repository", func(t *testing.T) {
		b, _, gh, f := newASBackend(t)
		withArtifactStore(t, b, nil)
		f.snap = withJobKey()
		gh.userRepos = darkraiseRepos(map[string]bool{"darkmem": false})
		rows, err := b.EnablePrivateArtifactRepos(ctx)
		if err != nil || rows == nil || len(rows) != 0 {
			t.Fatalf("rows %v, %v", rows, err)
		}
		if got := f.recorded(); len(got) != 0 {
			t.Fatalf("calls %q", got)
		}
	})
	t.Run("a repository another daemon stores for", func(t *testing.T) {
		b, _, gh, f := newASBackend(t)
		withArtifactStore(t, b, nil)
		f.snap = withJobKey()
		f.snap.Repos = []model.ArtifactStoreRepo{{Name: "homelab", Private: true, State: "foreign"}}
		gh.userRepos = darkraiseRepos(map[string]bool{"darkmem": true, "homelab": true})
		rows, err := b.EnablePrivateArtifactRepos(ctx)
		if err != nil || len(rows) != 1 || rows[0].Name != "darkmem" {
			t.Fatalf("rows %+v, %v", rows, err)
		}
		if a := b.Store.Config().ArtifactStore; !a.Enabled("darkmem") || a.Enabled("homelab") {
			t.Fatalf("repos %v", a.Repos)
		}
		if got := f.recorded(); !slices.Equal(got, []string{"policy", "sync darkmem"}) {
			t.Fatalf("calls %q", got)
		}
	})
	t.Run("a failed sync", func(t *testing.T) {
		b, _, gh, f := newASBackend(t)
		withArtifactStore(t, b, nil)
		f.snap = withJobKey()
		gh.userRepos = darkraiseRepos(map[string]bool{"darkmem": true, "homelab": true})
		f.syncErr = errors.New("connection reset")
		if _, err := b.EnablePrivateArtifactRepos(ctx); err == nil || err.Error() != "connection reset" {
			t.Fatalf("%v", err)
		}
		if a := b.Store.Config().ArtifactStore; !a.Enabled("darkmem") || !a.Enabled("homelab") {
			t.Fatalf("repos %v", a.Repos)
		}
		if got := f.recorded(); !slices.Equal(got, []string{"policy", "sync darkmem", "kick"}) {
			t.Fatalf("calls %q", got)
		}
	})
}

// offProbe records, for each SwitchOff, whether config.yaml still had the
// repository switched on when it ran.
type offProbe struct {
	*fakeAS
	b       *Backend
	onAtOff []bool
}

func (p *offProbe) SwitchOff(ctx context.Context, repo string) error {
	p.onAtOff = append(p.onAtOff, p.b.Store.Config().ArtifactStore.Enabled(repo))
	return p.fakeAS.SwitchOff(ctx, repo)
}

func probeSwitchOff(b *Backend, f *fakeAS) *offProbe {
	p := &offProbe{fakeAS: f, b: b}
	b.AS = p
	return p
}

// Removing or unwatching a repository whose storage is on switches it off
// first, after saving the switch off, so a pass cannot write the variable
// and secret back; every way out of coverage drops its artifact_store entry.
func TestRemovingCoverageSwitchesArtifactsOff(t *testing.T) {
	b, _, _, f := newASBackend(t)
	p := probeSwitchOff(b, f)
	withArtifactStore(t, b, map[string]config.ArtifactRepo{
		"darkmem": {Enabled: true}, "darkcloud": {Cleanup: "3d"}, "homelab": {Enabled: true},
	})
	if err := b.UnwatchRepo("darkmem"); apiStatus(err) != 404 {
		t.Fatalf("unwatching a runner repository: %v", err)
	}
	if err := b.RemoveRepo("darkmem"); err != nil {
		t.Fatal(err)
	}
	if err := b.RemoveRepo("darkcloud"); err != nil {
		t.Fatal(err)
	}
	if err := b.UnwatchRepo("homelab"); err != nil {
		t.Fatal(err)
	}
	cfg := b.Store.Config()
	if cfg.ArtifactStore.Repos != nil || !cfg.Repo("darkmem").Removing || cfg.Watched("homelab") {
		t.Fatalf("repos %v, darkmem %+v, watched %v", cfg.ArtifactStore.Repos, cfg.Repo("darkmem"), cfg.WatchRepos)
	}
	if got := f.recorded(); !slices.Equal(got, []string{"switch-off darkmem", "switch-off homelab"}) {
		t.Fatalf("calls %q", got)
	}
	if !slices.Equal(p.onAtOff, []bool{false, false}) {
		t.Fatalf("switched on in config.yaml during SwitchOff: %v", p.onAtOff)
	}
}

// A failed switch-off restores the saved switch and refuses the removal.
func TestRemovalRefusedWhenTheSwitchOffFails(t *testing.T) {
	b, m, _, f := newASBackend(t)
	p := probeSwitchOff(b, f)
	withArtifactStore(t, b, map[string]config.ArtifactRepo{"darkmem": {Enabled: true}, "homelab": {Enabled: true}})
	f.offErr = &github.APIError{Status: 403, Kind: github.ErrAuth, Message: "Resource not accessible by personal access token", Permissions: "variables=write"}
	err := b.RemoveRepo("darkmem")
	if want := "could not switch off self-hosted storage for darkmem, so it was not removed: token lacks variables=write on darkraise/darkmem: grant it in the PAT settings"; apiStatus(err) != 403 || apiMsg(err) != want {
		t.Fatalf("status %d message %q", apiStatus(err), apiMsg(err))
	}
	f.offErr = errors.New("connection reset")
	err = b.UnwatchRepo("homelab")
	if want := "could not switch off self-hosted storage for homelab, so it is still watched: connection reset"; apiStatus(err) != 502 || apiMsg(err) != want {
		t.Fatalf("status %d message %q", apiStatus(err), apiMsg(err))
	}
	f.offErr = nil
	m.degraded = "Bad credentials"
	if err := b.RemoveRepo("darkmem"); apiStatus(err) != 503 {
		t.Fatalf("while GitHub rejects the token: %v", err)
	}
	cfg := b.Store.Config()
	if cfg.Repo("darkmem").Removing || !cfg.Watched("homelab") || !cfg.ArtifactStore.Enabled("darkmem") || !cfg.ArtifactStore.Enabled("homelab") {
		t.Fatalf("a refused removal changed the config: %+v %v %v", cfg.Repo("darkmem"), cfg.WatchRepos, cfg.ArtifactStore.Repos)
	}
	if got := f.recorded(); !slices.Equal(got, []string{"switch-off darkmem", "switch-off homelab"}) {
		t.Fatalf("calls %q", got)
	}
	if !slices.Equal(p.onAtOff, []bool{false, false}) {
		t.Fatalf("switched on in config.yaml during SwitchOff: %v", p.onAtOff)
	}
}

func TestArtifactStorePolicyAndTokenReachTheService(t *testing.T) {
	b, _, _, f := newASBackend(t)
	withArtifactStore(t, b, nil)
	ctx := context.Background()
	if err := patchJSON(t, b, `{"artifact_store":{"capacity_gb":50}}`); err != nil {
		t.Fatal(err)
	}
	all := "all"
	if err := b.PatchConfig(ctx, model.ConfigPatch{Mode: &all}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Reload(); err != nil {
		t.Fatal(err)
	}
	if err := b.SetToken(ctx, "tok2"); err != nil {
		t.Fatal(err)
	}
	if got := f.recorded(); !slices.Equal(got, []string{"policy", "kick", "policy", "kick", "kick"}) {
		t.Fatalf("calls %q", got)
	}
}
