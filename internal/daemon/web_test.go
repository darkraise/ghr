package daemon

import (
	"bytes"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/webui"
)

func TestResetRouteIsSocketOnly(t *testing.T) {
	b, _, _ := newBackend(t)
	auth := webui.NewAuth(filepath.Join(t.TempDir(), "web-password"), time.Now, rand.Reader)
	id, err := auth.Setup("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	apiHandler := api.NewServer(b)

	web := httptest.NewServer(webui.Handler(auth, apiHandler, fstest.MapFS{}, nil))
	defer web.Close()
	req, _ := http.NewRequest(http.MethodPost, web.URL+"/api/web/reset-password", bytes.NewReader(nil))
	req.Header.Set("X-GHR", "1")
	req.AddCookie(&http.Cookie{Name: "ghr_session", Value: id})
	resp, err := web.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || !auth.Valid(id) {
		t.Fatalf("over TCP: %d, session valid %v", resp.StatusCode, auth.Valid(id))
	}

	sock := httptest.NewServer(socketHandler(apiHandler, auth, b.Events))
	defer sock.Close()
	resp, err = sock.Client().Post(sock.URL+"/web/reset-password", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("over the socket: %d", resp.StatusCode)
	}
	if auth.Valid(id) {
		t.Fatal("the session survived the reset")
	}
	if req, err := auth.SetupRequired(); !req || err != nil {
		t.Fatalf("after reset: required %v err %v", req, err)
	}
	evs := b.Events.After(0)
	if len(evs) == 0 || evs[len(evs)-1].Level != "warn" ||
		!strings.Contains(evs[len(evs)-1].Msg, "web password reset; the next visitor to the web UI sets a new one") {
		t.Fatalf("events %+v", evs)
	}

	resp, err = sock.Client().Get(sock.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the socket no longer serves the API: %d", resp.StatusCode)
	}
}
