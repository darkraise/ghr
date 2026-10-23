package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestJobsDefaultWhenAbsent(t *testing.T) {
	c, warnings, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings %v", warnings)
	}
	want := Jobs{
		GhrUpdateCheck:    Job{Enabled: true, Every: Duration(24 * time.Hour)},
		RunnerUpdateCheck: Job{Enabled: true, Every: Duration(24 * time.Hour)},
		Prune:             Job{Enabled: true, Every: Duration(24 * time.Hour)},
		StorageMeasure:    Job{Enabled: true, Every: Duration(30 * time.Minute)},
		PackageIndex:      Job{Enabled: true, Every: Duration(24 * time.Hour)},
	}
	if c.Jobs != want || DefaultJobs() != want {
		t.Fatalf("jobs %+v, defaults %+v", c.Jobs, DefaultJobs())
	}
}

func TestJobsPartialBlockKeepsTheRest(t *testing.T) {
	c, _, err := Parse([]byte(sample + "jobs:\n  prune:\n    every: 12h\n  storage_measure:\n    enabled: false\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := DefaultJobs()
	want.Prune.Every = Duration(12 * time.Hour)
	want.StorageMeasure.Enabled = false
	if c.Jobs != want {
		t.Fatalf("jobs %+v, want %+v", c.Jobs, want)
	}
}

func TestJobsMinimums(t *testing.T) {
	for _, tc := range []struct{ yaml, want string }{
		{"jobs:\n  ghr_update_check:\n    every: 59m\n", "jobs.ghr_update_check.every must be >= 1h"},
		{"jobs:\n  runner_update_check:\n    every: 0s\n", "jobs.runner_update_check.every must be >= 1h"},
		{"jobs:\n  prune:\n    every: -1h\n", "jobs.prune.every must be >= 1h"},
		{"jobs:\n  storage_measure:\n    every: 4m\n", "jobs.storage_measure.every must be >= 5m"},
		{"jobs:\n  package_index:\n    every: 30m\n", "jobs.package_index.every must be >= 1h"},
	} {
		if _, _, err := Parse([]byte(sample + tc.yaml)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: err %v, want %q", tc.yaml, err, tc.want)
		}
	}
	if _, _, err := Parse([]byte(sample + "jobs:\n  prune:\n    every: 1h\n  storage_measure:\n    every: 5m\n  package_index:\n    every: 1h\n")); err != nil {
		t.Fatalf("the floors are inclusive: %v", err)
	}
}

func TestJobsOffWarns(t *testing.T) {
	_, warnings, err := Parse([]byte(sample + "jobs:\n  runner_update_check:\n    enabled: false\n  prune:\n    enabled: false\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{
		"jobs.runner_update_check is off: GitHub stops assigning jobs to a runner past its forced-update deadline, and ghr no longer learns of new runner releases or deadlines",
		"jobs.prune is off: history and logs older than history_retention are kept, and a full disk is no longer pruned automatically",
	} {
		if !slices.Contains(warnings, w) {
			t.Errorf("missing warning %q in %v", w, warnings)
		}
	}
}

func TestJobsRoundTrip(t *testing.T) {
	c, _, err := Parse([]byte(sample + "jobs:\n  prune:\n    every: 12h\n"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	back, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.Jobs != c.Jobs {
		t.Fatalf("yaml round trip %+v, want %+v", back.Jobs, c.Jobs)
	}
	data, err := json.Marshal(c.Jobs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"prune":{"enabled":true,"every":"12h0m0s"}`) {
		t.Fatalf("json %s", data)
	}
	var j Jobs
	if err := json.Unmarshal(data, &j); err != nil || j != c.Jobs {
		t.Fatalf("json round trip %+v err %v", j, err)
	}
}

func TestJobsLookup(t *testing.T) {
	j := DefaultJobs()
	for _, name := range JobNames {
		if _, ok := j.Get(name); !ok || j.Ref(name) == nil {
			t.Errorf("%s does not resolve", name)
		}
	}
	if _, ok := j.Get("nightly"); ok || j.Ref("nightly") != nil {
		t.Fatal("an unknown task resolved")
	}
	j.Ref(JobPrune).Enabled = false
	if j.Prune.Enabled {
		t.Fatal("Ref did not point into the block")
	}
}

// The example config shows the jobs block commented out, with the defaults;
// uncommented, it must parse to exactly those.
func TestExampleConfigDocumentsTheJobsBlock(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "deploy", "config.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var block []string
	in := false
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "# jobs:"):
			in = true
		case in && strings.HasPrefix(line, "#   "):
		default:
			in = false
		}
		if in {
			block = append(block, strings.TrimPrefix(line, "# "))
		}
	}
	if len(block) != 6 {
		t.Fatalf("want a commented jobs block of 6 lines, got %q", block)
	}
	c, _, err := Parse([]byte(sample + strings.Join(block, "\n") + "\n"))
	if err != nil {
		t.Fatalf("the jobs block does not parse: %v\n%s", err, strings.Join(block, "\n"))
	}
	if c.Jobs != DefaultJobs() {
		t.Fatalf("the jobs block reads %+v, want the defaults %+v", c.Jobs, DefaultJobs())
	}
}

// Releases before the jobs block refuse an unknown key, so a config that
// keeps the defaults must not grow one: rolling ghr back would crash-loop.
func TestSaveLeavesOutDefaultJobs(t *testing.T) {
	c, _, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "jobs:") {
		t.Fatalf("a default schedule was written:\n%s", data)
	}

	c.Jobs.Prune.Every = Duration(48 * time.Hour)
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	back, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.Jobs != c.Jobs {
		t.Fatalf("saved %+v, read back %+v", c.Jobs, back.Jobs)
	}
}

func TestCloneKeepsDefaultJobs(t *testing.T) {
	c, _, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Clone().Jobs; got != DefaultJobs() {
		t.Fatalf("clone has %+v, want the defaults", got)
	}
}
