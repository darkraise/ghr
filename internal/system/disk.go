package system

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// DiskRow is one row of `docker system df`.
type DiskRow struct {
	Type        string // Images, Containers, Local Volumes, Build Cache
	Count       int
	Active      int
	Bytes       int64
	Reclaimable int64
}

// CacheTypeUsage sums the build cache records of one CacheType; records not
// in use are reclaimable.
type CacheTypeUsage struct {
	Type        string
	Count       int
	Bytes       int64
	Reclaimable int64
}

var sizeUnits = map[string]float64{"B": 1, "kB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12, "PB": 1e15}

// ParseSize reads Docker's humanized sizes, which use decimal units:
// "40.96kB", "0B", or "5.627GB (85%)" with the share Docker appends.
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, " ("); i >= 0 {
		s = s[:i]
	}
	i := strings.IndexFunc(s, func(r rune) bool { return (r < '0' || r > '9') && r != '.' })
	if i <= 0 {
		return 0, fmt.Errorf("unreadable size %q", s)
	}
	mult, ok := sizeUnits[s[i:]]
	if !ok {
		return 0, fmt.Errorf("unknown unit in size %q", s)
	}
	n, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0, fmt.Errorf("unreadable size %q", s)
	}
	return int64(math.Round(n * mult)), nil
}

// DiskUsage reads `docker system df`: one JSON object per line, every field a string.
func (d Docker) DiskUsage(ctx context.Context) ([]DiskRow, error) {
	out, err := d.Run(ctx, "docker", "system", "df", "--format", "json")
	if err != nil {
		return nil, err
	}
	var rows []DiskRow
	for _, l := range lines(out) {
		var r struct{ Type, TotalCount, Active, Size, Reclaimable string }
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			return nil, fmt.Errorf("docker system df: %w", err)
		}
		count, err1 := strconv.Atoi(r.TotalCount)
		active, err2 := strconv.Atoi(r.Active)
		size, err3 := ParseSize(r.Size)
		reclaimable, err4 := ParseSize(r.Reclaimable)
		if err := errors.Join(err1, err2, err3, err4); err != nil {
			return nil, fmt.Errorf("docker system df %s: %w", r.Type, err)
		}
		rows = append(rows, DiskRow{Type: r.Type, Count: count, Active: active, Bytes: size, Reclaimable: reclaimable})
	}
	return rows, nil
}

// BuildCacheUsage groups the build cache records of `docker system df -v`
// by CacheType, sorted by type name.
func (d Docker) BuildCacheUsage(ctx context.Context) ([]CacheTypeUsage, error) {
	out, err := d.Run(ctx, "docker", "system", "df", "-v", "--format", "json")
	if err != nil {
		return nil, err
	}
	var doc struct {
		BuildCache []struct{ CacheType, Size, InUse string }
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, fmt.Errorf("docker system df -v: %w", err)
	}
	by := map[string]*CacheTypeUsage{}
	for _, r := range doc.BuildCache {
		n, err := ParseSize(r.Size)
		if err != nil {
			return nil, fmt.Errorf("docker system df -v: %w", err)
		}
		u := by[r.CacheType]
		if u == nil {
			u = &CacheTypeUsage{Type: r.CacheType}
			by[r.CacheType] = u
		}
		u.Count++
		u.Bytes += n
		if r.InUse != "true" {
			u.Reclaimable += n
		}
	}
	res := make([]CacheTypeUsage, 0, len(by))
	for _, u := range by {
		res = append(res, *u)
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Type < res[j].Type })
	return res, nil
}

func (d Docker) PruneAllBuildCache(ctx context.Context) (string, error) {
	out, err := d.Run(ctx, "docker", "builder", "prune", "-af")
	return reclaimed(out), err
}

// PruneUnusedVolumes removes anonymous volumes no container references.
// Without --all, named volumes stay.
func (d Docker) PruneUnusedVolumes(ctx context.Context) (string, error) {
	out, err := d.Run(ctx, "docker", "volume", "prune", "-f")
	return reclaimed(out), err
}
