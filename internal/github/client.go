// Package github is a minimal GitHub REST client for runner management.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ErrKind int

const (
	ErrOther ErrKind = iota
	ErrAuth
	ErrRateLimit
	ErrNotFound
	ErrUnprocessable
	ErrServer
)

type APIError struct {
	Status  int
	Kind    ErrKind
	Message string
	RetryAt time.Time // set for ErrRateLimit
	// Method and Path name the failed call; Permissions is GitHub's
	// x-accepted-github-permissions header (such as "actions=read"), when sent.
	Method, Path, Permissions string
}

func (e *APIError) Error() string { return fmt.Sprintf("github: %d %s", e.Status, e.Message) }

// Run is a workflow run. The tick and the label check read only ID; the
// Actions page reads the rest. GitHub may leave run_started_at out.
type Run struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	DisplayTitle string     `json:"display_title"`
	RunNumber    int64      `json:"run_number"`
	HeadBranch   string     `json:"head_branch"`
	Event        string     `json:"event"`
	Actor        Account    `json:"actor"`
	Status       string     `json:"status"`
	Conclusion   string     `json:"conclusion"`
	CreatedAt    time.Time  `json:"created_at"`
	RunStartedAt *time.Time `json:"run_started_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	HTMLURL      string     `json:"html_url"`
}

type Step struct {
	Number      int        `json:"number"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	Conclusion  string     `json:"conclusion"`
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

type Job struct {
	ID           int64      `json:"id"`
	RunID        int64      `json:"run_id"`
	Name         string     `json:"name"`
	WorkflowName string     `json:"workflow_name"`
	Status       string     `json:"status"`
	Conclusion   string     `json:"conclusion"`
	Labels       []string   `json:"labels"`
	RunnerName   string     `json:"runner_name"`
	CreatedAt    time.Time  `json:"created_at"`
	StartedAt    *time.Time `json:"started_at"`
	CompletedAt  *time.Time `json:"completed_at"`
	HTMLURL      string     `json:"html_url"`
	Steps        []Step     `json:"steps"`
}

type Label struct {
	Name string `json:"name"`
}

type Runner struct {
	ID     int64   `json:"id"`
	Name   string  `json:"name"`
	Status string  `json:"status"` // online | offline
	Busy   bool    `json:"busy"`
	Labels []Label `json:"labels"`
}

type Repository struct {
	FullName string `json:"full_name"`
	Private  bool   `json:"private"`
}

type JITConfig struct {
	Runner           Runner `json:"runner"`
	EncodedJITConfig string `json:"encoded_jit_config"`
}

// TokenMeta is what GitHub's responses said about the current token. Each
// pointer is nil until a response carried that header.
type TokenMeta struct {
	Generation    uint64
	CheckedAt     time.Time
	OK            bool
	RateRemaining *int
	RateLimit     *int
	RateReset     *time.Time
	ExpiresAt     *time.Time
}

const maxCacheEntries = 1000

type cached struct {
	etag string
	body []byte
	next string // a 304 may omit Link, so the page's next link is cached with its body
}

type Client struct {
	BaseURL string // https://api.github.com
	Owner   string
	Token   func() string
	HTTP    *http.Client
	Now     func() time.Time
	Sleep   func(time.Duration)

	mu         sync.Mutex
	cache      map[string]cached
	remaining  int
	retryAt    time.Time
	gen        uint64 // bumped when the token changes; older responses are not recorded
	meta       TokenMeta
	mutMu      sync.Mutex
	lastMutate time.Time
}

func New(owner string, token func() string) *Client {
	return &Client{
		BaseURL: "https://api.github.com", Owner: owner, Token: token,
		HTTP: &http.Client{Timeout: 30 * time.Second}, Now: time.Now, Sleep: time.Sleep,
		remaining: -1,
	}
}

// RateRemaining is the last seen x-ratelimit-remaining, or -1 before any request.
func (c *Client) RateRemaining() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.remaining
}

// ForgetCache drops ETag state, any rate-limit suspension and the token
// metadata, used after the token changes.
func (c *Client) ForgetCache() {
	c.mu.Lock()
	c.cache = nil
	c.retryAt = time.Time{}
	c.gen++
	c.meta = TokenMeta{}
	c.remaining = -1
	c.mu.Unlock()
}

// TokenMeta returns the metadata recorded for the current token.
func (c *Client) TokenMeta() TokenMeta {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := c.meta
	m.Generation = c.gen
	return m
}

// record stores the response's rate and expiry headers, unless the token
// changed while the request was in flight. Only a success or an
// authentication or permission failure (ErrAuth) is a verdict on the token;
// other failures leave CheckedAt and OK as they were.
func (c *Client) record(gen uint64, h http.Header, apiErr *APIError) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen != c.gen {
		return
	}
	if n, err := strconv.Atoi(h.Get("X-RateLimit-Remaining")); err == nil {
		c.remaining = n
		c.meta.RateRemaining = &n
	}
	if n, err := strconv.Atoi(h.Get("X-RateLimit-Limit")); err == nil {
		c.meta.RateLimit = &n
	}
	if n, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		t := time.Unix(n, 0)
		c.meta.RateReset = &t
	}
	if t, ok := parseExpiry(h.Get("GitHub-Authentication-Token-Expiration")); ok {
		c.meta.ExpiresAt = &t
	}
	switch {
	case apiErr == nil:
		c.meta.CheckedAt, c.meta.OK = c.Now(), true
	case apiErr.Kind == ErrAuth:
		c.meta.CheckedAt, c.meta.OK = c.Now(), false
	}
}

// parseExpiry reads GitHub's token expiry header, "2026-12-31 23:59:59 UTC".
func parseExpiry(v string) (time.Time, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{"2006-01-02 15:04:05 MST", "2006-01-02 15:04:05 -0700", time.RFC3339} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// SuspendedUntil is the time before which every request fails fast with
// ErrRateLimit, or the zero time when requests are allowed.
func (c *Client) SuspendedUntil() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Now().Before(c.retryAt) {
		return c.retryAt
	}
	return time.Time{}
}

func (c *Client) repoURL(repo, rest string) string {
	return fmt.Sprintf("%s/repos/%s/%s%s", c.BaseURL, url.PathEscape(c.Owner), url.PathEscape(repo), rest)
}

var nextLinkRe = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// do performs one request; GETs use ETag revalidation. It returns body and the next-page URL.
func (c *Client) do(ctx context.Context, method, u string, body any) ([]byte, string, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, "", err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	// SetToken and Reload store the new token before ForgetCache bumps the
	// generation, so reading the generation first means a request may carry
	// a newer token than its generation (its response is then dropped) but
	// never an older one.
	c.mu.Lock()
	gen := c.gen
	c.mu.Unlock()
	req.Header.Set("Authorization", "Bearer "+c.Token())
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.mu.Lock()
	prev, hasPrev := c.cache[u]
	retryAt := c.retryAt
	c.mu.Unlock()
	if c.Now().Before(retryAt) {
		return nil, "", &APIError{Status: http.StatusTooManyRequests, Kind: ErrRateLimit, Message: "rate limited until " + retryAt.Format(time.RFC3339), RetryAt: retryAt}
	}
	if method == http.MethodGet && hasPrev {
		req.Header.Set("If-None-Match", prev.etag)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	var apiErr *APIError
	if resp.StatusCode >= 300 && !(resp.StatusCode == http.StatusNotModified && hasPrev) {
		apiErr = c.classify(resp, data)
	}
	c.record(gen, resp.Header, apiErr)
	next := ""
	if m := nextLinkRe.FindStringSubmatch(resp.Header.Get("Link")); m != nil {
		next = m[1]
	}
	if resp.StatusCode == http.StatusNotModified && hasPrev {
		return prev.body, prev.next, nil
	}
	if apiErr != nil {
		err := apiErr
		if err.Kind == ErrRateLimit {
			c.mu.Lock()
			if gen == c.gen && err.RetryAt.After(c.retryAt) {
				c.retryAt = err.RetryAt
			}
			c.mu.Unlock()
		}
		return nil, "", err
	}
	if method == http.MethodGet {
		if etag := resp.Header.Get("ETag"); etag != "" {
			c.mu.Lock()
			if gen == c.gen {
				// Run and job URLs are unbounded over time; reset rather than track LRU.
				if c.cache == nil || len(c.cache) >= maxCacheEntries {
					c.cache = map[string]cached{}
				}
				c.cache[u] = cached{etag: etag, body: data, next: next}
			}
			c.mu.Unlock()
		}
	}
	return data, next, nil
}

func (c *Client) classify(resp *http.Response, data []byte) *APIError {
	var msg struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(data, &msg)
	e := &APIError{Status: resp.StatusCode, Message: msg.Message,
		Method: resp.Request.Method, Path: resp.Request.URL.Path, Permissions: resp.Header.Get("X-Accepted-GitHub-Permissions")}
	switch {
	case resp.StatusCode == 401:
		e.Kind = ErrAuth
	case resp.StatusCode == 403 || resp.StatusCode == 429:
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			e.Kind = ErrRateLimit
			if reset, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
				e.RetryAt = time.Unix(reset, 0)
			} else {
				e.RetryAt = c.Now().Add(time.Minute)
			}
		} else if ra := resp.Header.Get("Retry-After"); ra != "" {
			e.Kind = ErrRateLimit
			secs, err := strconv.Atoi(ra)
			if err != nil || secs < 0 {
				secs = 60
			}
			e.RetryAt = c.Now().Add(time.Duration(secs) * time.Second)
		} else if resp.StatusCode == 429 || strings.Contains(strings.ToLower(msg.Message), "secondary rate limit") {
			e.Kind = ErrRateLimit
			e.RetryAt = c.Now().Add(time.Minute)
		} else {
			e.Kind = ErrAuth
		}
	case resp.StatusCode == 404:
		e.Kind = ErrNotFound
	case resp.StatusCode == 422:
		e.Kind = ErrUnprocessable
	case resp.StatusCode >= 500:
		e.Kind = ErrServer
	}
	return e
}

// mutate serialises state-changing calls and spaces them at least 1s apart.
func (c *Client) mutate(ctx context.Context, method, u string, body any) ([]byte, error) {
	c.mutMu.Lock()
	defer c.mutMu.Unlock()
	if until := c.SuspendedUntil(); !until.IsZero() {
		return nil, &APIError{Status: http.StatusTooManyRequests, Kind: ErrRateLimit, Message: "rate limited until " + until.Format(time.RFC3339), RetryAt: until}
	}
	if wait := c.lastMutate.Add(time.Second).Sub(c.Now()); wait > 0 {
		c.Sleep(wait)
	}
	data, _, err := c.do(ctx, method, u, body)
	c.lastMutate = c.Now()
	return data, err
}

func (c *Client) getAll(ctx context.Context, u string, each func([]byte) error) error {
	for u != "" {
		data, next, err := c.do(ctx, http.MethodGet, u, nil)
		if err != nil {
			return err
		}
		if err := each(data); err != nil {
			return err
		}
		u = next
	}
	return nil
}

// ListRuns lists workflow runs with the given status (queued, in_progress, waiting).
func (c *Client) ListRuns(ctx context.Context, repo, status string) ([]Run, error) {
	var out []Run
	err := c.getAll(ctx, c.repoURL(repo, "/actions/runs?per_page=100&status="+url.QueryEscape(status)), func(b []byte) error {
		var page struct {
			WorkflowRuns []Run `json:"workflow_runs"`
		}
		if err := json.Unmarshal(b, &page); err != nil {
			return err
		}
		out = append(out, page.WorkflowRuns...)
		return nil
	})
	return out, err
}

// ListRecentRuns lists the repo's n most recent workflow runs of any status,
// one page only.
func (c *Client) ListRecentRuns(ctx context.Context, repo string, n int) ([]Run, error) {
	data, _, err := c.do(ctx, http.MethodGet, c.repoURL(repo, fmt.Sprintf("/actions/runs?per_page=%d", n)), nil)
	if err != nil {
		return nil, err
	}
	var page struct {
		WorkflowRuns []Run `json:"workflow_runs"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, err
	}
	return page.WorkflowRuns, nil
}

// ListJobs lists the latest attempt's jobs of a run.
func (c *Client) ListJobs(ctx context.Context, repo string, runID int64) ([]Job, error) {
	var out []Job
	err := c.getAll(ctx, c.repoURL(repo, fmt.Sprintf("/actions/runs/%d/jobs?filter=latest&per_page=100", runID)), func(b []byte) error {
		var page struct {
			Jobs []Job `json:"jobs"`
		}
		if err := json.Unmarshal(b, &page); err != nil {
			return err
		}
		out = append(out, page.Jobs...)
		return nil
	})
	return out, err
}

func (c *Client) ListRunners(ctx context.Context, repo string) ([]Runner, error) {
	var out []Runner
	err := c.getAll(ctx, c.repoURL(repo, "/actions/runners?per_page=100"), func(b []byte) error {
		var page struct {
			Runners []Runner `json:"runners"`
		}
		if err := json.Unmarshal(b, &page); err != nil {
			return err
		}
		out = append(out, page.Runners...)
		return nil
	})
	return out, err
}

func (c *Client) GetRunner(ctx context.Context, repo string, id int64) (*Runner, error) {
	data, _, err := c.do(ctx, http.MethodGet, c.repoURL(repo, fmt.Sprintf("/actions/runners/%d", id)), nil)
	if err != nil {
		return nil, err
	}
	var r Runner
	return &r, json.Unmarshal(data, &r)
}

// DeleteRunner removes a registration; an already-removed runner (404) is success.
func (c *Client) DeleteRunner(ctx context.Context, repo string, id int64) error {
	_, err := c.mutate(ctx, http.MethodDelete, c.repoURL(repo, fmt.Sprintf("/actions/runners/%d", id)), nil)
	if IsKind(err, ErrNotFound) {
		return nil
	}
	return err
}

func (c *Client) GenerateJITConfig(ctx context.Context, repo, name string, labels []string) (*JITConfig, error) {
	body := map[string]any{"name": name, "runner_group_id": 1, "labels": labels, "work_folder": "_work"}
	data, err := c.mutate(ctx, http.MethodPost, c.repoURL(repo, "/actions/runners/generate-jitconfig"), body)
	if err != nil {
		return nil, err
	}
	var j JITConfig
	return &j, json.Unmarshal(data, &j)
}

func (c *Client) GetRepo(ctx context.Context, repo string) (*Repository, error) {
	data, _, err := c.do(ctx, http.MethodGet, c.repoURL(repo, ""), nil)
	if err != nil {
		return nil, err
	}
	var r Repository
	return &r, json.Unmarshal(data, &r)
}

// IsKind reports whether err is an *APIError of the given kind.
func IsKind(err error, k ErrKind) bool {
	var e *APIError
	return errors.As(err, &e) && e.Kind == k
}
