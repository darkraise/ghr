package toolchain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

const dotnetIndex = `{"releases-index":[
 {"channel-version":"11.0","latest-sdk":"11.0.100-rc.2.25502.107","support-phase":"go-live","release-type":"sts"},
 {"channel-version":"10.0","latest-sdk":"10.0.105","support-phase":"active","release-type":"lts"},
 {"channel-version":"9.0","latest-sdk":"9.0.311","support-phase":"maintenance","release-type":"sts"},
 {"channel-version":"8.0","latest-sdk":"8.0.414","support-phase":"maintenance","release-type":"lts"},
 {"channel-version":"7.0","latest-sdk":"7.0.410","support-phase":"eol","release-type":"sts"}
]}`

func dotnetFixture(t *testing.T) *fixture {
	f := newFixture(t)
	f.serve("/dotnet-index.json", []byte(dotnetIndex))
	f.serve("/dotnet-install.sh", []byte("#!/bin/bash"))
	return f
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDotnetAvailableAndResolve(t *testing.T) {
	f := dotnetFixture(t)
	d := newDotnet(f.Env)
	cs, err := d.Available(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Choice{{"10.0", "10.0.105", true}, {"9.0", "9.0.311", false}, {"8.0", "8.0.414", true}}
	if !reflect.DeepEqual(cs, want) {
		t.Fatalf("available %+v", cs)
	}
	cases := map[string]string{"8.0": "8.0.414", "10.0": "10.0.105", "8.0.400": "8.0.400"}
	for spec, ver := range cases {
		rel, err := d.Resolve(context.Background(), spec)
		if err != nil {
			t.Fatalf("%s: %v", spec, err)
		}
		if want := (Release{Tool: "dotnet", Version: ver, Folder: ver, URL: f.url + "/dotnet-install.sh"}); rel != want {
			t.Fatalf("%s resolved %+v", spec, rel)
		}
	}
	for _, bad := range []string{"7.0", "11.0", "8", "latest"} {
		if _, err := d.Resolve(context.Background(), bad); err == nil {
			t.Errorf("%q resolved", bad)
		}
	}
}

func TestDotnetInstallRunsTheInstallScript(t *testing.T) {
	f := dotnetFixture(t)
	var gotArgs []string
	var gotEnv map[string]string
	f.host.scripts["dotnet-install.sh"] = func(dir string, env map[string]string, args []string) error {
		gotArgs, gotEnv = args, env
		if !exists(filepath.Join(dir, "dotnet-install.sh")) {
			return errors.New("script not in the working directory")
		}
		root := filepath.Join(f.Root, "dotnet")
		touch(t, filepath.Join(root, "dotnet"))
		touch(t, filepath.Join(root, "sdk", "8.0.414", "dotnet.dll"))
		touch(t, filepath.Join(root, "shared", "Microsoft.NETCore.App", "8.0.20", "x"))
		return nil
	}
	d := newDotnet(f.Env)
	rel, _ := d.Resolve(context.Background(), "8.0")
	var steps []string
	if err := d.Install(context.Background(), rel, func(s string) { steps = append(steps, s) }); err != nil {
		t.Fatal(err)
	}
	wantArgs := []string{"--version", "8.0.414", "--install-dir", filepath.Join(f.Root, "dotnet"), "--skip-non-versioned-files"}
	if !slices.Equal(gotArgs, wantArgs) {
		t.Fatalf("args %q", gotArgs)
	}
	if gotEnv["HOME"] != "/home/ghrunner" {
		t.Fatalf("env %v", gotEnv)
	}
	if dotnetFind(filepath.Join(f.Root, "dotnet"), "8.0.x") == "" {
		t.Fatal("setup-dotnet would not find SDK 8.0.414")
	}
	if !slices.Equal(steps, []string{"downloading", "running dotnet-install.sh"}) {
		t.Fatalf("progress %v", steps)
	}
	if !slices.Contains(f.host.commands(), "chown -h ghrunner:ghrunner "+filepath.Join(f.Root, "dotnet")) {
		t.Fatalf("commands %q: <Root>/dotnet must be created owned by ghrunner", f.host.commands())
	}
	if got := tmpEntries(t, f.Root); len(got) != 0 {
		t.Fatalf(".tmp holds %v", got)
	}
	if err := d.Install(context.Background(), rel, func(string) {}); !errors.Is(err, ErrAlreadyInstalled) {
		t.Fatalf("second install: %v", err)
	}
	got, err := d.Installed()
	if err != nil || len(got) != 1 || got[0].Version != "8.0.414" || got[0].Path != filepath.Join(f.Root, "dotnet", "sdk", "8.0.414") {
		t.Fatalf("installed %+v, %v", got, err)
	}
}

func TestDotnetFailedInstallRemovesOnlyWhatItCreated(t *testing.T) {
	f := dotnetFixture(t)
	f.host.scripts["dotnet-install.sh"] = func(dir string, env map[string]string, args []string) error {
		touch(t, filepath.Join(f.Root, "dotnet", "sdk", args[1], "partial"))
		return errors.New("download failed")
	}
	d := newDotnet(f.Env)
	rel, _ := d.Resolve(context.Background(), "8.0")
	if err := d.Install(context.Background(), rel, func(string) {}); err == nil {
		t.Fatal("no error")
	}
	if exists(filepath.Join(f.Root, "dotnet", "sdk", "8.0.414")) {
		t.Fatal("the SDK folder this install created survived")
	}
	touch(t, filepath.Join(f.Root, "dotnet", "sdk", "10.0.105", "old"))
	rel, _ = d.Resolve(context.Background(), "10.0")
	if err := d.Install(context.Background(), rel, func(string) {}); err == nil {
		t.Fatal("no error")
	}
	if !exists(filepath.Join(f.Root, "dotnet", "sdk", "10.0.105", "old")) {
		t.Fatal("an SDK folder that existed before the install was removed")
	}
}

func dotnetTree(t *testing.T, root string, sdks ...string) {
	touch(t, filepath.Join(root, "dotnet"))
	for _, s := range sdks {
		touch(t, filepath.Join(root, "sdk", s, "dotnet.dll"))
	}
	for _, major := range []string{"8", "10"} {
		for _, p := range []string{"shared/Microsoft.NETCore.App/%s.0.20", "shared/Microsoft.AspNetCore.App/%s.0.20", "packs/Microsoft.NETCore.App.Ref/%s.0.20",
			"host/fxr/%s.0.20", "templates/%s.0.20", "sdk-manifests/%s.0.100", "metadata/workloads/%s.0.100", "library-packs/%s.0.100"} {
			touch(t, filepath.Join(root, filepath.FromSlash(fmt.Sprintf(p, major)), "x"))
		}
	}
}

func TestDotnetRemoveDropsTheMajorWithItsLastSDK(t *testing.T) {
	f := dotnetFixture(t)
	root := filepath.Join(f.Root, "dotnet")
	dotnetTree(t, root, "8.0.414", "10.0.105")
	if err := newDotnet(f.Env).Remove("8.0.414"); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"sdk/8.0.414", "shared/Microsoft.NETCore.App/8.0.20", "shared/Microsoft.AspNetCore.App/8.0.20",
		"packs/Microsoft.NETCore.App.Ref/8.0.20", "host/fxr/8.0.20", "templates/8.0.20", "sdk-manifests/8.0.100", "metadata/workloads/8.0.100", "library-packs/8.0.100"} {
		if exists(filepath.Join(root, filepath.FromSlash(gone))) {
			t.Errorf("%s survived", gone)
		}
	}
	for _, kept := range []string{"dotnet", "sdk/10.0.105", "shared/Microsoft.NETCore.App/10.0.20", "host/fxr/10.0.20"} {
		if !exists(filepath.Join(root, filepath.FromSlash(kept))) {
			t.Errorf("%s was removed", kept)
		}
	}
}

func TestDotnetRemoveKeepsTheMajorWhileAnotherSDKUsesIt(t *testing.T) {
	f := dotnetFixture(t)
	root := filepath.Join(f.Root, "dotnet")
	dotnetTree(t, root, "8.0.414", "8.0.311")
	d := newDotnet(f.Env)
	if err := d.Remove("8.0.414"); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(root, "sdk", "8.0.414")) || !exists(filepath.Join(root, "shared", "Microsoft.NETCore.App", "8.0.20")) {
		t.Fatal("only the SDK itself may go while 8.0.311 remains")
	}
	touch(t, filepath.Join(root, "sdk", "preview", "dotnet.dll"))
	for _, bad := range []string{"9.0.100", "..", "8.0.414", "preview"} {
		if err := d.Remove(bad); !errors.Is(err, ErrNotInstalled) {
			t.Errorf("remove %q: %v", bad, err)
		}
	}
	if !exists(filepath.Join(root, "shared", "Microsoft.NETCore.App", "10.0.20")) {
		t.Error("removing a folder that is not a version took the runtimes with it")
	}
}
