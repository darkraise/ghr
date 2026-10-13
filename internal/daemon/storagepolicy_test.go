package daemon

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

func patchJSON(t *testing.T, b *Backend, body string) error {
	t.Helper()
	var p model.ConfigPatch
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatal(err)
	}
	return b.PatchConfig(context.Background(), p)
}

func TestPatchGitHubStorage(t *testing.T) {
	b, _, _ := newBackend(t)
	if err := patchJSON(t, b, `{"github_storage":{"included_mb":2048,"warn_pct":[60,80],"cleanup_max_age":"2d","repos":{"darkmem":"6h","homelab":"off"}}}`); err != nil {
		t.Fatal(err)
	}
	want := config.GitHubStorage{IncludedMB: 2048, WarnPct: []int{60, 80}, CleanupMaxAge: config.Duration(48 * time.Hour),
		Repos: map[string]config.StorageOverride{"darkmem": {Cleanup: "6h"}, "homelab": {Cleanup: "off"}}}
	if got := b.Store.Config().GitHubStorage; !reflect.DeepEqual(got, want) {
		t.Fatalf("block %+v", got)
	}
	saved, _, err := config.Load(b.Store.ConfigPath)
	if err != nil || !reflect.DeepEqual(saved.GitHubStorage, want) {
		t.Fatalf("not persisted: %+v %v", saved.GitHubStorage, err)
	}
	// A key that differs only by case replaces the override; null removes one.
	if err := patchJSON(t, b, `{"github_storage":{"cleanup_max_age":"","repos":{"DarkMem":"12h","homelab":null}}}`); err != nil {
		t.Fatal(err)
	}
	want = config.GitHubStorage{IncludedMB: 2048, WarnPct: []int{60, 80}, Repos: map[string]config.StorageOverride{"DarkMem": {Cleanup: "12h"}}}
	if got := b.Store.Config().GitHubStorage; !reflect.DeepEqual(got, want) {
		t.Fatalf("block %+v", got)
	}
	// Back to every default: the block is no longer written.
	if err := patchJSON(t, b, `{"github_storage":{"included_mb":500,"warn_pct":[70,90],"repos":{"darkmem":null}}}`); err != nil {
		t.Fatal(err)
	}
	if got := b.Store.Config().GitHubStorage; !reflect.DeepEqual(got, config.GitHubStorage{}) {
		t.Fatalf("block %+v", got)
	}
	data, err := os.ReadFile(b.Store.ConfigPath)
	if err != nil || strings.Contains(string(data), "github_storage") {
		t.Fatalf("default block saved: %v\n%s", err, data)
	}
}

func TestPatchGitHubStorageRejects(t *testing.T) {
	b, _, _ := newBackend(t)
	if err := patchJSON(t, b, `{"github_storage":{"cleanup_max_age":"2d"}}`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		body, want string
	}{
		{`{"included_mb":0}`, "github_storage.included_mb must be at least 1"},
		{`{"cleanup_max_age":"soon"}`, `github_storage.cleanup_max_age: invalid duration "soon"`},
		{`{"cleanup_max_age":"23h"}`, "github_storage.cleanup_max_age must be at least 24h"},
		{`{"warn_pct":[90,70]}`, "github_storage.warn_pct must be two increasing percentages from 1 to 100, such as [70, 90]"},
		{`{"repos":{"darkmem":"30m"}}`, "github_storage.repos: darkmem: cleanup must be off or a duration of at least 1h"},
		{`{"repos":{"":"off"}}`, "github_storage.repos must not contain an empty name"},
	} {
		err := patchJSON(t, b, `{"github_storage":`+tc.body+`}`)
		if apiStatus(err) != 400 || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err %v, want 400 %q", tc.body, err, tc.want)
		}
	}
	want := config.GitHubStorage{CleanupMaxAge: config.Duration(48 * time.Hour)}
	if got := b.Store.Config().GitHubStorage; !reflect.DeepEqual(got, want) {
		t.Fatalf("a refused patch changed the block: %+v", got)
	}
}
