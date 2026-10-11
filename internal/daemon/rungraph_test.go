package daemon

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/github"
)

const ciPath = ".github/workflows/ci.yml"

const ciYAML = `
jobs:
  build:
    name: Build
  test:
    needs: build
`

func ciJobs() []github.Job {
	return []github.Job{{ID: 1, Name: "Build"}, {ID: 2, Name: "test"}}
}

func TestRunGraphNotes(t *testing.T) {
	for _, tc := range []struct {
		name, path, yaml string
		err              error
		jobs             []github.Job
		note             string
	}{
		{"no jobs yet", ciPath, ciYAML, nil, nil, "This run has no jobs yet."},
		{"dynamic workflow", "dynamic/dependabot/dependabot-updates", ciYAML, nil, ciJobs(), "This run has no workflow file to read."},
		{"missing file", ciPath, "", nil, ciJobs(), "This run has no workflow file to read."},
		{"no Contents permission", ciPath, "", &github.APIError{Status: 403, Kind: github.ErrAuth, Permissions: "contents=read"}, ciJobs(),
			"Add Contents: read to the token to see which jobs depend on which."},
		{"job not in the file", ciPath, ciYAML, nil, append(ciJobs(), github.Job{ID: 3, Name: "Deploy"}),
			"The workflow file at this commit does not match the run's jobs."},
		{"invalid YAML", ciPath, "jobs: [", nil, ciJobs(), "ghr could not read .github/workflows/ci.yml."},
		{"undecodable file", ciPath, "", fmt.Errorf("%w: %s has encoding %q, not base64", github.ErrBadContent, ciPath, "none"), ciJobs(),
			"ghr could not read .github/workflows/ci.yml."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, _, gh := newBackend(t)
			gh.contentErr = tc.err
			if tc.yaml != "" {
				gh.content = map[string][]byte{contentKey("darkmem", tc.path, "abc"): []byte(tc.yaml)}
			}
			g, note, err := b.runGraph(context.Background(), "darkmem", "abc", tc.path, tc.jobs)
			if err != nil || g != nil || note != tc.note {
				t.Fatalf("graph %+v note %q err %v", g, note, err)
			}
			if len(tc.jobs) == 0 && gh.count("GetContent") != 0 {
				t.Fatalf("the workflow file was read for a run with no jobs: %v", gh.calls)
			}
		})
	}
}

func TestRunGraphBuildsFromTheFile(t *testing.T) {
	b, _, gh := newBackend(t)
	gh.content = map[string][]byte{contentKey("darkmem", ciPath, "abc"): []byte(ciYAML)}
	g, note, err := b.runGraph(context.Background(), "darkmem", "abc", ciPath, ciJobs())
	if err != nil || note != "" || g == nil || len(g.Nodes) != 2 {
		t.Fatalf("graph %+v note %q err %v", g, note, err)
	}
	if n := g.Nodes[1]; n.Key != "test" || n.Column != 1 || fmt.Sprint(n.Needs) != "[build]" || fmt.Sprint(n.JobIDs) != "[2]" {
		t.Fatalf("node %+v", n)
	}
}

func TestRunGraphFailsOnRateLimit(t *testing.T) {
	b, _, gh := newBackend(t)
	gh.contentErr = &github.APIError{Status: 429, Kind: github.ErrRateLimit}
	if _, _, err := b.runGraph(context.Background(), "darkmem", "abc", ciPath, ciJobs()); !github.IsKind(err, github.ErrRateLimit) {
		t.Fatalf("err %v", err)
	}
}

func TestWorkflowFileCache(t *testing.T) {
	ctx := context.Background()
	t.Run("keeps bytes", func(t *testing.T) {
		b, _, gh := newBackend(t)
		gh.content = map[string][]byte{contentKey("darkmem", ciPath, "abc"): []byte(ciYAML)}
		for range 2 {
			if _, note, _ := b.runGraph(ctx, "darkmem", "abc", ciPath, ciJobs()); note != "" {
				t.Fatalf("note %q", note)
			}
		}
		if n := gh.count("GetContent"); n != 1 {
			t.Fatalf("%d GetContent calls, want 1", n)
		}
	})
	t.Run("keeps not found for 10 minutes", func(t *testing.T) {
		b, _, gh := newBackend(t)
		clock := frozenClock(b, actionsT0)
		b.runGraph(ctx, "darkmem", "abc", ciPath, ciJobs())
		gh.content = map[string][]byte{contentKey("darkmem", ciPath, "abc"): []byte(ciYAML)}
		clock.Store(actionsT0.Add(wfMissingTTL - time.Second).UnixNano())
		if _, note, _ := b.runGraph(ctx, "darkmem", "abc", ciPath, ciJobs()); note != noteNoFile || gh.count("GetContent") != 1 {
			t.Fatalf("note %q, %d calls", note, gh.count("GetContent"))
		}
		clock.Store(actionsT0.Add(wfMissingTTL).UnixNano())
		if _, note, _ := b.runGraph(ctx, "darkmem", "abc", ciPath, ciJobs()); note != "" || gh.count("GetContent") != 2 {
			t.Fatalf("note %q, %d calls", note, gh.count("GetContent"))
		}
	})
	t.Run("keeps an undecodable file for good", func(t *testing.T) {
		b, _, gh := newBackend(t)
		clock := frozenClock(b, actionsT0)
		gh.contentErr = fmt.Errorf("%w: %s has encoding %q, not base64", github.ErrBadContent, ciPath, "none")
		b.runGraph(ctx, "darkmem", "abc", ciPath, ciJobs())
		gh.contentErr = nil
		gh.content = map[string][]byte{contentKey("darkmem", ciPath, "abc"): []byte(ciYAML)}
		clock.Store(actionsT0.Add(24 * time.Hour).UnixNano())
		g, note, err := b.runGraph(ctx, "darkmem", "abc", ciPath, ciJobs())
		if err != nil || g != nil || note != fmt.Sprintf(noteInvalid, ciPath) || gh.count("GetContent") != 1 {
			t.Fatalf("graph %+v note %q err %v, %d calls", g, note, err, gh.count("GetContent"))
		}
	})
	t.Run("never keeps a 403", func(t *testing.T) {
		b, _, gh := newBackend(t)
		gh.contentErr = &github.APIError{Status: 403, Kind: github.ErrAuth}
		b.runGraph(ctx, "darkmem", "abc", ciPath, ciJobs())
		gh.contentErr = nil
		gh.content = map[string][]byte{contentKey("darkmem", ciPath, "abc"): []byte(ciYAML)}
		if g, note, _ := b.runGraph(ctx, "darkmem", "abc", ciPath, ciJobs()); g == nil || note != "" {
			t.Fatalf("graph %+v note %q", g, note)
		}
	})
	t.Run("holds 64 files", func(t *testing.T) {
		b, _, gh := newBackend(t)
		gh.content = map[string][]byte{}
		for i := range wfFileEntries + 1 {
			sha := fmt.Sprintf("sha%d", i)
			gh.content[contentKey("darkmem", ciPath, sha)] = []byte(ciYAML)
			b.runGraph(ctx, "darkmem", sha, ciPath, ciJobs())
		}
		b.runGraph(ctx, "darkmem", "sha0", ciPath, ciJobs())
		if n := gh.count("GetContent"); n != wfFileEntries+2 {
			t.Fatalf("%d GetContent calls, want %d: the oldest file should have been evicted", n, wfFileEntries+2)
		}
	})
}
