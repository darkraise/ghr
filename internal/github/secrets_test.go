package github

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/nacl/box"
)

func TestVariables(t *testing.T) {
	var mu sync.Mutex
	vars := map[string]string{}
	const path = "/repos/darkraise/darkmem/actions/variables"
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		name := strings.TrimPrefix(r.URL.Path, path+"/")
		var body struct{ Name, Value string }
		if r.Body != nil {
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &body)
		}
		_, ok := vars[name]
		switch {
		case r.Method == http.MethodPost && r.URL.Path == path:
			vars[body.Name] = body.Value
			w.WriteHeader(http.StatusCreated)
		case !ok:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		case r.Method == http.MethodGet:
			fmt.Fprintf(w, `{"name":%q,"value":%q,"created_at":"2026-10-15T09:00:00Z","updated_at":"2026-10-15T09:00:00Z"}`, name, vars[name])
		case r.Method == http.MethodPatch:
			vars[name] = body.Value
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete:
			delete(vars, name)
			w.WriteHeader(http.StatusNoContent)
		}
	})
	c.Sleep = func(context.Context, time.Duration) error { return nil }
	ctx := context.Background()
	const name = "GHR_ARTIFACT_STORE"
	if v, found, err := c.GetVariable(ctx, "darkmem", name); err != nil || found || v != "" {
		t.Fatalf("missing: %q %v %v", v, found, err)
	}
	if err := c.SetVariable(ctx, "darkmem", name, `{"v":1}`); err != nil {
		t.Fatal(err)
	}
	if v, found, err := c.GetVariable(ctx, "darkmem", name); err != nil || !found || v != `{"v":1}` {
		t.Fatalf("created: %q %v %v", v, found, err)
	}
	if err := c.SetVariable(ctx, "darkmem", name, `{"v":2}`); err != nil {
		t.Fatal(err)
	}
	if v, found, err := c.GetVariable(ctx, "darkmem", name); err != nil || !found || v != `{"v":2}` {
		t.Fatalf("updated: %q %v %v", v, found, err)
	}
	if deleted, err := c.DeleteVariable(ctx, "darkmem", name); err != nil || !deleted {
		t.Fatalf("delete: %v %v", deleted, err)
	}
	if deleted, err := c.DeleteVariable(ctx, "darkmem", name); err != nil || deleted {
		t.Fatalf("delete again: %v %v", deleted, err)
	}
	want := []string{
		"GET " + path + "/" + name,
		"PATCH " + path + "/" + name,
		"POST " + path,
		"GET " + path + "/" + name,
		"PATCH " + path + "/" + name,
		"GET " + path + "/" + name,
		"DELETE " + path + "/" + name,
		"DELETE " + path + "/" + name,
	}
	if got := strings.Join(f.requests, "|"); got != strings.Join(want, "|") {
		t.Fatalf("requests %q", got)
	}
}

func TestSecrets(t *testing.T) {
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	secrets := map[string]string{}
	const path = "/repos/darkraise/darkmem/actions/secrets"
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == path+"/public-key" {
			fmt.Fprintf(w, `{"key_id":"568250167242549743","key":%q}`, base64.StdEncoding.EncodeToString(pub[:]))
			return
		}
		name := strings.TrimPrefix(r.URL.Path, path+"/")
		_, ok := secrets[name]
		switch {
		case r.Method == http.MethodPut:
			var body struct {
				EncryptedValue string `json:"encrypted_value"`
				KeyID          string `json:"key_id"`
			}
			b, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(b, &body); err != nil || body.KeyID != "568250167242549743" {
				t.Errorf("body %s: %v", b, err)
			}
			sealed, err := base64.StdEncoding.DecodeString(body.EncryptedValue)
			if err != nil {
				t.Errorf("encrypted_value is not base64: %v", err)
			}
			plain, ok := box.OpenAnonymous(nil, sealed, pub, priv)
			if !ok {
				t.Errorf("encrypted_value does not open with the repository's key")
			}
			secrets[name] = string(plain)
			w.WriteHeader(http.StatusCreated)
		case !ok:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		case r.Method == http.MethodGet:
			fmt.Fprintf(w, `{"name":%q,"created_at":"2026-10-15T09:00:00Z","updated_at":"2026-10-15T09:00:00Z"}`, name)
		case r.Method == http.MethodDelete:
			delete(secrets, name)
			w.WriteHeader(http.StatusNoContent)
		}
	})
	c.Sleep = func(context.Context, time.Duration) error { return nil }
	ctx := context.Background()
	const name = "GHR_ARTIFACT_KEY"
	if found, err := c.SecretExists(ctx, "darkmem", name); err != nil || found {
		t.Fatalf("missing: %v %v", found, err)
	}
	if err := c.SetSecret(ctx, "darkmem", name, "JOBKEY:job-secret"); err != nil {
		t.Fatal(err)
	}
	if secrets[name] != "JOBKEY:job-secret" {
		t.Fatalf("stored %q", secrets[name])
	}
	if found, err := c.SecretExists(ctx, "darkmem", name); err != nil || !found {
		t.Fatalf("set: %v %v", found, err)
	}
	if deleted, err := c.DeleteSecret(ctx, "darkmem", name); err != nil || !deleted {
		t.Fatalf("delete: %v %v", deleted, err)
	}
	if deleted, err := c.DeleteSecret(ctx, "darkmem", name); err != nil || deleted {
		t.Fatalf("delete again: %v %v", deleted, err)
	}
	want := []string{
		"GET " + path + "/" + name,
		"GET " + path + "/public-key",
		"PUT " + path + "/" + name,
		"GET " + path + "/" + name,
		"DELETE " + path + "/" + name,
		"DELETE " + path + "/" + name,
	}
	if got := strings.Join(f.requests, "|"); got != strings.Join(want, "|") {
		t.Fatalf("requests %q", got)
	}
}

// A refusal is an error, not "not found", so a caller never writes over a
// variable or secret it could not read.
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
	if found, err := c.SecretExists(ctx, "darkmem", "GHR_ARTIFACT_KEY"); !IsKind(err, ErrAuth) || found {
		t.Errorf("SecretExists: %v %v", found, err)
	}
	if err := c.SetVariable(ctx, "darkmem", "GHR_ARTIFACT_STORE", "x"); !IsKind(err, ErrAuth) {
		t.Errorf("SetVariable: %v", err)
	}
	if err := c.SetSecret(ctx, "darkmem", "GHR_ARTIFACT_KEY", "JOBKEY:job-secret"); !IsKind(err, ErrAuth) || strings.Contains(err.Error(), "job-secret") {
		t.Errorf("SetSecret: %v", err)
	}
	if deleted, err := c.DeleteSecret(ctx, "darkmem", "GHR_ARTIFACT_KEY"); !IsKind(err, ErrAuth) || deleted {
		t.Errorf("DeleteSecret: %v %v", deleted, err)
	}
}

func TestSealSecret(t *testing.T) {
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := SealSecret(base64.StdEncoding.EncodeToString(pub[:]), "JOBKEY:job-secret")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if plain, ok := box.OpenAnonymous(nil, raw, pub, priv); !ok || string(plain) != "JOBKEY:job-secret" {
		t.Fatalf("opened %q %v", plain, ok)
	}
	for _, key := range []string{"not base64!", base64.StdEncoding.EncodeToString(make([]byte, 31)), ""} {
		_, err := SealSecret(key, "JOBKEY:job-secret")
		if err == nil || err.Error() != "github: the repository's public key is not a base64 32-byte key" {
			t.Errorf("key %q: %v", key, err)
		}
	}
}
