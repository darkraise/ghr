package system

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParseSize(t *testing.T) {
	for in, want := range map[string]int64{
		"0B":            0,
		"512B":          512,
		"40.96kB":       40960,
		"6.571GB":       6571000000,
		"5.627GB (85%)": 5627000000,
		"0B (0%)":       0,
		"1.5TB":         1500000000000,
		"2PB":           2000000000000000,
		" 456MB ":       456000000,
	} {
		got, err := ParseSize(in)
		if err != nil || got != want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "GB", "12XB", "abcMB", "1.2.3GB"} {
		if _, err := ParseSize(in); err == nil {
			t.Errorf("ParseSize(%q) accepted", in)
		}
	}
}

func TestDiskUsageReadsTheLXCOutput(t *testing.T) {
	run, calls := fake(map[string]string{"docker system df --format json": readFixture(t, "df.jsonl")})
	rows, err := Docker{Run: run}.DiskUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []DiskRow{
		{Type: "Images", Count: 9, Active: 2, Bytes: 6571000000, Reclaimable: 5627000000},
		{Type: "Containers", Count: 2, Active: 2, Bytes: 40960, Reclaimable: 0},
		{Type: "Local Volumes", Count: 68, Active: 1, Bytes: 8650000000, Reclaimable: 8650000000},
		{Type: "Build Cache", Count: 0, Active: 0, Bytes: 0, Reclaimable: 0},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows %+v", rows)
	}
	if got := (*calls)[0].String(); got != "docker system df --format json" {
		t.Fatalf("call %s", got)
	}
}

func TestDiskUsageReadsABareBuildCacheReclaimable(t *testing.T) {
	run, _ := fake(map[string]string{"docker system df --format json": readFixture(t, "df-buildcache.jsonl")})
	rows, err := Docker{Run: run}.DiskUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[1] != (DiskRow{Type: "Build Cache", Count: 42, Active: 1, Bytes: 1500000000000, Reclaimable: 1200000000}) {
		t.Fatalf("rows %+v", rows)
	}
}

func TestDiskUsageRejectsAnUnreadableRow(t *testing.T) {
	run, _ := fake(map[string]string{"docker system df --format json": `{"Active":"2","Reclaimable":"1GB","Size":"lots","TotalCount":"9","Type":"Images"}`})
	if _, err := (Docker{Run: run}).DiskUsage(context.Background()); err == nil || !strings.Contains(err.Error(), "Images") {
		t.Fatalf("err %v", err)
	}
}

func TestBuildCacheUsageGroupsRecordsByType(t *testing.T) {
	run, calls := fake(map[string]string{"docker system df -v --format json": readFixture(t, "df-v.json")})
	got, err := Docker{Run: run}.BuildCacheUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []CacheTypeUsage{
		{Type: "exec.cachemount", Count: 1, Bytes: 2100000000, Reclaimable: 2100000000},
		{Type: "regular", Count: 2, Bytes: 1686000000, Reclaimable: 456000000},
		{Type: "source.local", Count: 1, Bytes: 12500, Reclaimable: 12500},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("usage %+v", got)
	}
	if c := (*calls)[0].String(); c != "docker system df -v --format json" {
		t.Fatalf("call %s", c)
	}
}

func TestBuildCacheUsageWithoutRecords(t *testing.T) {
	run, _ := fake(map[string]string{"docker system df -v --format json": `{"BuildCache":[],"Containers":[],"Images":[],"Volumes":[]}`})
	got, err := Docker{Run: run}.BuildCacheUsage(context.Background())
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestPruneAllBuildCacheAndUnusedVolumes(t *testing.T) {
	run, calls := fake(map[string]string{
		"docker builder prune -af": "ID\tRECLAIMABLE\nabc\t3.1GB\nTotal:\t3.1GB\n",
		"docker volume prune -f":   "Deleted Volumes:\n7fb3455f\n\nTotal reclaimed space: 8.65GB\n",
	})
	d := Docker{Run: run}
	if freed, err := d.PruneAllBuildCache(context.Background()); err != nil || freed != "3.1GB" {
		t.Fatalf("build cache freed %q, %v", freed, err)
	}
	if freed, err := d.PruneUnusedVolumes(context.Background()); err != nil || freed != "8.65GB" {
		t.Fatalf("volumes freed %q, %v", freed, err)
	}
	if a, b := (*calls)[0].String(), (*calls)[1].String(); a != "docker builder prune -af" || b != "docker volume prune -f" {
		t.Fatalf("calls %s; %s", a, b)
	}
}
