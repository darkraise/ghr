package toolchain

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const javaDir = "Java_Temurin-Hotspot_jdk"

type java struct{ e *Env }

func newJava(e *Env) Installer { return java{e} }

func (j java) Available(ctx context.Context) ([]Choice, error) {
	b, err := j.e.Get(ctx, j.e.Sources.Adoptium+"/v3/info/available_releases")
	if err != nil {
		return nil, fmt.Errorf("Java versions: %w", err)
	}
	var r struct {
		Releases []int `json:"available_releases"`
		LTS      []int `json:"available_lts_releases"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("Java versions: %w", err)
	}
	lts := map[int]bool{}
	for _, v := range r.LTS {
		lts[v] = true
	}
	sort.Sort(sort.Reverse(sort.IntSlice(r.Releases)))
	out := []Choice{}
	for _, v := range r.Releases {
		s := strconv.Itoa(v)
		out = append(out, Choice{Spec: s, Version: s, LTS: lts[v]})
	}
	return out, nil
}

// Resolve takes a major version only and returns its newest GA Temurin JDK.
// The folder is version_data.semver with its first "+" turned into "-", as
// setup-java names it; the release name can have four parts, which is not
// semver.
func (j java) Resolve(ctx context.Context, spec string) (Release, error) {
	major, err := strconv.Atoi(spec)
	if err != nil || major <= 0 {
		return Release{}, fmt.Errorf("Java installs by major version, such as 21; got %q", spec)
	}
	url := fmt.Sprintf("%s/v3/assets/feature_releases/%d/ga?architecture=x64&os=linux&image_type=jdk&jvm_impl=hotspot&vendor=eclipse&page_size=1&sort_order=DESC", j.e.Sources.Adoptium, major)
	b, err := j.e.Get(ctx, url)
	if err != nil {
		return Release{}, fmt.Errorf("Java %d: %w", major, err)
	}
	var rs []struct {
		VersionData struct {
			Semver string `json:"semver"`
		} `json:"version_data"`
		Binaries []struct {
			Package struct {
				Link     string `json:"link"`
				Checksum string `json:"checksum"`
			} `json:"package"`
		} `json:"binaries"`
	}
	if err := json.Unmarshal(b, &rs); err != nil {
		return Release{}, fmt.Errorf("Java %d: %w", major, err)
	}
	if len(rs) == 0 || len(rs[0].Binaries) == 0 || rs[0].VersionData.Semver == "" {
		return Release{}, fmt.Errorf("Adoptium lists no GA JDK %d for linux x64", major)
	}
	sv, pkg := rs[0].VersionData.Semver, rs[0].Binaries[0].Package
	if pkg.Link == "" || pkg.Checksum == "" {
		return Release{}, fmt.Errorf("Adoptium lists no download link or checksum for JDK %d", major)
	}
	return Release{Tool: "java", Version: sv, Folder: strings.Replace(sv, "+", "-", 1), URL: pkg.Link, SHA256: pkg.Checksum}, nil
}

func (j java) Install(ctx context.Context, rel Release, progress func(string)) error {
	return j.e.installArchive(ctx, javaDir, rel, progress)
}

func (j java) Installed() ([]Installed, error) {
	return j.e.listLayout("java", javaDir, func(folder string) string { return strings.Replace(folder, "-", "+", 1) })
}

func (j java) Remove(version string) error {
	return j.e.removeLayout(javaDir, strings.Replace(version, "+", "-", 1))
}
