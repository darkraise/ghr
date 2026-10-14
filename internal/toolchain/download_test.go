package toolchain

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

const (
	realNodeURL   = "https://github.com/actions/node-versions/releases/download/22.11.0-11593095476/node-22.11.0-linux-x64.tar.gz"
	realPythonURL = "https://github.com/actions/python-versions/releases/download/3.13.7-16980743123/python-3.13.7-linux-24.04-x64.tar.gz"
	realJDK21URL  = "https://github.com/adoptium/temurin21-binaries/releases/download/jdk-21.0.12.1%2B1/OpenJDK21U-jdk_x64_linux_hotspot_21.0.12.1_1.tar.gz"
	realJDK8URL   = "https://github.com/adoptium/temurin8-binaries/releases/download/jdk8u504-b01/OpenJDK8U-jdk_x64_linux_hotspot_8u504b01.tar.gz"
	realGoURL     = "https://go.dev/dl/go1.25.1.linux-amd64.tar.gz"
	realDotnetURL = "https://builds.dotnet.microsoft.com/dotnet/scripts/v1/dotnet-install.sh"
)

var errFetched = errors.New("fetched")

// strictInstall runs tool's installer on url in an Env straight from NewEnv,
// so with the download check the daemon uses, and returns what reached Fetch.
func strictInstall(t *testing.T, tool, version, url string, sources func(*Sources)) ([]string, error) {
	t.Helper()
	var fetched []string
	fetch := func(_ context.Context, url string, _ io.Writer) error {
		fetched = append(fetched, url)
		return errFetched
	}
	e := NewEnv(t.TempDir(), "/home/ghrunner", "ghrunner", (&host{}).run, fetch)
	if sources != nil {
		sources(&e.Sources)
	}
	in, err := New(e).Get(tool)
	if err != nil {
		t.Fatal(err)
	}
	rel := Release{Tool: tool, Version: version, Folder: strings.Replace(version, "+", "-", 1), URL: url}
	err = in.Install(context.Background(), rel, func(string) {})
	if got := tmpEntries(t, e.Root); len(got) != 0 {
		t.Errorf("%s %s: .tmp holds %v", tool, url, got)
	}
	return fetched, err
}

func wantRefused(t *testing.T, what, url string, fetched []string, err error) {
	t.Helper()
	if err == nil || err.Error() != "refusing to download "+url {
		t.Errorf("%s %q: got %v, want it refused", what, url, err)
	}
	if len(fetched) != 0 {
		t.Errorf("%s %q: fetched %q", what, url, fetched)
	}
}

func TestInstallDownloadsOnlyFromItsOwnToolsReleases(t *testing.T) {
	cases := []struct {
		tool, version, url string
		refused            bool
	}{
		{"node", "22.11.0", realNodeURL, false},
		{"node", "22.11.0", "http://github.com/actions/node-versions/releases/download/22.11.0-11593095476/node-22.11.0-linux-x64.tar.gz", true},
		{"node", "22.11.0", "https://example.net/actions/node-versions/releases/download/22.11.0-11593095476/node-22.11.0-linux-x64.tar.gz", true},
		{"node", "22.11.0", "https://github.com/evil/node-versions/releases/download/22.11.0-11593095476/node-22.11.0-linux-x64.tar.gz", true},
		{"node", "22.11.0", "http://127.0.0.1:8080/node-22.11.0-linux-x64.tar.gz", true},
		{"node", "22.11.0", realPythonURL, true},
		{"node", "22.11.0", realJDK21URL, true},
		{"python", "3.13.7", realPythonURL, false},
		{"python", "3.13.7", "http://github.com/actions/python-versions/releases/download/3.13.7-16980743123/python-3.13.7-linux-24.04-x64.tar.gz", true},
		{"python", "3.13.7", "https://example.net/actions/python-versions/releases/download/3.13.7-16980743123/python-3.13.7-linux-24.04-x64.tar.gz", true},
		{"python", "3.13.7", realNodeURL, true},
		{"java", "21.0.12+101.0.LTS", realJDK21URL, false},
		{"java", "8.0.504+1", realJDK8URL, false},
		{"java", "21.0.12+101.0.LTS", "http://github.com/adoptium/temurin21-binaries/releases/download/jdk-21.0.12.1%2B1/OpenJDK21U-jdk_x64_linux_hotspot_21.0.12.1_1.tar.gz", true},
		{"java", "21.0.12+101.0.LTS", "https://api.adoptium.net/adoptium/temurin21-binaries/releases/download/jdk-21.0.12.1%2B1/OpenJDK21U-jdk_x64_linux_hotspot_21.0.12.1_1.tar.gz", true},
		{"java", "21.0.12+101.0.LTS", "https://github.com/evil/temurin21-binaries/releases/download/jdk-21.0.12.1%2B1/OpenJDK21U-jdk_x64_linux_hotspot_21.0.12.1_1.tar.gz", true},
		{"java", "21.0.12+101.0.LTS", realJDK8URL, true},
		{"java", "x.0.1", "https://github.com/adoptium/temurinx-binaries/releases/download/jdk-x/OpenJDK.tar.gz", true},
		{"java", ".0.1", "https://github.com/adoptium/temurin-binaries/releases/download/jdk/OpenJDK.tar.gz", true},
		{"java", "21.0.12+101.0.LTS", realNodeURL, true},
		{"go", "1.25.1", realGoURL, false},
		{"go", "1.25.1", "http://go.dev/dl/go1.25.1.linux-amd64.tar.gz", true},
		{"go", "1.25.1", "https://github.com/actions/go-versions/releases/download/1.25.1-17451174567/go-1.25.1-linux-x64.tar.gz", true},
		{"go", "1.25.1", "https://go.dev/play/go1.25.1.linux-amd64.tar.gz", true},
		{"dotnet", "8.0.414", realDotnetURL, false},
		{"dotnet", "8.0.414", "http://builds.dotnet.microsoft.com/dotnet/scripts/v1/dotnet-install.sh", true},
		{"dotnet", "8.0.414", "https://github.com/dotnet/install-scripts/raw/main/src/dotnet-install.sh", true},
		{"dotnet", "8.0.414", "https://builds.dotnet.microsoft.com/dotnet/scripts/v2/dotnet-install.sh", true},
		{"dotnet", "8.0.414", realDotnetURL + ".other", true},
		{"dotnet", "8.0.414", realDotnetURL + "/another-script", true},
		{"dotnet", "8.0.414", realDotnetURL + "/", true},
		{"dotnet", "8.0.414", "https://builds.dotnet.microsoft.com/dotnet/scripts/v1/dotnet-install", true},
		{"dotnet", "8.0.414", "https://user@builds.dotnet.microsoft.com/dotnet/scripts/v1/dotnet-install.sh", true},
		{"dotnet", "8.0.414", "https://builds.dotnet.microsoft.com:443/dotnet/scripts/v1/dotnet-install.sh", true},
		{"dotnet", "8.0.414", realDotnetURL + "?x=1", true},
		{"dotnet", "8.0.414", realDotnetURL + "#x", true},
	}
	for _, c := range cases {
		fetched, err := strictInstall(t, c.tool, c.version, c.url, nil)
		if c.refused {
			wantRefused(t, c.tool, c.url, fetched, err)
			continue
		}
		if !errors.Is(err, errFetched) || !slices.Equal(fetched, []string{c.url}) {
			t.Errorf("%s %s: got %v after fetching %q, want exactly that URL fetched", c.tool, c.url, err, fetched)
		}
	}
}

func TestInstallRefusesAURLThatIsNotPlain(t *testing.T) {
	// Adoptium's links carry an encoded "+", so an escape as such is fine.
	plain := "https://github.com/actions/node-versions/releases/download/a%2Bb/node.tar.gz"
	if fetched, err := strictInstall(t, "node", "22.11.0", plain, nil); !errors.Is(err, errFetched) || !slices.Equal(fetched, []string{plain}) {
		t.Errorf("%s: got %v after fetching %q, want exactly that URL fetched", plain, err, fetched)
	}
	for _, u := range []string{
		"https://GitHub.com/actions/node-versions/releases/download/1/node.tar.gz",
		"HTTPS://github.com/actions/node-versions/releases/download/1/node.tar.gz",
		"https://github.com@evil.example/actions/node-versions/releases/download/1/node.tar.gz",
		"https://user@github.com/actions/node-versions/releases/download/1/node.tar.gz",
		"https://user:pw@github.com/actions/node-versions/releases/download/1/node.tar.gz",
		"https://github.com:443/actions/node-versions/releases/download/1/node.tar.gz",
		"https://github.com:/actions/node-versions/releases/download/1/node.tar.gz",
		"https://github.com./actions/node-versions/releases/download/1/node.tar.gz",
		"https://github.com.evil.example/actions/node-versions/releases/download/1/node.tar.gz",
		"https://evilgithub.com/actions/node-versions/releases/download/1/node.tar.gz",
		"https:///actions/node-versions/releases/download/1/node.tar.gz",
		"https:github.com/actions/node-versions/releases/download/1/node.tar.gz",
		"//github.com/actions/node-versions/releases/download/1/node.tar.gz",
		"https://github.com/actions/node-versions/releases/downloads/1/node.tar.gz",
		"https://github.com/actions/node-versions/releases/download",
		"https://github.com/actions/node-versions/releases/download/../../../evil/repo/releases/download/1/node.tar.gz",
		"https://github.com/actions/node-versions/releases/download/1/../node.tar.gz",
		"https://github.com/actions/node-versions/releases/download/1/./node.tar.gz",
		"https://github.com/actions/node-versions/releases/download//node.tar.gz",
		"https://github.com//actions/node-versions/releases/download/1/node.tar.gz",
		`https://github.com/actions/node-versions/releases/download/1\node.tar.gz`,
		"https://github.com/actions/node-versions/releases/download/1%5Cnode.tar.gz",
		"https://github.com/actions/node-versions/releases/download/%2e%2e/%2e%2e/node.tar.gz",
		"https://github.com/actions/node-versions/releases/download/%2E%2E/node.tar.gz",
		"https://github.com/actions/node-versions/releases/download/1/node%2Etar.gz",
		"https://github.com/actions/node-versions/releases/download/1%2fnode.tar.gz",
		"https://github.com/actions/node-versions/releases/download%2F1/node.tar.gz",
		"https://github.com/actions/node-versions/releases/download/1/node.tar.gz#x",
		"https://github.com/actions/node-versions/releases/download/1/node.tar.gz#",
		"https://github.com/actions/node-versions/releases/download/1/node.tar.gz?x=1",
		"https://github.com/actions/node-versions/releases/download/1/node.tar.gz?",
		" https://github.com/actions/node-versions/releases/download/1/node.tar.gz",
		"",
	} {
		fetched, err := strictInstall(t, "node", "22.11.0", u, nil)
		wantRefused(t, "node", u, fetched, err)
	}
}

// A source pointed at a local server, as this package's other tests do, must
// not pass the check NewEnv sets.
func TestNewEnvRefusesALocalSource(t *testing.T) {
	local := func(s *Sources) {
		s.GoDownload = "http://127.0.0.1:8080/dl/"
		s.DotnetScript = "http://127.0.0.1:8080/dotnet-install.sh"
	}
	fetched, err := strictInstall(t, "go", "1.25.1", "http://127.0.0.1:8080/dl/go1.25.1.linux-amd64.tar.gz", local)
	wantRefused(t, "go", "http://127.0.0.1:8080/dl/go1.25.1.linux-amd64.tar.gz", fetched, err)
	fetched, err = strictInstall(t, "dotnet", "8.0.414", "http://127.0.0.1:8080/dotnet-install.sh", local)
	wantRefused(t, "dotnet", "http://127.0.0.1:8080/dotnet-install.sh", fetched, err)
	tls := func(s *Sources) {
		s.GoDownload = "https://127.0.0.1:8080/dl/"
		s.DotnetScript = "https://127.0.0.1:8080/dotnet-install.sh"
	}
	fetched, err = strictInstall(t, "go", "1.25.1", "https://127.0.0.1:8080/dl/go1.25.1.linux-amd64.tar.gz", tls)
	wantRefused(t, "go", "https://127.0.0.1:8080/dl/go1.25.1.linux-amd64.tar.gz", fetched, err)
	fetched, err = strictInstall(t, "dotnet", "8.0.414", "https://127.0.0.1:8080/dotnet-install.sh", tls)
	wantRefused(t, "dotnet", "https://127.0.0.1:8080/dotnet-install.sh", fetched, err)
}

// A directory given without its closing slash would also cover its siblings
// ("/dl" is a prefix of "/dlx/"), so it is no directory at all.
func TestInstallRefusesADirectoryWithoutItsSlash(t *testing.T) {
	noSlash := func(s *Sources) { s.GoDownload = "https://go.dev/dl" }
	for _, u := range []string{
		"https://go.dev/dl/go1.25.1.linux-amd64.tar.gz",
		"https://go.dev/dlx/go1.25.1.linux-amd64.tar.gz",
		"https://go.dev/dl",
	} {
		fetched, err := strictInstall(t, "go", "1.25.1", u, noSlash)
		wantRefused(t, "go", u, fetched, err)
	}
}

// The one file an installer may fetch is held to the same shape rules as a
// URL below a directory, even when the configured URL breaks them itself.
func TestInstallRefusesAConfiguredFileThatIsNotPlain(t *testing.T) {
	for _, script := range []string{
		realDotnetURL + "?x=1",
		realDotnetURL + "#x",
		"https://user@builds.dotnet.microsoft.com/dotnet/scripts/v1/dotnet-install.sh",
		"https://builds.dotnet.microsoft.com/dotnet/scripts/v1/../v1/dotnet-install.sh",
	} {
		fetched, err := strictInstall(t, "dotnet", "8.0.414", script, func(s *Sources) { s.DotnetScript = script })
		wantRefused(t, "dotnet", script, fetched, err)
	}
}
