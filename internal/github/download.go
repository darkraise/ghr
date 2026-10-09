package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrTooLarge = errors.New("github: download is larger than the limit")

func newBlobClient() *http.Client {
	return &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ForceAttemptHTTP2:     true,
	}}
}

// request performs one API call that may answer with a redirect, which it
// returns instead of following. The response comes back unread and is never
// kept in the ETag cache; statuses of 400 and above become an *APIError.
func (c *Client) request(ctx context.Context, method, u string) (*http.Response, error) {
	req, gen, err := c.newRequest(ctx, method, u, nil)
	if err != nil {
		return nil, err
	}
	if err := c.suspendedErr(); err != nil {
		return nil, err
	}
	hc := *c.HTTP
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	var apiErr *APIError
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		apiErr = c.classify(resp, data)
	}
	c.record(gen, resp.Header, apiErr)
	if apiErr != nil {
		c.suspend(gen, apiErr)
		return nil, apiErr
	}
	return resp, nil
}

// redirectURL returns where GitHub redirects a download endpoint to.
func (c *Client) redirectURL(ctx context.Context, u string) (string, error) {
	resp, err := c.request(ctx, http.MethodGet, u)
	if err != nil {
		return "", err
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusFound || loc == "" {
		return "", fmt.Errorf("github: %s answered %d without a download address", resp.Request.URL.Path, resp.StatusCode)
	}
	return loc, nil
}

// OpenJobLog starts a job log download and returns the body unread with its
// Content-Length, -1 when GitHub sends none. The body stays bound to ctx; the
// caller closes it.
func (c *Client) OpenJobLog(ctx context.Context, repo string, jobID int64) (io.ReadCloser, int64, error) {
	loc, err := c.redirectURL(ctx, c.repoURL(repo, fmt.Sprintf("/actions/jobs/%d/logs", jobID)))
	if err != nil {
		return nil, 0, hideURL(err, "log")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loc, nil)
	if err != nil {
		return nil, 0, hideURL(err, "log")
	}
	resp, err := c.Blob.Do(req)
	if err != nil {
		return nil, 0, hideURL(err, "log")
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("github: log download answered %d", resp.StatusCode)
	}
	return resp.Body, resp.ContentLength, nil
}

// JobLog downloads a finished job's log, at most max bytes. The signed URL
// GitHub redirects to is fetched without the token.
func (c *Client) JobLog(ctx context.Context, repo string, jobID int64, max int64) ([]byte, error) {
	body, _, err := c.OpenJobLog(ctx, repo, jobID)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, ErrTooLarge
	}
	return data, nil
}

// hideURL drops the address a *url.Error quotes: a signed download URL grants
// access to the log or artifact until it expires. net/http quotes a Location
// it cannot parse inside the inner error too, so that one is replaced whole.
func hideURL(err error, noun string) error {
	var ue *url.Error
	if !errors.As(err, &ue) {
		return err
	}
	if strings.HasPrefix(ue.Err.Error(), "failed to parse Location header") {
		return fmt.Errorf("github: the %s download address is malformed", noun)
	}
	return fmt.Errorf("github: %s download failed: %w", noun, ue.Err)
}

// ArtifactURL returns the signed address of an artifact's zip without
// following it; it expires after about a minute.
func (c *Client) ArtifactURL(ctx context.Context, repo string, id int64) (string, error) {
	loc, err := c.redirectURL(ctx, c.repoURL(repo, fmt.Sprintf("/actions/artifacts/%d/zip", id)))
	if err != nil {
		return "", hideURL(err, "artifact")
	}
	return loc, nil
}
