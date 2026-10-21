package daemon

import "github.com/darkraise/ghr/internal/artifact"

// artifactSettings is the artifact store settings file for a runner of repo,
// built from the config and the job key as they are at the call. It answers
// false unless the store is set up, the job key is set, and repo is listed
// under repos with its artifact_store entry on. The runner manager asks only
// for a repository it knows is private.
func (b *Backend) artifactSettings(repo string) ([]byte, bool) {
	if b.AS == nil {
		return nil, false
	}
	cfg := b.Store.Config()
	a := cfg.ArtifactStore
	if !a.Configured() || cfg.Repo(repo) == nil || !a.Enabled(repo) {
		return nil, false
	}
	k := b.AS.JobKey()
	if k == nil {
		return nil, false
	}
	return artifact.Settings{
		V: artifact.SettingsVersion, Endpoint: a.Endpoint, Bucket: a.Bucket, Region: a.RegionOrDefault(),
		KeyID: k.ID, Secret: k.Secret,
	}.JSON(), true
}
