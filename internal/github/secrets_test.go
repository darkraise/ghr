package github

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestVariables(t *testing.T) {
	var mu sync.Mutex
	vars := map[string]string{"GHR_ARTIFACT_STORE": `{"v":1}`}
	const path = "/repos/darkraise/darkmem/actions/variables"
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		name := strings.TrimPrefix(r.URL.Path, path+"/")
		_, ok := vars[name]
		switch {
		case !ok:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		case r.Method == http.MethodGet:
			fmt.Fprintf(w, `{"name":%q,"value":%q,"created_at":"2026-10-15T09:00:00Z","updated_at":"2026-10-15T09:00:00Z"}`, name, vars[name])
		case r.Method == http.MethodDelete:
			delete(vars, name)
			w.WriteHeader(http.StatusNoContent)
		}
	})
	c.Sleep = func(context.Context, time.Duration) error { return nil }
	ctx := context.Background()
	const name = "GHR_ARTIFACT_STORE"
	if v, found, err := c.GetVariable(ctx, "darkmem", name); err != nil || !found || v != `{"v":1}` {
		t.Fatalf("present: %q %v %v", v, found, err)
	}
	if deleted, err := c.DeleteVariable(ctx, "darkmem", name); err != nil || !deleted {
		t.Fatalf("delete: %v %v", deleted, err)
	}
	if v, found, err := c.GetVariable(ctx, "darkmem", name); err != nil || found || v != "" {
		t.Fatalf("missing: %q %v %v", v, found, err)
	}
	if deleted, err := c.DeleteVariable(ctx, "darkmem", name); err != nil || deleted {
		t.Fatalf("delete again: %v %v", deleted, err)
	}
	want := []string{
		"GET " + path + "/" + name,
		"DELETE " + path + "/" + name,
		"GET " + path + "/" + name,
		"DELETE " + path + "/" + name,
	}
	if got := strings.Join(f.requests, "|"); got != strings.Join(want, "|") {
		t.Fatalf("requests %q", got)
	}
}

func TestSecrets(t *testing.T) {
	var mu sync.Mutex
	secrets := map[string]bool{"GHR_ARTIFACT_KEY": true}
	const path = "/repos/darkraise/darkmem/actions/secrets"
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		name := strings.TrimPrefix(r.URL.Path, path+"/")
		if !secrets[name] {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
			return
		}
		delete(secrets, name)
		w.WriteHeader(http.StatusNoContent)
	})
	c.Sleep = func(context.Context, time.Duration) error { return nil }
	ctx := context.Background()
	const name = "GHR_ARTIFACT_KEY"
	if deleted, err := c.DeleteSecret(ctx, "darkmem", name); err != nil || !deleted {
		t.Fatalf("delete: %v %v", deleted, err)
	}
	if deleted, err := c.DeleteSecret(ctx, "darkmem", name); err != nil || deleted {
		t.Fatalf("delete again: %v %v", deleted, err)
	}
	want := []string{
		"DELETE " + path + "/" + name,
		"DELETE " + path + "/" + name,
	}
	if got := strings.Join(f.requests, "|"); got != strings.Join(want, "|") {
		t.Fatalf("requests %q", got)
	}
}

// A refusal is an error, not "not found" or "already gone", so a caller
// never reports a variable or secret removed when it could not be.
func TestVariableAndSecretRefusals(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Accepted-GitHub-Permissions", "secrets=read")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"Resource not accessible by personal access token"}`)
	})
	c.Sleep = func(context.Context, time.Duration) error { return nil }
	ctx := context.Background()
	if _, found, err := c.GetVariable(ctx, "darkmem", "GHR_ARTIFACT_STORE"); !IsKind(err, ErrAuth) || found {
		t.Errorf("GetVariable: %v %v", found, err)
	}
	if deleted, err := c.DeleteVariable(ctx, "darkmem", "GHR_ARTIFACT_STORE"); !IsKind(err, ErrAuth) || deleted {
		t.Errorf("DeleteVariable: %v %v", deleted, err)
	}
	if deleted, err := c.DeleteSecret(ctx, "darkmem", "GHR_ARTIFACT_KEY"); !IsKind(err, ErrAuth) || deleted {
		t.Errorf("DeleteSecret: %v %v", deleted, err)
	}
}
