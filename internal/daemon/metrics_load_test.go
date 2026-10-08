package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/metrics"
)

func TestLoadMetricsWarnsOnAMalformedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := metrics.NewSampler(func() metrics.Snapshot { return metrics.Snapshot{} }, func() int { return 0 })
	s.Path = path
	ev := events.New()
	loadMetrics(s, ev)
	got := ev.After(0)
	if len(got) != 1 || got[0].Level != "warn" || !strings.Contains(got[0].Msg, "metrics history unreadable") {
		t.Fatalf("events %+v", got)
	}
}

func TestDefaultMetricsPath(t *testing.T) {
	if p := DefaultOptions().MetricsPath; p != "/var/lib/ghr/metrics.json" {
		t.Fatalf("MetricsPath = %q", p)
	}
}
