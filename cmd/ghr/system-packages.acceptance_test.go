package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/darkraise/ghr/internal/api"
)

func TestA20_PackageCommandsMapToTheRoutes(t *testing.T) {
	for _, tc := range []struct {
		args         []string
		method, path string
		body         map[string]any
	}{
		{[]string{"package", "list"}, "GET", "/packages", nil},
		{[]string{"package", "search", "rip"}, "GET", "/packages/search?q=rip", nil},
		{[]string{"package", "install", "ripgrep", "jq"}, "POST", "/packages", map[string]any{"names": []any{"ripgrep", "jq"}}},
		{[]string{"package", "upgrade", "ripgrep", "jq"}, "POST", "/packages/upgrade", map[string]any{"names": []any{"ripgrep", "jq"}}},
		{[]string{"package", "upgrade", "--all"}, "POST", "/packages/upgrade", map[string]any{"all": true}},
		{[]string{"package", "uninstall", "ripgrep"}, "DELETE", "/packages/ripgrep?uninstall=1", nil},
		{[]string{"package", "forget", "ripgrep"}, "DELETE", "/packages/ripgrep", nil},
		{[]string{"package", "refresh"}, "POST", "/packages/refresh", nil},
	} {
		var method, path string
		var body []byte
		requests := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++
			method, path = r.Method, r.URL.RequestURI()
			body, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/packages":
				w.Write([]byte(`{"managed":[],"base":[],"detected":[]}`))
			case r.Method == http.MethodGet:
				w.Write([]byte(`[]`))
			case r.Method == http.MethodPost && r.URL.Path == "/packages":
				w.WriteHeader(http.StatusAccepted)
				w.Write([]byte(`{"adopt":[],"install":["ripgrep","jq"]}`))
			case r.Method == http.MethodPost:
				w.WriteHeader(http.StatusAccepted)
			default:
				w.WriteHeader(http.StatusNoContent)
			}
		}))
		old := newClient
		newClient = func() *api.Client { return &api.Client{Base: srv.URL, HTTP: srv.Client()} }
		code, _, errb := runCLI(t, "", tc.args...)
		newClient = old
		srv.Close()
		if code != 0 {
			t.Errorf("%v: exit %d, stderr %s", tc.args, code, errb)
			continue
		}
		if requests != 1 || method != tc.method || path != tc.path {
			t.Errorf("%v: %d requests, last %s %s, want 1 request %s %s", tc.args, requests, method, path, tc.method, tc.path)
			continue
		}
		if tc.body == nil {
			if len(body) != 0 {
				t.Errorf("%v: unexpected body %s", tc.args, body)
			}
			continue
		}
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil || !reflect.DeepEqual(got, tc.body) {
			t.Errorf("%v: body %s, want %v", tc.args, body, tc.body)
		}
	}
}
