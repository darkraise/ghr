package config

import (
	"encoding/json"
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
	} {
		if _, _, err := Parse([]byte(sample + tc.yaml)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: err %v, want %q", tc.yaml, err, tc.want)
		}
	}
	if _, _, err := Parse([]byte(sample + "jobs:\n  prune:\n    every: 1h\n  storage_measure:\n    every: 5m\n")); err != nil {
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
