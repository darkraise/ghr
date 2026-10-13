package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type PullRequest struct {
	Number int `json:"number"`
}

type Artifact struct {
	ID          int64       `json:"id"`
	Name        string      `json:"name"`
	SizeInBytes int64       `json:"size_in_bytes"`
	Expired     bool        `json:"expired"`
	ExpiresAt   *time.Time  `json:"expires_at"`
	CreatedAt   time.Time   `json:"created_at"`
	WorkflowRun ArtifactRun `json:"workflow_run"`
}

// ArtifactRun is the run that uploaded an artifact.
type ArtifactRun struct {
	ID         int64  `json:"id"`
	HeadBranch string `json:"head_branch"`
}

func (c *Client) getJSON(ctx context.Context, u string, v any) error {
	data, _, err := c.do(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func (c *Client) GetRun(ctx context.Context, repo string, id int64) (*Run, error) {
	var r Run
	if err := c.getJSON(ctx, c.repoURL(repo, fmt.Sprintf("/actions/runs/%d", id)), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *Client) GetRunAttempt(ctx context.Context, repo string, id int64, attempt int) (*Run, error) {
	var r Run
	if err := c.getJSON(ctx, c.repoURL(repo, fmt.Sprintf("/actions/runs/%d/attempts/%d", id, attempt)), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ListAttemptJobs lists every job of one attempt. A re-run attempt lists the
// jobs it did not re-run too, under new IDs.
func (c *Client) ListAttemptJobs(ctx context.Context, repo string, id int64, attempt int) ([]Job, error) {
	var out []Job
	err := c.getAll(ctx, c.repoURL(repo, fmt.Sprintf("/actions/runs/%d/attempts/%d/jobs?per_page=100", id, attempt)), func(b []byte) error {
		var page struct {
			Jobs []Job `json:"jobs"`
		}
		if err := json.Unmarshal(b, &page); err != nil {
			return err
		}
		out = append(out, page.Jobs...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetJob(ctx context.Context, repo string, jobID int64) (*Job, error) {
	var j Job
	if err := c.getJSON(ctx, c.repoURL(repo, fmt.Sprintf("/actions/jobs/%d", jobID)), &j); err != nil {
		return nil, err
	}
	return &j, nil
}

func (c *Client) ListArtifacts(ctx context.Context, repo string, runID int64) ([]Artifact, error) {
	var out []Artifact
	err := c.getAll(ctx, c.repoURL(repo, fmt.Sprintf("/actions/runs/%d/artifacts?per_page=100", runID)), func(b []byte) error {
		var page struct {
			Artifacts []Artifact `json:"artifacts"`
		}
		if err := json.Unmarshal(b, &page); err != nil {
			return err
		}
		out = append(out, page.Artifacts...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ErrBadContent is a file GitHub answered for but did not deliver in a form
// that can be decoded. Asking again gives the same answer.
var ErrBadContent = errors.New("github: file content cannot be decoded")

// GetContent reads one file at ref. GitHub answers base64 with line breaks;
// a file over 1 MB comes back with encoding "none". That, and base64 that
// does not decode, is an ErrBadContent.
func (c *Client) GetContent(ctx context.Context, repo, path, ref string) ([]byte, error) {
	segs := strings.Split(path, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	u := c.repoURL(repo, "/contents/"+strings.Join(segs, "/")+"?ref="+url.QueryEscape(ref))
	var body struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := c.getJSON(ctx, u, &body); err != nil {
		return nil, err
	}
	if body.Encoding != "base64" {
		return nil, fmt.Errorf("%w: %s has encoding %q, not base64", ErrBadContent, path, body.Encoding)
	}
	data, err := base64.StdEncoding.DecodeString(strings.NewReplacer("\n", "", "\r", "").Replace(body.Content))
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrBadContent, path, err)
	}
	return data, nil
}
