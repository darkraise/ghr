package toolchain

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func nodeManifest(url string) []byte {
	return []byte(fmt.Sprintf(`[
 {"version":"24.9.0","stable":true,"files":[
   {"filename":"node-24.9.0-win32-x64","arch":"x64","platform":"win32","download_url":"%[1]s/win.zip"},
   {"filename":"node-24.9.0-linux-x64","arch":"x64","platform":"linux","download_url":"%[1]s/node-24.9.0.tar.gz"}]},
 {"version":"25.0.0-rc.1","stable":false,"files":[
   {"filename":"node-25.0.0-rc.1-linux-x64","arch":"x64","platform":"linux","download_url":"%[1]s/rc.tar.gz"}]},
 {"version":"22.12.0","stable":false,"files":[
   {"filename":"node-22.12.0-linux-x64","arch":"x64","platform":"linux","download_url":"%[1]s/unstable.tar.gz"}]},
 {"version":"22.11.0","stable":true,"files":[
   {"filename":"node-22.11.0-linux-arm64","arch":"arm64","platform":"linux","download_url":"%[1]s/arm.tar.gz"},
   {"filename":"node-22.11.0-linux-x64","arch":"x64","platform":"linux","download_url":"%[1]s/node-22.11.0.tar.gz"}]},
 {"version":"22.10.0","stable":true,"files":[
   {"filename":"node-22.10.0-win32-x64","arch":"x64","platform":"win32","download_url":"%[1]s/win.zip"}]}
]`, url))
}

func TestNodeAvailableAndResolve(t *testing.T) {
	f := newFixture(t)
	f.serve("/node.json", nodeManifest(f.url))
	n := newNode(f.Env)
	cs, err := n.Available(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []Choice{{Spec: "24.9.0", Version: "24.9.0"}, {Spec: "22.11.0", Version: "22.11.0"}}; !reflect.DeepEqual(cs, want) {
		t.Fatalf("available %+v", cs)
	}
	rel, err := n.Resolve(context.Background(), "22")
	if err != nil {
		t.Fatal(err)
	}
	if want := (Release{Tool: "node", Version: "22.11.0", Folder: "22.11.0", URL: f.url + "/node-22.11.0.tar.gz"}); rel != want {
		t.Fatalf("resolved %+v", rel)
	}
	if rel, _ := n.Resolve(context.Background(), "latest"); rel.Version != "24.9.0" {
		t.Fatalf("latest resolved %+v", rel)
	}
	if _, err := n.Resolve(context.Background(), "20"); err == nil {
		t.Fatal("20 resolved")
	}
}

func TestNodeInstallIsFoundBySetupNode(t *testing.T) {
	f := newFixture(t)
	f.serve("/node.json", nodeManifest(f.url))
	f.serve("/node-22.11.0.tar.gz", tarGz(map[string]string{"node-22.11.0-linux-x64/bin/node": "node"}))
	n := newNode(f.Env)
	rel, err := n.Resolve(context.Background(), "22")
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Install(context.Background(), rel, func(string) {}); err != nil {
		t.Fatal(err)
	}
	dir := toolCacheFind(f.Root, "node", "22")
	if dir != filepath.Join(f.Root, "node", "22.11.0", "x64") || !exists(filepath.Join(dir, "bin", "node")) {
		t.Fatalf("setup-node would find %q", dir)
	}
	got, err := n.Installed()
	if err != nil || len(got) != 1 || got[0].Version != "22.11.0" || got[0].Tool != "node" {
		t.Fatalf("installed %+v, %v", got, err)
	}
	if err := n.Remove("22.11.0"); err != nil {
		t.Fatal(err)
	}
	if toolCacheFind(f.Root, "node", "22") != "" {
		t.Fatal("removed version still found")
	}
	if err := n.Remove("22.11.0"); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("second remove: %v", err)
	}
}

func TestNodeManifestErrorsNameTheTool(t *testing.T) {
	f := newFixture(t)
	if _, err := newNode(f.Env).Available(context.Background()); err == nil || !strings.HasPrefix(err.Error(), "Node versions: ") {
		t.Fatalf("error %v", err)
	}
}
