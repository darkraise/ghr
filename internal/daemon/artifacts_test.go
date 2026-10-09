package daemon

import (
	"context"
	"errors"
	"testing"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/github"
)

func TestDownloadHost(t *testing.T) {
	for u, want := range map[string]bool{
		"https://productionresultssa1.blob.core.windows.net/actions-results/x.zip?sig=1": true,
		"https://pipelines.actions.githubusercontent.com/x":                              true,
		"https://PIPELINES.ACTIONS.GITHUBUSERCONTENT.COM/x":                              true,
		"http://productionresultssa1.blob.core.windows.net/x":                            false,
		"https://blob.core.windows.net.example.com/x":                                    false,
		"https://evilblob.core.windows.net/x":                                            false,
		"https://example.com/x":                                                          false,
		"https://evilgithubusercontent.com/x":                                            false,
		"https://githubusercontent.com/x":                                                false,
		"https://pipelines.actions.githubusercontent.com@example.com/x":                  false,
		"https://pipelines.actions.githubusercontent.com:8443/x":                         true,
		"https://example.com#.githubusercontent.com":                                     false,
		"https://example.com/.githubusercontent.com":                                     false,
		"/relative": false,
		"":          false,
	} {
		if got := downloadHost(u); got != want {
			t.Errorf("%q: got %v", u, got)
		}
	}
}

func TestArtifactDownload(t *testing.T) {
	const signed = "https://productionresultssa1.blob.core.windows.net/actions-results/x.zip?sig=1"
	for _, tc := range []struct {
		name   string
		url    string
		err    error
		status int
		msg    string
	}{
		{"signed address", signed, nil, 0, ""},
		{"expired", "", &github.APIError{Status: 410, Kind: github.ErrGone}, 410, "artifact has expired"},
		{"unknown host", "https://example.com/x.zip", nil, 502, "GitHub returned an unexpected download address"},
		{"not found", "", &github.APIError{Status: 404, Kind: github.ErrNotFound}, 404, "not found on GitHub"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, _, gh := newBackend(t)
			gh.artURL, gh.artErr = tc.url, tc.err
			d, err := b.ArtifactDownload(context.Background(), "DarkMem", 11)
			var ae *api.Error
			if tc.status == 0 {
				if err != nil || d.URL != signed || gh.count("ArtifactURL darkmem 11") != 1 {
					t.Fatalf("url %q err %v calls %v", d.URL, err, gh.calls)
				}
				return
			}
			if !errors.As(err, &ae) || ae.Status != tc.status || ae.Msg != tc.msg || d.URL != "" {
				t.Fatalf("url %q err %#v", d.URL, err)
			}
		})
	}
}

func TestArtifactDownloadRefusals(t *testing.T) {
	b, m, gh := newBackend(t)
	if _, err := b.ArtifactDownload(context.Background(), "nope", 11); apiStatus(err) != 404 {
		t.Fatalf("uncovered: %v", err)
	}
	m.degraded = "401 Bad credentials"
	if _, err := b.ArtifactDownload(context.Background(), "darkmem", 11); apiStatus(err) != 503 || gh.count("ArtifactURL") != 0 {
		t.Fatalf("degraded: %v, calls %v", err, gh.calls)
	}
}
