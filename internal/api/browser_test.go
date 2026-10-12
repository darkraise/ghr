package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The mark survives contexts derived from it and reaches the backend through
// the request's context; a request without it reads as one over the socket.
func TestBrowserMark(t *testing.T) {
	if host, ok := Browser(context.Background()); ok || host != "" {
		t.Fatalf("an unmarked context reads as %q, %v", host, ok)
	}
	ctx := WithBrowser(context.Background(), "ghr.lan:8080")
	derived, cancel := context.WithCancel(context.WithValue(ctx, ctxMark{}, "other"))
	defer cancel()
	if host, ok := Browser(derived); !ok || host != "ghr.lan:8080" {
		t.Fatalf("derived context: %q, %v", host, ok)
	}
	if host, ok := Browser(WithBrowser(context.Background(), "")); !ok || host != "" {
		t.Fatalf("a request without a Host is still the browser's: %q, %v", host, ok)
	}
	b := &fakeBackend{}
	req := httptest.NewRequest(http.MethodPatch, "/config", strings.NewReader(`{"global_max":3}`)).WithContext(ctx)
	rec := httptest.NewRecorder()
	NewServer(b).ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || len(b.patchCtxs) != 1 {
		t.Fatalf("patch: %d %s, %d calls", rec.Code, rec.Body.String(), len(b.patchCtxs))
	}
	if host, ok := Browser(b.patchCtxs[0]); !ok || host != "ghr.lan:8080" {
		t.Fatalf("the backend saw %q, %v", host, ok)
	}
}
