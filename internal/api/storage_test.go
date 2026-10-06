package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

func (f *fakeBackend) Storage() model.Storage {
	f.storageCalls = append(f.storageCalls, "storage")
	return f.storage
}
func (f *fakeBackend) RefreshStorage() error {
	f.storageCalls = append(f.storageCalls, "refresh")
	return f.storageErr
}
func (f *fakeBackend) AvailableToolchains(_ context.Context, tool string) ([]model.ToolchainChoice, error) {
	f.storageCalls = append(f.storageCalls, "available "+tool)
	return f.choices, f.storageErr
}
func (f *fakeBackend) InstallToolchain(req model.InstallRequest) error {
	f.storageCalls = append(f.storageCalls, fmt.Sprintf("install tool=%s version=%s preset=%s", req.Tool, req.Version, req.Preset))
	return f.storageErr
}
func (f *fakeBackend) RemoveToolchain(tool, version string) error {
	f.storageCalls = append(f.storageCalls, "remove "+tool+" "+version)
	return f.storageErr
}
func (f *fakeBackend) ClearCache(name string) error {
	f.storageCalls = append(f.storageCalls, "clear "+name)
	return f.storageErr
}
func (f *fakeBackend) PruneScope(scope string) error {
	f.storageCalls = append(f.storageCalls, "prune "+scope)
	return f.storageErr
}

func TestStorageRoutesThroughTheClient(t *testing.T) {
	c, b := setup(t)
	ctx := context.Background()
	b.storage = model.Storage{MeasureError: "x", PackageCaches: []model.PackageCache{{Name: "npm"}}}
	st, err := c.Storage(ctx)
	if err != nil || st.MeasureError != "x" || len(st.PackageCaches) != 1 {
		t.Fatalf("storage %+v, %v", st, err)
	}
	b.choices = []model.ToolchainChoice{{Spec: "21", Version: "21.0.8+9", LTS: true}}
	cs, err := c.AvailableToolchains(ctx, "java")
	if err != nil || len(cs) != 1 || !cs[0].LTS || cs[0].Version != "21.0.8+9" {
		t.Fatalf("choices %+v, %v", cs, err)
	}
	for _, call := range []func() error{
		func() error { return c.RefreshStorage(ctx) },
		func() error { return c.InstallToolchain(ctx, "node", "22") },
		func() error { return c.InstallPreset(ctx, "popular") },
		func() error { return c.RemoveToolchain(ctx, "java", "21.0.8+9") },
		func() error { return c.ClearCache(ctx, "nuget") },
		func() error { return c.PruneScope(ctx, "unused-volumes") },
	} {
		if err := call(); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{
		"storage", "available java", "refresh",
		"install tool=node version=22 preset=", "install tool= version= preset=popular",
		"remove java 21.0.8+9", "clear nuget", "prune unused-volumes",
	}
	if !reflect.DeepEqual(b.storageCalls, want) {
		t.Fatalf("calls %q", b.storageCalls)
	}
}

func TestStorageMutationsAnswer202(t *testing.T) {
	srv := httptest.NewServer(NewServer(&fakeBackend{}))
	defer srv.Close()
	for _, r := range []struct{ method, path, body string }{
		{http.MethodPost, "/storage/refresh", ""},
		{http.MethodPost, "/toolchains", `{"tool":"node","version":"22"}`},
		{http.MethodDelete, "/toolchains/node/22.11.0", ""},
		{http.MethodPost, "/caches/nuget/clear", ""},
		{http.MethodPost, "/prune/dangling-images", ""},
	} {
		req, err := http.NewRequest(r.method, srv.URL+r.path, strings.NewReader(r.body))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Errorf("%s %s: %d", r.method, r.path, resp.StatusCode)
		}
	}
}

func TestStorageErrorsReachTheClient(t *testing.T) {
	c, b := setup(t)
	ctx := context.Background()
	b.storageErr = Conflict("package cache not present: cargo")
	var ae *Error
	if err := c.ClearCache(ctx, "cargo"); !errors.As(err, &ae) || ae.Status != 409 || ae.Msg != "package cache not present: cargo" {
		t.Fatalf("clear: %v", err)
	}
	b.storageErr = &Error{Status: http.StatusBadGateway, Msg: "GET https://api.adoptium.net: 503"}
	if _, err := c.AvailableToolchains(ctx, "java"); !errors.As(err, &ae) || ae.Status != 502 {
		t.Fatalf("available: %v", err)
	}
	b.storageErr = NotFound("toolchain version not installed: node 20.0.0")
	if err := c.RemoveToolchain(ctx, "node", "20.0.0"); !errors.As(err, &ae) || ae.Status != 404 {
		t.Fatalf("remove: %v", err)
	}
	b.storageErr = &Error{Status: http.StatusServiceUnavailable, Msg: "ghr is shutting down"}
	if err := c.InstallToolchain(ctx, "node", "22"); !errors.As(err, &ae) || ae.Status != 503 || ae.Msg != "ghr is shutting down" {
		t.Fatalf("install: %v", err)
	}
	b.storageErr = BadRequest("unknown prune scope \"everything\"")
	if err := c.PruneScope(ctx, "everything"); !errors.As(err, &ae) || ae.Status != 400 {
		t.Fatalf("prune scope: %v", err)
	}
	srv := httptest.NewServer(NewServer(&fakeBackend{}))
	defer srv.Close()
	resp, err := srv.Client().Post(srv.URL+"/toolchains", "application/json", strings.NewReader("{"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad JSON: %d", resp.StatusCode)
	}
}

func TestOlderDaemonAsksForARestartOnStorageRoutes(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /prune", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusAccepted) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := &Client{Base: srv.URL, HTTP: &http.Client{Timeout: 5 * time.Second}}
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"prune scope": func() error { return c.PruneScope(ctx, "build-cache-all") },
		"storage":     func() error { _, err := c.Storage(ctx); return err },
		"install":     func() error { return c.InstallToolchain(ctx, "node", "22") },
		"clear":       func() error { return c.ClearCache(ctx, "nuget") },
	} {
		if err := call(); err == nil || !strings.Contains(err.Error(), "systemctl restart ghr") {
			t.Errorf("%s: %v", name, err)
		}
	}
}
