package daemon

import (
	"context"
	"errors"
	"maps"
	"slices"
	"testing"
	"time"

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

// A saved switch or cleanup reaches the service as a policy change, and the
// answer is the service's row; nothing goes to GitHub.
func TestSetArtifactRepoSavesTheSwitch(t *testing.T) {
	on, off := true, false
	threeDays, none := "3d", ""
	for _, tc := range []struct {
		name  string
		start map[string]config.ArtifactRepo
		repo  string
		req   model.ArtifactRepoRequest
		want  map[string]config.ArtifactRepo
		row   string
	}{
		{"on, named in another case", nil, "DarkMem", model.ArtifactRepoRequest{Enabled: &on},
			map[string]config.ArtifactRepo{"darkmem": {Enabled: true}}, "darkmem"},
		{"on keeps the cleanup", map[string]config.ArtifactRepo{"darkmem": {Cleanup: "3d"}}, "darkmem", model.ArtifactRepoRequest{Enabled: &on},
			map[string]config.ArtifactRepo{"darkmem": {Enabled: true, Cleanup: "3d"}}, "darkmem"},
		{"a cleanup override", nil, "darkmem", model.ArtifactRepoRequest{Cleanup: &threeDays},
			map[string]config.ArtifactRepo{"darkmem": {Cleanup: "3d"}}, "darkmem"},
		{"a cleanup override for a watched repository", nil, "homelab", model.ArtifactRepoRequest{Cleanup: &threeDays},
			map[string]config.ArtifactRepo{"homelab": {Cleanup: "3d"}}, "homelab"},
		{"off keeps the cleanup", map[string]config.ArtifactRepo{"darkmem": {Enabled: true, Cleanup: "3d"}}, "darkmem", model.ArtifactRepoRequest{Enabled: &off},
			map[string]config.ArtifactRepo{"darkmem": {Cleanup: "3d"}}, "darkmem"},
		{"off with no override drops the entry", map[string]config.ArtifactRepo{"darkmem": {Enabled: true, Cleanup: "3d"}}, "darkmem",
			model.ArtifactRepoRequest{Enabled: &off, Cleanup: &none}, nil, "darkmem"},
		{"off for a watched repository", map[string]config.ArtifactRepo{"homelab": {Enabled: true, Cleanup: "3d"}}, "homelab", model.ArtifactRepoRequest{Enabled: &off},
			map[string]config.ArtifactRepo{"homelab": {Cleanup: "3d"}}, "homelab"},
		{"off for a leftover entry of a repository ghr does not cover", map[string]config.ArtifactRepo{"oldrepo": {Enabled: true, Cleanup: "3d"}}, "OldRepo",
			model.ArtifactRepoRequest{Enabled: &off}, map[string]config.ArtifactRepo{"oldrepo": {Cleanup: "3d"}}, "oldrepo"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, _, gh, f := newASBackend(t)
			withArtifactStore(t, b, tc.start)
			f.snap = withJobKey()
			gh.userRepos = darkraiseRepos(map[string]bool{"darkmem": true, "homelab": true})
			row, err := b.SetArtifactRepo(context.Background(), tc.repo, tc.req)
			if err != nil || row.Name != tc.row || row.State != "row" {
				t.Fatalf("row %+v, %v", row, err)
			}
			if got := b.Store.Config().ArtifactStore.Repos; !maps.Equal(got, tc.want) {
				t.Fatalf("repos %v, want %v", got, tc.want)
			}
			if got := f.recorded(); !slices.Equal(got, []string{"policy", "row " + tc.row}) {
				t.Fatalf("calls %q", got)
			}
			if len(gh.deleted) != 0 {
				t.Fatalf("runner registrations deleted: %v", gh.deleted)
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
		}, "darkmem", model.ArtifactRepoRequest{Enabled: &on}, 404,
			"darkmem is not a covered repository; add it as a runner or watched repository first"},
		{"a repository being removed that keeps an entry", func(t *testing.T, b *Backend, _ *fakeManager, _ *fakeGH, _ *fakeAS) {
			update(t, b, func(c *config.Config) {
				r := c.Repo("darkmem")
				r.Paused, r.Removing = true, true
				c.ArtifactStore.Repos = map[string]config.ArtifactRepo{"darkmem": {Cleanup: "3d"}}
			})
		}, "darkmem", model.ArtifactRepoRequest{Enabled: &on}, 409,
			"darkmem is not served by ghr; self-hosted storage serves only repositories ghr runs jobs for"},
		{"nothing to change", nil, "darkmem", model.ArtifactRepoRequest{}, 400, "give enabled or cleanup"},
		{"a cleanup under an hour", nil, "darkmem", model.ArtifactRepoRequest{Cleanup: &tenMinutes}, 400, "cleanup must be at least 1h, or off"},
		{"a cleanup that is not a duration", nil, "darkmem", model.ArtifactRepoRequest{Cleanup: &soon}, 400, `cleanup: invalid duration "soon"`},
		{"a watched repository", nil, "homelab", model.ArtifactRepoRequest{Enabled: &on}, 409,
			"homelab is not served by ghr; self-hosted storage serves only repositories ghr runs jobs for"},
		{"a watched repository while GitHub rejects the token", func(_ *testing.T, _ *Backend, m *fakeManager, _ *fakeGH, _ *fakeAS) {
			m.degraded = "Bad credentials"
		}, "homelab", model.ArtifactRepoRequest{Enabled: &on}, 409,
			"homelab is not served by ghr; self-hosted storage serves only repositories ghr runs jobs for"},
		{"a leftover entry of a repository ghr does not cover", func(t *testing.T, b *Backend, _ *fakeManager, _ *fakeGH, _ *fakeAS) {
			update(t, b, func(c *config.Config) {
				c.ArtifactStore.Repos = map[string]config.ArtifactRepo{"oldrepo": {Cleanup: "3d"}}
			})
		}, "oldrepo", model.ArtifactRepoRequest{Enabled: &on}, 409,
			"oldrepo is not served by ghr; self-hosted storage serves only repositories ghr runs jobs for"},
		{"no job key", func(_ *testing.T, _ *Backend, _ *fakeManager, _ *fakeGH, f *fakeAS) {
			f.snap.JobKey = nil
		}, "darkmem", model.ArtifactRepoRequest{Enabled: &on}, 409, "set the job key first"},
		{"a public repository", nil, "darkcloud", model.ArtifactRepoRequest{Enabled: &on}, 409,
			"darkcloud is public; self-hosted storage serves only private repositories"},
		{"a repository the token cannot see", func(_ *testing.T, _ *Backend, _ *fakeManager, gh *fakeGH, _ *fakeAS) {
			gh.userRepos = darkraiseRepos(map[string]bool{"darkmem": true})
		}, "darkcloud", model.ArtifactRepoRequest{Enabled: &on}, 400,
			"token cannot see darkraise/darkcloud: add it to the PAT's repository access first"},
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
			if apiStatus(err) != tc.status || apiMsg(err) != tc.msg {
				t.Fatalf("status %d message %q, want %d %q", apiStatus(err), apiMsg(err), tc.status, tc.msg)
			}
			if got := b.Store.Config().ArtifactStore.Repos; !maps.Equal(got, before) {
				t.Fatalf("repos changed to %v", got)
			}
			if got := f.recorded(); len(got) != 0 {
				t.Fatalf("calls %q", got)
			}
			if len(gh.deleted) != 0 {
				t.Fatalf("runner registrations deleted: %v", gh.deleted)
			}
		})
	}
}

// A row reads private while its visibility is unknown, so the switch reads
// the visibility at GitHub whatever the row says.
func TestSetArtifactRepoReadsTheVisibilityAtGitHub(t *testing.T) {
	b, _, gh, f := newASBackend(t)
	withArtifactStore(t, b, nil)
	ctx := context.Background()
	on := true
	f.snap = withJobKey()
	f.snap.Repos = []model.ArtifactStoreRepo{{Name: "darkmem", Private: true}, {Name: "darkcloud", Private: true}}
	gh.userRepos = darkraiseRepos(map[string]bool{"darkmem": false})
	_, err := b.SetArtifactRepo(ctx, "darkmem", model.ArtifactRepoRequest{Enabled: &on})
	if apiStatus(err) != 409 || apiMsg(err) != "darkmem is public; self-hosted storage serves only private repositories" {
		t.Fatalf("a public repository on a private row: status %d message %q", apiStatus(err), apiMsg(err))
	}
	_, err = b.SetArtifactRepo(ctx, "darkcloud", model.ArtifactRepoRequest{Enabled: &on})
	if apiStatus(err) != 400 || apiMsg(err) != "token cannot see darkraise/darkcloud: add it to the PAT's repository access first" {
		t.Fatalf("a repository the token cannot see: status %d message %q", apiStatus(err), apiMsg(err))
	}
	gh.userRepos, gh.repoErr = nil, errors.New("connection reset")
	if _, err := b.SetArtifactRepo(ctx, "darkcloud", model.ArtifactRepoRequest{Enabled: &on}); err == nil || err.Error() != "connection reset" {
		t.Fatalf("a failed visibility read: %v", err)
	}
	if a := b.Store.Config().ArtifactStore; a.Repos != nil {
		t.Fatalf("repos %v, want neither switched on", a.Repos)
	}
}

// Switching off and cleanup changes need no GitHub: they work while GitHub
// rejects the token and never read the visibility. A leftover entry that
// switching off drops answers a not_served row.
func TestSetArtifactRepoOffNeedsNoGitHub(t *testing.T) {
	b, m, gh, f := newASBackend(t)
	withArtifactStore(t, b, map[string]config.ArtifactRepo{"darkmem": {Enabled: true}, "oldrepo": {Enabled: true}})
	m.degraded = "Bad credentials"
	gh.repoErr = errors.New("ListUserRepos must not be called")
	ctx := context.Background()
	off, threeDays := false, "3d"
	if _, err := b.SetArtifactRepo(ctx, "darkmem", model.ArtifactRepoRequest{Enabled: &off}); err != nil {
		t.Fatalf("switch-off: %v", err)
	}
	if _, err := b.SetArtifactRepo(ctx, "homelab", model.ArtifactRepoRequest{Cleanup: &threeDays}); err != nil {
		t.Fatalf("cleanup change: %v", err)
	}
	f.noRow = true
	row, err := b.SetArtifactRepo(ctx, "oldrepo", model.ArtifactRepoRequest{Enabled: &off})
	if err != nil || row.Name != "oldrepo" || row.State != "not_served" || row.Items == nil || !row.Private {
		t.Fatalf("leftover entry: row %+v, %v", row, err)
	}
	want := map[string]config.ArtifactRepo{"homelab": {Cleanup: "3d"}}
	if got := b.Store.Config().ArtifactStore.Repos; !maps.Equal(got, want) {
		t.Fatalf("repos %v, want %v", got, want)
	}
	if got := f.recorded(); !slices.Equal(got, []string{"policy", "row darkmem", "policy", "row homelab", "policy", "row oldrepo"}) {
		t.Fatalf("calls %q", got)
	}
}

// The bulk switch turns on the private repositories ghr serves and leaves
// watched-only, public and departing ones alone.
func TestEnablePrivateArtifactRepos(t *testing.T) {
	b, _, gh, f := newASBackend(t)
	withArtifactStore(t, b, map[string]config.ArtifactRepo{"darkmem": {Cleanup: "3d"}})
	f.snap = withJobKey()
	gh.userRepos = append(darkraiseRepos(map[string]bool{"darkmem": true, "darkcloud": false, "homelab": true}),
		github.UserRepo{Name: "darkcloud", Private: true, Owner: github.Account{Login: "someone"}})
	rows, err := b.EnablePrivateArtifactRepos(context.Background())
	if err != nil || len(rows) != 1 || rows[0].Name != "darkmem" || rows[0].State != "row" {
		t.Fatalf("rows %+v, %v", rows, err)
	}
	want := map[string]config.ArtifactRepo{"darkmem": {Enabled: true, Cleanup: "3d"}}
	if got := b.Store.Config().ArtifactStore.Repos; !maps.Equal(got, want) {
		t.Fatalf("repos %v", got)
	}
	if got := f.recorded(); !slices.Equal(got, []string{"policy", "row darkmem"}) {
		t.Fatalf("calls %q", got)
	}
	if len(gh.deleted) != 0 {
		t.Fatalf("runner registrations deleted: %v", gh.deleted)
	}
}

func TestEnablePrivateArtifactReposRefusals(t *testing.T) {
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
	t.Run("no private repository ghr serves", func(t *testing.T) {
		b, _, gh, f := newASBackend(t)
		withArtifactStore(t, b, nil)
		f.snap = withJobKey()
		gh.userRepos = darkraiseRepos(map[string]bool{"darkmem": false, "homelab": true})
		rows, err := b.EnablePrivateArtifactRepos(ctx)
		if err != nil || rows == nil || len(rows) != 0 {
			t.Fatalf("rows %v, %v", rows, err)
		}
		if got := f.recorded(); len(got) != 0 {
			t.Fatalf("calls %q", got)
		}
	})
	t.Run("a repository being removed", func(t *testing.T) {
		b, _, gh, f := newASBackend(t)
		withArtifactStore(t, b, nil)
		if _, err := b.Store.Update(func(c *config.Config) error {
			r := c.Repo("darkcloud")
			r.Paused, r.Removing = true, true
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		f.snap = withJobKey()
		gh.userRepos = darkraiseRepos(map[string]bool{"darkmem": true, "darkcloud": true})
		rows, err := b.EnablePrivateArtifactRepos(ctx)
		if err != nil || len(rows) != 1 || rows[0].Name != "darkmem" {
			t.Fatalf("rows %+v, %v", rows, err)
		}
		if a := b.Store.Config().ArtifactStore; !a.Enabled("darkmem") || a.Enabled("darkcloud") {
			t.Fatalf("repos %v", a.Repos)
		}
	})
	t.Run("GitHub rejecting the token", func(t *testing.T) {
		b, m, _, f := newASBackend(t)
		withArtifactStore(t, b, nil)
		f.snap = withJobKey()
		m.degraded = "Bad credentials"
		if _, err := b.EnablePrivateArtifactRepos(ctx); apiStatus(err) != 503 {
			t.Fatalf("%v", err)
		}
		if got := f.recorded(); len(got) != 0 {
			t.Fatalf("calls %q", got)
		}
	})
}

// retireProbe records, for each Retire, whether config.yaml still covered
// the repository and whether the call had a deadline of at most
// artifactRetireTimeout.
type retireProbe struct {
	*fakeAS
	b         *Backend
	coveredAt []bool
	bounded   []bool
}

func (p *retireProbe) Retire(ctx context.Context, repo string) error {
	_, covered := artifactCovered(p.b.Store.Config(), repo)
	p.coveredAt = append(p.coveredAt, covered)
	dl, ok := ctx.Deadline()
	p.bounded = append(p.bounded, ok && time.Until(dl) <= artifactRetireTimeout)
	return p.fakeAS.Retire(ctx, repo)
}

func probeRetire(b *Backend, f *fakeAS) *retireProbe {
	p := &retireProbe{fakeAS: f, b: b}
	b.AS = p
	return p
}

// Removing or unwatching a repository retires its old variable and secret
// once, while it is still covered, whatever its switch says; every way out
// of coverage drops its artifact_store entry.
func TestLeavingCoverageRetiresTheOldVariable(t *testing.T) {
	b, _, _, f := newASBackend(t)
	p := probeRetire(b, f)
	withArtifactStore(t, b, map[string]config.ArtifactRepo{
		"darkmem": {Enabled: true}, "darkcloud": {Cleanup: "3d"}, "homelab": {Enabled: true},
	})
	if err := b.UnwatchRepo("darkmem"); apiStatus(err) != 404 {
		t.Fatalf("unwatching a runner repository: %v", err)
	}
	if err := b.RemoveRepo("nope"); apiStatus(err) != 404 {
		t.Fatalf("removing an unknown repository: %v", err)
	}
	for _, name := range []string{"darkmem", "darkcloud"} {
		if err := b.RemoveRepo(name); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.UnwatchRepo("homelab"); err != nil {
		t.Fatal(err)
	}
	cfg := b.Store.Config()
	if cfg.ArtifactStore.Repos != nil || !cfg.Repo("darkmem").Removing || !cfg.Repo("darkcloud").Removing || cfg.Watched("homelab") {
		t.Fatalf("repos %v, darkmem %+v, darkcloud %+v, watched %v", cfg.ArtifactStore.Repos, cfg.Repo("darkmem"), cfg.Repo("darkcloud"), cfg.WatchRepos)
	}
	if got := f.recorded(); !slices.Equal(got, []string{"retire darkmem", "retire darkcloud", "retire homelab"}) {
		t.Fatalf("calls %q", got)
	}
	if !slices.Equal(p.coveredAt, []bool{true, true, true}) {
		t.Fatalf("covered in config.yaml during Retire: %v", p.coveredAt)
	}
	if !slices.Equal(p.bounded, []bool{true, true, true}) {
		t.Fatalf("Retire deadlines within %s: %v", artifactRetireTimeout, p.bounded)
	}
}

// A retire GitHub refuses, Variables or Secrets alike, never stops a
// removal or keeps a repository watched, nor does GitHub rejecting the
// token; the backend writes no event of its own about it.
func TestLeavingCoverageIgnoresAFailedRetire(t *testing.T) {
	b, m, _, f := newASBackend(t)
	withArtifactStore(t, b, map[string]config.ArtifactRepo{"darkmem": {Enabled: true}, "homelab": {Enabled: true}})
	f.retireErr = &github.APIError{Status: 403, Kind: github.ErrAuth, Message: "Resource not accessible by personal access token", Permissions: "variables=read"}
	if err := b.RemoveRepo("darkmem"); err != nil {
		t.Fatalf("a refused variable read stopped the removal: %v", err)
	}
	f.retireErr = &github.APIError{Status: 403, Kind: github.ErrAuth, Message: "Resource not accessible by personal access token", Permissions: "secrets=write"}
	m.degraded = "Bad credentials"
	if err := b.UnwatchRepo("homelab"); err != nil {
		t.Fatalf("a refused secret delete kept the repository watched: %v", err)
	}
	cfg := b.Store.Config()
	if cfg.ArtifactStore.Repos != nil || !cfg.Repo("darkmem").Removing || cfg.Watched("homelab") {
		t.Fatalf("repos %v, darkmem %+v, watched %v", cfg.ArtifactStore.Repos, cfg.Repo("darkmem"), cfg.WatchRepos)
	}
	if got := f.recorded(); !slices.Equal(got, []string{"retire darkmem", "retire homelab"}) {
		t.Fatalf("calls %q", got)
	}
	var msgs []string
	for _, e := range b.Events.After(0) {
		msgs = append(msgs, e.Msg)
	}
	if want := []string{"repo removal requested; waiting for its runners to finish", "stopped watching"}; !slices.Equal(msgs, want) {
		t.Fatalf("events %q, want %q", msgs, want)
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
