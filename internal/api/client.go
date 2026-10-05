package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

const DefaultSocket = "/run/ghr/ghr.sock"

type Client struct {
	Base string // "http://ghr" for the socket
	HTTP *http.Client
}

// NewUnixClient talks to the daemon over its Unix socket.
func NewUnixClient(socket string) *Client {
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}}
	return &Client{Base: "http://ghr", HTTP: &http.Client{Transport: tr, Timeout: 30 * time.Second}}
}

func (c *Client) call(ctx context.Context, method, path string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, body)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("ghr daemon unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e struct {
			Error   string    `json:"error"`
			RetryAt time.Time `json:"retry_at"`
		}
		json.NewDecoder(resp.Body).Decode(&e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return &Error{Status: resp.StatusCode, Msg: e.Error, RetryAt: e.RetryAt}
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func jsonBody(v any) io.Reader {
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}

// The getters below call before returning: Go leaves the order of
// `return v, f(&v)` unspecified, so v could be read before f fills it.
func (c *Client) Status(ctx context.Context) (model.Status, error) {
	var s model.Status
	err := c.call(ctx, http.MethodGet, "/status", nil, &s)
	return s, err
}

func (c *Client) Events(ctx context.Context, after int64) ([]model.Event, error) {
	var e []model.Event
	err := c.call(ctx, http.MethodGet, fmt.Sprintf("/events?after=%d", after), nil, &e)
	return e, err
}

func (c *Client) History(ctx context.Context, repo, conclusion string, limit int) ([]model.HistoryEntry, error) {
	q := url.Values{}
	q.Set("repo", repo)
	q.Set("conclusion", conclusion)
	q.Set("limit", fmt.Sprint(limit))
	var h []model.HistoryEntry
	err := c.call(ctx, http.MethodGet, "/history?"+q.Encode(), nil, &h)
	return h, err
}

// Log returns the runner's log after cursor ("" = from the start); pass the
// returned chunk's Next to continue.
func (c *Client) Log(ctx context.Context, id, cursor string) (model.LogChunk, error) {
	var l model.LogChunk
	err := c.call(ctx, http.MethodGet, "/runners/"+url.PathEscape(id)+"/log?cursor="+url.QueryEscape(cursor), nil, &l)
	return l, err
}

func (c *Client) Containers(ctx context.Context, id string) ([]model.Container, error) {
	var cs []model.Container
	err := c.call(ctx, http.MethodGet, "/runners/"+url.PathEscape(id)+"/containers", nil, &cs)
	return cs, err
}

func (c *Client) Steps(ctx context.Context, id string) ([]model.Step, error) {
	var s []model.Step
	err := c.call(ctx, http.MethodGet, "/runners/"+url.PathEscape(id)+"/steps", nil, &s)
	return s, err
}

// Config decodes the daemon's current config into out (normally *config.Config).
func (c *Client) Config(ctx context.Context, out any) error {
	return c.call(ctx, http.MethodGet, "/config", nil, out)
}

func (c *Client) PatchConfig(ctx context.Context, p model.ConfigPatch) error {
	return c.call(ctx, http.MethodPatch, "/config", jsonBody(p), nil)
}

func (c *Client) AddRepo(ctx context.Context, req model.AddRepoRequest) error {
	return c.call(ctx, http.MethodPost, "/repos", jsonBody(req), nil)
}

func (c *Client) RemoveRepo(ctx context.Context, name string) error {
	return c.call(ctx, http.MethodDelete, "/repos/"+url.PathEscape(name), nil, nil)
}

func (c *Client) Pause(ctx context.Context, name string) error {
	return c.call(ctx, http.MethodPost, "/repos/"+url.PathEscape(name)+"/pause", nil, nil)
}

func (c *Client) Resume(ctx context.Context, name string) error {
	return c.call(ctx, http.MethodPost, "/repos/"+url.PathEscape(name)+"/resume", nil, nil)
}

func (c *Client) PauseAll(ctx context.Context) error {
	return c.call(ctx, http.MethodPost, "/pause-all", nil, nil)
}

func (c *Client) ResumeAll(ctx context.Context) error {
	return c.call(ctx, http.MethodPost, "/resume-all", nil, nil)
}

func (c *Client) SetToken(ctx context.Context, token string) error {
	return c.call(ctx, http.MethodPut, "/token", strings.NewReader(token), nil)
}

func (c *Client) Kill(ctx context.Context, id string) error {
	return c.call(ctx, http.MethodDelete, "/runners/"+url.PathEscape(id), nil, nil)
}

// Reload makes the daemon re-read config.yaml and its token; it returns the config warnings.
func (c *Client) Reload(ctx context.Context) ([]string, error) {
	var ws []string
	err := c.call(ctx, http.MethodPost, "/reload", nil, &ws)
	return ws, err
}

// Prune starts a maintenance prune; its outcome arrives as events.
func (c *Client) Prune(ctx context.Context) error {
	return c.call(ctx, http.MethodPost, "/prune", nil, nil)
}

// Metrics returns the daemon's last hour of samples and current host figures.
func (c *Client) Metrics(ctx context.Context) (model.Metrics, error) {
	var m model.Metrics
	err := c.call(ctx, http.MethodGet, "/metrics", nil, &m)
	return m, err
}

// Token reports the daemon's view of its GitHub token.
func (c *Client) Token(ctx context.Context) (model.TokenStatus, error) {
	var ts model.TokenStatus
	err := c.call(ctx, http.MethodGet, "/token", nil, &ts)
	return ts, err
}

// Registrations lists the runners GitHub has registered for repo.
func (c *Client) Registrations(ctx context.Context, repo string) ([]model.Registration, error) {
	var rs []model.Registration
	err := c.call(ctx, http.MethodGet, "/repos/"+url.PathEscape(repo)+"/registrations", nil, &rs)
	return rs, err
}

// DeleteRegistration deletes an offline runner registration outside ghr's namespace.
func (c *Client) DeleteRegistration(ctx context.Context, repo string, id int64) error {
	return c.call(ctx, http.MethodDelete, fmt.Sprintf("/repos/%s/registrations/%d", url.PathEscape(repo), id), nil, nil)
}

// StartLabelCheck asks the daemon to scan repo's recent jobs in the background.
func (c *Client) StartLabelCheck(ctx context.Context, repo string) error {
	return c.call(ctx, http.MethodPost, "/repos/"+url.PathEscape(repo)+"/label-check", nil, nil)
}

// LabelCheck returns repo's last label scan; it never starts one.
func (c *Client) LabelCheck(ctx context.Context, repo string) (model.LabelCheck, error) {
	var lc model.LabelCheck
	err := c.call(ctx, http.MethodGet, "/repos/"+url.PathEscape(repo)+"/label-check", nil, &lc)
	return lc, err
}

// QueueRunnerUpdate asks the daemon to install the latest runner once no job
// is running or queued.
func (c *Client) QueueRunnerUpdate(ctx context.Context) error {
	return c.call(ctx, http.MethodPost, "/runner-update", nil, nil)
}

// CancelRunnerUpdate drops a queued runner update.
func (c *Client) CancelRunnerUpdate(ctx context.Context) error {
	return c.call(ctx, http.MethodDelete, "/runner-update", nil, nil)
}
