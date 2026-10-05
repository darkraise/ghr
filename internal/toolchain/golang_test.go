package toolchain

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

const goManifest = `[
 {"version":"1.25.1","stable":true,"files":[{"filename":"go-1.25.1-linux-x64.tar.gz","arch":"x64","platform":"linux","download_url":"https://example.invalid/a"}]},
 {"version":"1.24.7","stable":true,"files":[{"filename":"go-1.24.7-linux-x64.tar.gz","arch":"x64","platform":"linux","download_url":"https://example.invalid/b"}]},
 {"version":"1.20","stable":true,"files":[{"filename":"go-1.20-linux-x64.tar.gz","arch":"x64","platform":"linux","download_url":"https://example.invalid/c"}]}
]`

func goReleases(sums map[string]string) []byte {
	var files []string
	for name, sum := range sums {
		files = append(files, fmt.Sprintf(`{"filename":%q,"os":"linux","arch":"amd64","kind":"archive","sha256":%q}`, name, sum))
	}
	return []byte(`[{"version":"go1.25.2","stable":true,"files":[` + strings.Join(files, ",") + `]}]`)
}

func TestGoResolvesFromTheManifestAndGoDev(t *testing.T) {
	f := newFixture(t)
	f.serve("/go.json", []byte(goManifest))
	f.serve("/godl.json", goReleases(map[string]string{
		"go1.25.2.linux-amd64.tar.gz": "22",
		"go1.25.1.linux-amd64.tar.gz": "11",
		"go1.20.linux-amd64.tar.gz":   "20",
	}))
	g := newGo(f.Env)
	rel, err := g.Resolve(context.Background(), "latest")
	if err != nil {
		t.Fatal(err)
	}
	want := Release{Tool: "go", Version: "1.25.1", Folder: "1.25.1", URL: f.url + "/dl/go1.25.1.linux-amd64.tar.gz", SHA256: "11"}
	if rel != want {
		t.Fatalf("latest resolved %+v; setup-go's stable comes from the go-versions manifest, not go.dev", rel)
	}
	rel, err = g.Resolve(context.Background(), "1.20")
	if err != nil {
		t.Fatal(err)
	}
	if rel.Folder != "1.20.0" || rel.Version != "1.20.0" || rel.URL != f.url+"/dl/go1.20.linux-amd64.tar.gz" {
		t.Fatalf("1.20 resolved %+v", rel)
	}
	if _, err := g.Resolve(context.Background(), "1.24"); err == nil || !strings.Contains(err.Error(), "go1.24.7.linux-amd64.tar.gz") {
		t.Fatalf("a release without a go.dev checksum: %v", err)
	}
}

func TestGoInstallIsFoundBySetupGo(t *testing.T) {
	f := newFixture(t)
	archive := tarGz(map[string]string{"go/bin/go": "go"})
	f.serve("/go.json", []byte(goManifest))
	f.serve("/godl.json", goReleases(map[string]string{"go1.25.1.linux-amd64.tar.gz": sum256(archive)}))
	f.serve("/dl/go1.25.1.linux-amd64.tar.gz", archive)
	g := newGo(f.Env)
	rel, err := g.Resolve(context.Background(), "1.25")
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Install(context.Background(), rel, func(string) {}); err != nil {
		t.Fatal(err)
	}
	dir := toolCacheFind(f.Root, "go", "1.25.1")
	if dir != filepath.Join(f.Root, "go", "1.25.1", "x64") || !exists(filepath.Join(dir, "bin", "go")) {
		t.Fatalf("setup-go would find %q", dir)
	}
	if got := toolCacheFind(f.Root, "go", "1.25"); got != dir {
		t.Fatalf("setup-go with 1.25 would find %q", got)
	}
	if got, err := g.Installed(); err != nil || len(got) != 1 || got[0].Version != "1.25.1" {
		t.Fatalf("installed %+v, %v", got, err)
	}
}
