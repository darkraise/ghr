package daemon

import (
	"maps"
	"slices"
	"strings"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

// applyStoragePatch writes a github_storage patch into c; Validate judges
// the values it stores. A value equal to its default is stored as the zero
// value, so a block back at its defaults is not written.
func applyStoragePatch(c *config.Config, p *model.GitHubStoragePatch) error {
	g := &c.GitHubStorage
	if p.IncludedMB != nil {
		if *p.IncludedMB < 1 {
			return api.BadRequest("github_storage.included_mb must be at least 1")
		}
		g.IncludedMB = *p.IncludedMB
	}
	if p.WarnPct != nil {
		g.WarnPct = *p.WarnPct
	}
	if p.CleanupMaxAge != nil {
		g.CleanupMaxAge = 0
		if *p.CleanupMaxAge != "" {
			d, err := config.ParseDuration(*p.CleanupMaxAge)
			if err != nil {
				return api.BadRequest("github_storage.cleanup_max_age: " + err.Error())
			}
			g.CleanupMaxAge = d
		}
	}
	for _, name := range slices.Sorted(maps.Keys(p.Repos)) {
		for k := range g.Repos {
			if strings.EqualFold(k, name) {
				delete(g.Repos, k)
			}
		}
		if v := p.Repos[name]; v != nil {
			if g.Repos == nil {
				g.Repos = map[string]config.StorageOverride{}
			}
			g.Repos[name] = config.StorageOverride{Cleanup: *v}
		}
	}
	if g.IncludedMB == config.DefaultIncludedMB {
		g.IncludedMB = 0
	}
	if slices.Equal(g.WarnPct, config.DefaultWarnPct[:]) {
		g.WarnPct = nil
	}
	if len(g.Repos) == 0 {
		g.Repos = nil
	}
	return nil
}

// applyArtifactStorePatch writes an artifact_store patch into c; Validate
// judges the values it stores. A region or cleanup age equal to its default
// is stored empty, so a block back at its defaults is not written.
func applyArtifactStorePatch(c *config.Config, p *model.ArtifactStorePatch) error {
	if len(p.Repos) > 0 {
		return api.BadRequest("artifact_store.repos is set per repository on the Artifacts page")
	}
	a := &c.ArtifactStore
	if p.Endpoint != nil {
		a.Endpoint = strings.TrimSuffix(strings.TrimSpace(*p.Endpoint), "/")
	}
	if p.Bucket != nil {
		a.Bucket = strings.TrimSpace(*p.Bucket)
	}
	if p.Region != nil {
		a.Region = strings.TrimSpace(*p.Region)
		if a.Region == config.DefaultArtifactRegion {
			a.Region = ""
		}
	}
	if p.CleanupMaxAge != nil {
		v := strings.TrimSpace(*p.CleanupMaxAge)
		if v != "" && v != "off" {
			d, err := config.ParseDuration(v)
			if err != nil {
				return api.BadRequest("artifact_store.cleanup_max_age: " + err.Error())
			}
			v = config.ShortDuration(d.D())
			if d.D() == config.DefaultArtifactCleanup {
				v = ""
			}
		}
		a.CleanupMaxAge = v
	}
	if p.CapacityGB != nil {
		a.CapacityGB = *p.CapacityGB
	}
	return nil
}
