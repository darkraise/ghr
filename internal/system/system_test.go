package system

import (
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

type call struct {
	name string
	args []string
}

func (c call) String() string { return c.name + " " + strings.Join(c.args, " ") }

// fake returns outputs by command-line prefix and records every call.
func fake(outputs map[string]string) (Runner, *[]call) {
	var calls []call
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		c := call{name, args}
		calls = append(calls, c)
		for prefix, out := range outputs {
			if strings.HasPrefix(c.String(), prefix) {
				if strings.HasPrefix(out, "ERR:") {
					return nil, errors.New(out[4:])
				}
				return []byte(out), nil
			}
		}
		return nil, nil
	}, &calls
}

func TestSystemdStartArgs(t *testing.T) {
	run, calls := fake(nil)
	err := Systemd{Run: run}.Start(context.Background(), UnitSpec{
		Unit: "ghr-runner-abc123", User: "ghrunner", WorkDir: "/var/lib/ghr/instances/abc123",
		Props: []string{"MemoryMax=6G"}, Env: map[string]string{"B": "2", "A": "1"},
		Command: []string{"/x/run.sh", "--jitconfig", "ENC"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "systemd-run --unit=ghr-runner-abc123 --description=ghr runner abc123 --uid=ghrunner --gid=ghrunner --collect --quiet " +
		"--working-directory=/var/lib/ghr/instances/abc123 --property=MemoryMax=6G --setenv=A=1 --setenv=B=2 -- /x/run.sh --jitconfig ENC"
	if got := (*calls)[0].String(); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

// Without --description systemd describes the unit by its command line, which
// carries the JIT credential into list-units and the journal.
func TestSystemdStartDescriptionHidesCredential(t *testing.T) {
	run, calls := fake(nil)
	const secret = "SECRET-JIT-CONFIG"
	if err := (Systemd{Run: run}).Start(context.Background(), UnitSpec{
		Unit: "ghr-runner-f00d42", User: "ghrunner", Command: []string{"/x/run.sh", "--jitconfig", secret},
	}); err != nil {
		t.Fatal(err)
	}
	args := (*calls)[0].args
	sep := slices.Index(args, "--")
	if sep < 0 || !slices.Contains(args[:sep], "--description=ghr runner f00d42") {
		t.Fatalf("no description in %q", args)
	}
	for _, a := range args[:sep] {
		if strings.Contains(a, secret) {
			t.Fatalf("credential in systemd-run option %q", a)
		}
	}
}

func TestSystemdActiveAndList(t *testing.T) {
	run, _ := fake(map[string]string{
		"systemctl is-active ghr-runner-aaaaaa": "active\n",
		"systemctl is-active ghr-runner-bbbbbb": "inactive\n",
		"systemctl list-units":                  "ghr-runner-aaaaaa.service loaded active running x\nghr-runner-cccccc.service loaded failed failed y\n",
	})
	s := Systemd{Run: run}
	if ok, _ := s.Active(context.Background(), "ghr-runner-aaaaaa"); !ok {
		t.Fatal("aaaaaa should be active")
	}
	if ok, err := s.Active(context.Background(), "ghr-runner-bbbbbb"); ok || err != nil {
		t.Fatalf("bbbbbb should be confirmed inactive, got %v %v", ok, err)
	}
	units, err := s.List(context.Background(), "ghr-runner-")
	if err != nil || len(units) != 1 || units[0] != "ghr-runner-aaaaaa" {
		t.Fatalf("units = %v err = %v", units, err)
	}
}

func TestDockerParsing(t *testing.T) {
	run, calls := fake(map[string]string{
		"docker ps -a --no-trunc --filter label=com.docker.compose.project.working_dir": "c1\tghr-abc123\t/var/lib/ghr/instances/abc123/_work/r/r\n",
		"docker ps -a --no-trunc --format":                                              "c2\tdc-e2e-web\nc3\tother\n",
		"docker network ls":                                                             "n1\nn2\n",
		"docker volume ls":                                                              "",
		"docker info":                                                                   "/var/lib/docker\n",
		"df --output=pcent /var/lib/docker":                                             "Use%\n 81%\n",
		"docker builder prune --help":                                                   "Options:\n      --reserved-space bytes   Amount of disk space always allowed to keep for cache\n",
		"docker builder prune -f --reserved-space":                                      "ID\nTotal:\t6.2GB\n",
	})
	d := Docker{Run: run}
	ctx := context.Background()
	cc, _ := d.ComposeContainers(ctx)
	if len(cc) != 1 || cc[0].Project != "ghr-abc123" || !strings.HasSuffix(cc[0].WorkingDir, "/r/r") {
		t.Fatalf("compose = %+v", cc)
	}
	nc, _ := d.Containers(ctx)
	if len(nc) != 2 || nc[0].Name != "dc-e2e-web" {
		t.Fatalf("containers = %+v", nc)
	}
	if err := d.RemoveNetworksByLabel(ctx, "com.docker.compose.project=p"); err != nil {
		t.Fatal(err)
	}
	if err := d.RemoveVolumesByLabel(ctx, "com.docker.compose.project=p"); err != nil {
		t.Fatal(err)
	}
	pct, err := d.DataRootUsage(ctx)
	if err != nil || pct != 81 {
		t.Fatalf("pct = %d err = %v", pct, err)
	}
	freed, err := d.PruneBuildCacheTo(ctx, "20GB")
	if err != nil || freed != "6.2GB" {
		t.Fatalf("freed = %q err = %v", freed, err)
	}
	var cmds []string
	for _, c := range *calls {
		cmds = append(cmds, c.String())
	}
	joined := strings.Join(cmds, "\n")
	if !strings.Contains(joined, "docker network rm n1 n2") {
		t.Fatalf("networks not removed:\n%s", joined)
	}
	if strings.Contains(joined, "docker volume rm") {
		t.Fatalf("volume rm called with nothing to remove:\n%s", joined)
	}
	if !strings.Contains(joined, "docker builder prune -f --reserved-space 20GB") {
		t.Fatalf("prune flag wrong:\n%s", joined)
	}
}

func TestPruneWithoutReservedSpaceUsesKeepStorage(t *testing.T) {
	run, calls := fake(map[string]string{"docker builder prune --help": "Options:\n      --keep-storage bytes   Amount of disk space to keep for cache\n"})
	if _, err := (Docker{Run: run}).PruneBuildCacheTo(context.Background(), "20GB"); err != nil {
		t.Fatal(err)
	}
	if got := (*calls)[1].String(); got != "docker builder prune -f --keep-storage 20GB" {
		t.Fatalf("got %s", got)
	}
}

func TestExecErrorHidesArguments(t *testing.T) {
	_, err := Exec(context.Background(), "definitely-not-a-command-ghr", "first", "SECRET")
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("err = %v", err)
	}
}

func TestSystemdActiveErrorsAreNotInactive(t *testing.T) {
	run, _ := fake(map[string]string{
		"systemctl is-active ghr-runner-gone00": "ERR:exit status 3",
		"systemctl is-active ghr-runner-bus000": "ERR:Failed to connect to bus",
	})
	s := Systemd{Run: run}
	// A unit that no longer exists prints "inactive" or "unknown" with exit 3.
	gone, _ := fake(map[string]string{"systemctl is-active": "unknown\n"})
	if ok, err := (Systemd{Run: gone}).Active(context.Background(), "ghr-runner-gone00"); ok || err != nil {
		t.Fatalf("unknown unit: %v %v", ok, err)
	}
	if _, err := s.Active(context.Background(), "ghr-runner-bus000"); err == nil {
		t.Fatal("a bus failure with no state must be an error")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Active(ctx, "ghr-runner-gone00"); err == nil {
		t.Fatal("a cancelled context must be an error")
	}
}

func TestSystemdActiveNeedsANormalExitToConfirmInactive(t *testing.T) {
	killed := Systemd{Run: func(context.Context, string, ...string) ([]byte, error) {
		return []byte("inactive\n"), errors.New("signal: killed")
	}}
	if _, err := killed.Active(context.Background(), "ghr-runner-kill00"); err == nil {
		t.Fatal("printed inactive with a failed query must be an error")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
	exited := Systemd{Run: func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
		return Exec(ctx, "sh", "-c", "echo inactive; exit 3")
	}}
	if ok, err := exited.Active(context.Background(), "ghr-runner-gone00"); ok || err != nil {
		t.Fatalf("inactive with exit 3: %v %v", ok, err)
	}
}

func TestProjectContainers(t *testing.T) {
	run, calls := fake(map[string]string{"docker ps": "c1\tghr-abc123-db-1\tpostgres:17\trunning\nbad line\n"})
	got, err := Docker{Run: run}.ProjectContainers(context.Background(), "ghr-abc123")
	if err != nil || len(got) != 1 || got[0] != (ProjectContainer{ID: "c1", Name: "ghr-abc123-db-1", Image: "postgres:17", State: "running"}) {
		t.Fatalf("got %+v err %v", got, err)
	}
	if !strings.Contains((*calls)[0].String(), "--filter label=com.docker.compose.project=ghr-abc123") {
		t.Fatalf("call = %s", (*calls)[0])
	}
}

func TestExecTimesOut(t *testing.T) {
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep not on PATH")
	}
	old := CommandTimeout
	CommandTimeout = 100 * time.Millisecond
	defer func() { CommandTimeout = old }()
	start := time.Now()
	if _, err := Exec(context.Background(), "sleep", "10"); err == nil {
		t.Fatal("expected a timeout error")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("Exec ignored CommandTimeout: %v", time.Since(start))
	}
}

func TestExecTimesOutWhenAChildHoldsOutput(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
	old := CommandTimeout
	CommandTimeout = 100 * time.Millisecond
	defer func() { CommandTimeout = old }()
	start := time.Now()
	if _, err := Exec(context.Background(), "sh", "-c", "sleep 30 & sleep 30"); err == nil {
		t.Fatal("expected a timeout error")
	}
	if time.Since(start) > 15*time.Second {
		t.Fatalf("Exec waited on an inherited pipe: %v", time.Since(start))
	}
}
