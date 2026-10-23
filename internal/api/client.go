package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

const (
	callLimit = 30 * time.Second
	// killLimit is above systemd's default stop timeout of 90 seconds: the
	// daemon answers a kill once the runner's unit has stopped.
	killLimit = 100 * time.Second
)

// NewUnixClient talks to the daemon over its Unix socket. The HTTP client
// has no timeout of its own: every call sets its limit.
func NewUnixClient(socket string) *Client {
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}}
	return &Client{Base: "http://ghr", HTTP: &http.Client{Transport: tr}}
}

func (c *Client) call(ctx context.Context, method, path string, body io.Reader, out any) error {
	return c.callWithin(ctx, callLimit, method, path, body, out)
}

// callWithin gives the daemon limit to answer, response body included.
func (c *Client) callWithin(ctx context.Context, limit time.Duration, method, path string, body io.Reader, out any) error {
	callCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	// early names the reason when the call was cut short rather than failed:
	// the caller's context, returned as it is, or the call's own limit.
	early := func() error {
		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case callCtx.Err() != nil:
			return fmt.Errorf("ghr daemon did not answer within %ds; the request may still be running", int(limit/time.Second))
		}
		return nil
	}
	req, err := http.NewRequestWithContext(callCtx, method, c.Base+path, body)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if e := early(); e != nil {
			return e
		}
		return fmt.Errorf("ghr daemon unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e struct {
			Error   string    `json:"error"`
			RetryAt time.Time `json:"retry_at"`
		}
		derr := json.NewDecoder(resp.Body).Decode(&e)
		if derr != nil {
			// An error body cut short says nothing about the daemon.
			if cut := early(); cut != nil {
				return cut
			}
		}
		// The daemon's handlers always answer JSON; a plain-text 404 or 405
		// is the router of a daemon that predates this route.
		if derr != nil && (resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed) {
			e.Error = "the running ghr daemon does not know " + method + " " + path +
				"; it is older than this ghr, restart it: systemctl restart ghr"
		}
		if e.Error == "" {
			e.Error = resp.Status
		}
		return &Error{Status: resp.StatusCode, Msg: e.Error, RetryAt: e.RetryAt}
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		if e := early(); e != nil {
			return e
		}
		var se *json.SyntaxError
		var te *json.UnmarshalTypeError
		if errors.As(err, &se) || errors.As(err, &te) {
			return err
		}
		return fmt.Errorf("ghr daemon unreachable: %w", err)
	}
	return nil
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

func (c *Client) History(ctx context.Context, repo, conclusion string, since time.Time, limit int) ([]model.HistoryEntry, error) {
	q := url.Values{}
	q.Set("repo", repo)
	q.Set("conclusion", conclusion)
	if !since.IsZero() {
		q.Set("since", since.Format(time.RFC3339Nano))
	}
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
	return c.callWithin(ctx, killLimit, http.MethodDelete, "/runners/"+url.PathEscape(id), nil, nil)
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

// RunJob runs a background task now.
func (c *Client) RunJob(ctx context.Context, name string) error {
	return c.call(ctx, http.MethodPost, "/jobs/"+url.PathEscape(name)+"/run", nil, nil)
}

// StartGhrUpdate starts installing the newest ghr release.
func (c *Client) StartGhrUpdate(ctx context.Context) error {
	return c.call(ctx, http.MethodPost, "/ghr-update", nil, nil)
}

// CancelGhrUpdate stops a ghr install before it swaps the files.
func (c *Client) CancelGhrUpdate(ctx context.Context) error {
	return c.call(ctx, http.MethodDelete, "/ghr-update", nil, nil)
}

// AvailableRepos lists the owner's repositories the token can access.
func (c *Client) AvailableRepos(ctx context.Context) ([]model.AvailableRepo, error) {
	var rs []model.AvailableRepo
	err := c.call(ctx, http.MethodGet, "/repos/available", nil, &rs)
	return rs, err
}

// Storage returns the daemon's storage snapshot; it never measures.
func (c *Client) Storage(ctx context.Context) (model.Storage, error) {
	var s model.Storage
	err := c.call(ctx, http.MethodGet, "/storage", nil, &s)
	return s, err
}

// RefreshStorage asks the daemon to measure now.
func (c *Client) RefreshStorage(ctx context.Context) error {
	return c.call(ctx, http.MethodPost, "/storage/refresh", nil, nil)
}

// AvailableToolchains lists what an install of tool can ask for.
func (c *Client) AvailableToolchains(ctx context.Context, tool string) ([]model.ToolchainChoice, error) {
	var cs []model.ToolchainChoice
	err := c.call(ctx, http.MethodGet, "/toolchains/available?tool="+url.QueryEscape(tool), nil, &cs)
	return cs, err
}

// InstallToolchain queues an install; version is resolved when it runs.
func (c *Client) InstallToolchain(ctx context.Context, tool, version string) error {
	return c.call(ctx, http.MethodPost, "/toolchains", jsonBody(model.InstallRequest{Tool: tool, Version: version}), nil)
}

// InstallPreset queues every install of a preset.
func (c *Client) InstallPreset(ctx context.Context, preset string) error {
	return c.call(ctx, http.MethodPost, "/toolchains", jsonBody(model.InstallRequest{Preset: preset}), nil)
}

// RemoveToolchain queues the removal of an installed version.
func (c *Client) RemoveToolchain(ctx context.Context, tool, version string) error {
	return c.call(ctx, http.MethodDelete, "/toolchains/"+url.PathEscape(tool)+"/"+url.PathEscape(version), nil, nil)
}

// ClearCache queues clearing a package cache.
func (c *Client) ClearCache(ctx context.Context, name string) error {
	return c.call(ctx, http.MethodPost, "/caches/"+url.PathEscape(name)+"/clear", nil, nil)
}

// PruneScope starts a manual prune of one scope; its outcome arrives as events.
func (c *Client) PruneScope(ctx context.Context, scope string) error {
	return c.call(ctx, http.MethodPost, "/prune/"+url.PathEscape(scope), nil, nil)
}

// Packages lists the managed apt packages, setup.sh's base packages and the
// installed packages ghr can adopt.
func (c *Client) Packages(ctx context.Context) (model.Packages, error) {
	var p model.Packages
	err := c.call(ctx, http.MethodGet, "/packages", nil, &p)
	return p, err
}

// SearchPackages searches the host's apt sources by package name.
func (c *Client) SearchPackages(ctx context.Context, term string) ([]model.PackageInfo, error) {
	var ps []model.PackageInfo
	err := c.call(ctx, http.MethodGet, "/packages/search?q="+url.QueryEscape(term), nil, &ps)
	return ps, err
}

// AddPackages queues an adopt for each installed name and an install for
// each other one.
func (c *Client) AddPackages(ctx context.Context, names []string) (model.AddPackagesResult, error) {
	var r model.AddPackagesResult
	err := c.call(ctx, http.MethodPost, "/packages", jsonBody(model.AddPackagesRequest{Names: names}), &r)
	return r, err
}

// UpgradePackages queues an upgrade of names, or of every managed package
// when all is set.
func (c *Client) UpgradePackages(ctx context.Context, names []string, all bool) error {
	return c.call(ctx, http.MethodPost, "/packages/upgrade", jsonBody(model.UpgradePackagesRequest{Names: names, All: all}), nil)
}

// UninstallPackage queues the removal of a managed package from the host.
func (c *Client) UninstallPackage(ctx context.Context, name string) error {
	return c.call(ctx, http.MethodDelete, "/packages/"+url.PathEscape(name)+"?uninstall=1", nil, nil)
}

// ForgetPackage drops a package from the managed list and leaves it installed.
func (c *Client) ForgetPackage(ctx context.Context, name string) error {
	return c.call(ctx, http.MethodDelete, "/packages/"+url.PathEscape(name), nil, nil)
}

// RefreshPackageIndex queues an apt index refresh.
func (c *Client) RefreshPackageIndex(ctx context.Context) error {
	return c.call(ctx, http.MethodPost, "/packages/refresh", nil, nil)
}

// ArtifactStore returns the self-hosted artifact storage snapshot.
func (c *Client) ArtifactStore(ctx context.Context) (model.ArtifactStore, error) {
	var s model.ArtifactStore
	err := c.call(ctx, http.MethodGet, "/artifact-store", nil, &s)
	return s, err
}

// SetArtifactKey replaces the admin or the job key of self-hosted artifact
// storage.
func (c *Client) SetArtifactKey(ctx context.Context, req model.ArtifactKeyRequest) error {
	return c.call(ctx, http.MethodPut, "/artifact-store/keys", jsonBody(req), nil)
}

// ResetWebPassword forgets the web UI password and ends its sessions. The
// route exists on the Unix socket only.
func (c *Client) ResetWebPassword(ctx context.Context) error {
	return c.call(ctx, http.MethodPost, "/web/reset-password", nil, nil)
}

// SetWebPassword replaces the web UI password and ends its sessions. The
// route exists on the Unix socket only.
func (c *Client) SetWebPassword(ctx context.Context, password string) error {
	return c.call(ctx, http.MethodPost, "/web/set-password", jsonBody(map[string]string{"password": password}), nil)
}

// SetupCode returns the code the web UI asks for before its first password is
// set. The route exists on the Unix socket only.
func (c *Client) SetupCode(ctx context.Context) (string, error) {
	var r struct {
		Code string `json:"code"`
	}
	err := c.call(ctx, http.MethodGet, "/web/setup-code", nil, &r)
	return r.Code, err
}

// SetupState reports how far first-run setup has got.
func (c *Client) SetupState(ctx context.Context) (model.SetupState, error) {
	var s model.SetupState
	err := c.call(ctx, http.MethodGet, "/setup", nil, &s)
	return s, err
}

// SetupGitHub saves the GitHub owner and token of an unconfigured daemon; an
// empty owner keeps the one config.yaml names.
func (c *Client) SetupGitHub(ctx context.Context, owner, token string) error {
	return c.call(ctx, http.MethodPost, "/setup/github", jsonBody(model.SetupGitHubRequest{Owner: owner, Token: token}), nil)
}

// SetupFinish ends first-run setup; toolchains is "popular" or "none".
func (c *Client) SetupFinish(ctx context.Context, toolchains string) error {
	return c.call(ctx, http.MethodPost, "/setup/finish", jsonBody(model.SetupFinishRequest{Toolchains: toolchains}), nil)
}
