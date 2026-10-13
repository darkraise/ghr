package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNormalizeSKU(t *testing.T) {
	for in, want := range map[string]string{
		"Actions storage":  SKUActionsStorage,
		"actions_storage":  SKUActionsStorage,
		"Packages storage": SKUPackagesStorage,
		"GigabyteHours":    "gigabytehours",
		"gigabyte-hours":   "gigabytehours",
	} {
		if got := NormalizeSKU(in); got != want {
			t.Errorf("NormalizeSKU(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOwnerTypeAndTokenLogin(t *testing.T) {
	kind := "User"
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/darkraise":
			fmt.Fprintf(w, `{"login":"darkraise","type":%q}`, kind)
		case "/user":
			fmt.Fprint(w, `{"login":"someone"}`)
		default:
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()
	if isOrg, err := c.OwnerType(ctx); err != nil || isOrg {
		t.Fatalf("user owner: isOrg %v err %v", isOrg, err)
	}
	kind = "Organization"
	if isOrg, err := c.OwnerType(ctx); err != nil || !isOrg {
		t.Fatalf("organization owner: isOrg %v err %v", isOrg, err)
	}
	if login, err := c.TokenLogin(ctx); err != nil || login != "someone" {
		t.Fatalf("login %q err %v", login, err)
	}
	if got := strings.Join(f.requests, "|"); got != "GET /users/darkraise|GET /users/darkraise|GET /user" {
		t.Fatalf("requests %q", got)
	}
}

const usageBody = `{"usageItems":[
{"date":"2026-10-01T00:00:00Z","product":"actions","sku":"Actions storage","quantity":5.745991992,"unitType":"GigabyteHours","repositoryName":"chicken-farm"},
{"date":"2026-10-02T00:00:00Z","product":"packages","sku":"Packages storage","quantity":0.185828452,"unitType":"GigabyteHours","repositoryName":""},
{"date":"2026-10-02T00:00:00Z","product":"git_lfs","sku":"Git LFS storage","quantity":1.6,"unitType":"GigabyteHours","repositoryName":"darkfoto"},
{"date":"2026-10-02T00:00:00Z","product":"actions","sku":"Actions Linux","quantity":120,"unitType":"Minutes","repositoryName":"darkmem"}]}`

// A user owner's billing lives under /users, an organization's under
// /organizations; only Actions and Packages storage are kept.
func TestStorageUsage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		isOrg  bool
		prefix string
	}{
		{"user", false, "/users/darkraise/settings/billing/usage"},
		{"organization", true, "/organizations/darkraise/settings/billing/usage"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case tc.prefix:
					fmt.Fprint(w, usageBody)
				default:
					http.NotFound(w, r)
				}
			})
			ctx := context.Background()
			items, err := c.StorageUsage(ctx, tc.isOrg, 2026, time.October)
			if err != nil {
				t.Fatal(err)
			}
			want := []UsageItem{
				{Date: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), SKU: SKUActionsStorage, Repo: "chicken-farm", Quantity: 5.745991992},
				{Date: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC), SKU: SKUPackagesStorage, Repo: "", Quantity: 0.185828452},
			}
			if len(items) != len(want) {
				t.Fatalf("items %+v", items)
			}
			for i := range want {
				if !items[i].Date.Equal(want[i].Date) || items[i].SKU != want[i].SKU || items[i].Repo != want[i].Repo || items[i].Quantity != want[i].Quantity {
					t.Fatalf("item %d = %+v, want %+v", i, items[i], want[i])
				}
			}
			wantReq := "GET " + tc.prefix + "?year=2026&month=10"
			if got := strings.Join(f.requests, "|"); got != wantReq {
				t.Fatalf("requests %q, want %q", got, wantReq)
			}
		})
	}
}

func TestRepoArtifactsAndCachesPaginate(t *testing.T) {
	var base string
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		page := "1"
		if r.URL.Query().Get("page") == "" {
			w.Header().Set("Link", fmt.Sprintf(`<%s%s&page=2>; rel="next"`, base, r.URL.RequestURI()))
		} else {
			page = "2"
		}
		if strings.HasSuffix(r.URL.Path, "/caches") {
			fmt.Fprintf(w, `{"total_count":2,"actions_caches":[{"id":%s,"ref":"refs/heads/master","key":"node-cache","size_in_bytes":71431401,"last_accessed_at":"2026-10-11T05:51:49.688101Z","created_at":"2026-10-11T05:33:45.524625Z"}]}`, page)
			return
		}
		fmt.Fprintf(w, `{"total_count":2,"artifacts":[{"id":%s,"name":"dist","size_in_bytes":2400000,"expired":false,"created_at":"2026-10-10T08:00:00Z","expires_at":"2027-01-08T08:00:00Z","workflow_run":{"id":102,"head_branch":"master"}}]}`, page)
	})
	base = c.BaseURL
	ctx := context.Background()
	arts, err := c.ListRepoArtifacts(ctx, "darkmem")
	if err != nil || len(arts) != 2 || arts[1].ID != 2 {
		t.Fatalf("artifacts %+v err %v", arts, err)
	}
	if a := arts[0]; a.Name != "dist" || a.SizeInBytes != 2400000 || !a.CreatedAt.Equal(time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)) ||
		a.WorkflowRun != (ArtifactRun{ID: 102, HeadBranch: "master"}) {
		t.Fatalf("artifact %+v", a)
	}
	caches, err := c.ListCaches(ctx, "darkmem")
	if err != nil || len(caches) != 2 || caches[1].ID != 2 {
		t.Fatalf("caches %+v err %v", caches, err)
	}
	if k := caches[0]; k.Key != "node-cache" || k.Ref != "refs/heads/master" || k.SizeInBytes != 71431401 ||
		!k.LastAccessedAt.Equal(time.Date(2026, 10, 11, 5, 51, 49, 688101000, time.UTC)) ||
		!k.CreatedAt.Equal(time.Date(2026, 10, 11, 5, 33, 45, 524625000, time.UTC)) {
		t.Fatalf("cache %+v", k)
	}
	want := "GET /repos/darkraise/darkmem/actions/artifacts?per_page=100|GET /repos/darkraise/darkmem/actions/artifacts?per_page=100&page=2|" +
		"GET /repos/darkraise/darkmem/actions/caches?per_page=100|GET /repos/darkraise/darkmem/actions/caches?per_page=100&page=2"
	if got := strings.Join(f.requests, "|"); got != want {
		t.Fatalf("requests %q", got)
	}
}

// A 404 means someone else deleted the item first: not deleted, no error.
func TestDeleteArtifactAndCache(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		status  int
		del     func(c *Client) (bool, error)
		path    string
		deleted bool
		wantErr bool
	}{
		{"artifact deleted", 204, func(c *Client) (bool, error) { return c.DeleteArtifact(ctx, "darkmem", 7) }, "/repos/darkraise/darkmem/actions/artifacts/7", true, false},
		{"artifact already gone", 404, func(c *Client) (bool, error) { return c.DeleteArtifact(ctx, "darkmem", 7) }, "/repos/darkraise/darkmem/actions/artifacts/7", false, false},
		{"artifact refused", 403, func(c *Client) (bool, error) { return c.DeleteArtifact(ctx, "darkmem", 7) }, "/repos/darkraise/darkmem/actions/artifacts/7", false, true},
		{"cache deleted", 204, func(c *Client) (bool, error) { return c.DeleteCache(ctx, "darkmem", 9) }, "/repos/darkraise/darkmem/actions/caches/9", true, false},
		{"cache already gone", 404, func(c *Client) (bool, error) { return c.DeleteCache(ctx, "darkmem", 9) }, "/repos/darkraise/darkmem/actions/caches/9", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				if tc.status != 204 {
					fmt.Fprint(w, `{"message":"no"}`)
				}
			})
			c.Sleep = func(context.Context, time.Duration) error { return nil }
			deleted, err := tc.del(c)
			if deleted != tc.deleted || (err != nil) != tc.wantErr {
				t.Fatalf("deleted %v err %v", deleted, err)
			}
			if got := strings.Join(f.requests, "|"); got != "DELETE "+tc.path {
				t.Fatalf("requests %q", got)
			}
		})
	}
}

func TestRetention(t *testing.T) {
	var body string
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			b, _ := io.ReadAll(r.Body)
			body = string(b)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		fmt.Fprint(w, `{"days":90,"maximum_allowed_days":400}`)
	})
	c.Sleep = func(context.Context, time.Duration) error { return nil }
	ctx := context.Background()
	r, err := c.Retention(ctx, "darkmem")
	if err != nil || r != (Retention{Days: 90, MaxDays: 400}) {
		t.Fatalf("retention %+v err %v", r, err)
	}
	if err := c.SetRetention(ctx, "darkmem", 3); err != nil {
		t.Fatal(err)
	}
	if body != `{"days":3}` {
		t.Fatalf("body %q", body)
	}
	const path = "/repos/darkraise/darkmem/actions/permissions/artifact-and-log-retention"
	if got := strings.Join(f.requests, "|"); got != "GET "+path+"|PUT "+path {
		t.Fatalf("requests %q", got)
	}
}
