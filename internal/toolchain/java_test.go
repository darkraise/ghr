package toolchain

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
)

const javaQuery = "/v3/assets/feature_releases/21/ga?architecture=x64&os=linux&image_type=jdk&jvm_impl=hotspot&vendor=eclipse&page_size=1&sort_order=DESC"

func javaFixture(t *testing.T) (*fixture, []byte) {
	f := newFixture(t)
	archive := tarGz(map[string]string{"jdk-21.0.12.1+1/bin/java": "java"})
	f.serve("/v3/info/available_releases", []byte(`{"available_lts_releases":[8,11,17,21,25],"available_releases":[8,11,17,21,24,25],"most_recent_feature_release":25}`))
	f.serve(javaQuery, []byte(fmt.Sprintf(`[{"release_name":"jdk-21.0.12.1+1","version_data":{"semver":"21.0.12+101.0.LTS"},
	  "binaries":[{"package":{"name":"OpenJDK21U-jdk_x64_linux_hotspot.tar.gz","link":%q,"checksum":%q}}]}]`, f.url+"/jdk.tar.gz", sum256(archive))))
	f.serve("/jdk.tar.gz", archive)
	return f, archive
}

func TestJavaAvailableListsMajors(t *testing.T) {
	f, _ := javaFixture(t)
	cs, err := newJava(f.Env).Available(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Choice{{"25", "25", true}, {"24", "24", false}, {"21", "21", true}, {"17", "17", true}, {"11", "11", true}, {"8", "8", true}}
	if !reflect.DeepEqual(cs, want) {
		t.Fatalf("available %+v", cs)
	}
}

func TestJavaResolveUsesTheSemverFolder(t *testing.T) {
	f, archive := javaFixture(t)
	rel, err := newJava(f.Env).Resolve(context.Background(), "21")
	if err != nil {
		t.Fatal(err)
	}
	want := Release{Tool: "java", Version: "21.0.12+101.0.LTS", Folder: "21.0.12-101.0.LTS", URL: f.url + "/jdk.tar.gz", SHA256: sum256(archive)}
	if rel != want {
		t.Fatalf("resolved %+v", rel)
	}
	for _, bad := range []string{"21.0.8", "latest", "0"} {
		if _, err := newJava(f.Env).Resolve(context.Background(), bad); err == nil {
			t.Errorf("%q resolved", bad)
		}
	}
	if _, err := newJava(f.Env).Resolve(context.Background(), "17"); err == nil {
		t.Error("a major the source lists nothing for resolved")
	}
}

func TestJavaInstallIsFoundBySetupJava(t *testing.T) {
	f, _ := javaFixture(t)
	j := newJava(f.Env)
	rel, err := j.Resolve(context.Background(), "21")
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Install(context.Background(), rel, func(string) {}); err != nil {
		t.Fatal(err)
	}
	dir := javaFind(f.Root, "21")
	if dir != filepath.Join(f.Root, "Java_Temurin-Hotspot_jdk", "21.0.12-101.0.LTS", "x64") || !exists(filepath.Join(dir, "bin", "java")) {
		t.Fatalf("setup-java would find %q", dir)
	}
	got, err := j.Installed()
	if err != nil || len(got) != 1 || got[0].Version != "21.0.12+101.0.LTS" || got[0].Tool != "java" {
		t.Fatalf("installed %+v, %v", got, err)
	}
	if err := j.Remove("21.0.12+101.0.LTS"); err != nil {
		t.Fatal(err)
	}
	if javaFind(f.Root, "21") != "" {
		t.Fatal("removed version still found")
	}
	if err := j.Remove("21.0.12+101.0.LTS"); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("second remove: %v", err)
	}
}
