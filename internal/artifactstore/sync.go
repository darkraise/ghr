package artifactstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/artifact"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/ghstorage"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

var errRateLow = errors.New("waiting for GitHub's rate limit to recover; ghr retries on a later pass")

// rateLow reports whether the token has fewer calls left this hour than the
// reserve kept for the runner manager.
func (s *Service) rateLow() bool {
	n := s.GH.RateRemaining()
	return n >= 0 && n < ghstorage.MinRateLeft
}

// syncRepos reads the owner's repositories' visibility, then brings each
// covered repository's variable and secret in line with its switch. Each
// repository is synced with the config as it is then, so a switch saved
// during the pass is not undone. It stops while the token is below the
// reserve kept for the runner manager.
func (s *Service) syncRepos(cfg *config.Config, keys Keys) {
	if s.rateLow() {
		return
	}
	visErr := s.readVisibility(s.ctx, cfg.Owner)
	for _, repo := range covered(cfg) {
		if s.ctx.Err() != nil || s.rateLow() {
			return
		}
		s.syncMu.Lock()
		st := s.syncOne(s.ctx, s.Config(), keys, repo, visErr, true)
		if s.ctx.Err() == nil {
			s.setSync(repo, st)
		}
		s.syncMu.Unlock()
		if s.ctx.Err() != nil {
			return
		}
	}
}

// readVisibility records which of the owner's repositories are private. A
// failed read keeps the last one and is returned.
func (s *Service) readVisibility(ctx context.Context, owner string) error {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	all, err := s.GH.ListUserRepos(ctx)
	if err != nil {
		return err
	}
	vis := map[string]bool{}
	for _, r := range all {
		if strings.EqualFold(r.Owner.Login, owner) {
			vis[strings.ToLower(r.Name)] = r.Private
		}
	}
	s.mu.Lock()
	s.private = vis
	s.bump()
	s.mu.Unlock()
	return nil
}

func (s *Service) visibility(repo string) (private, known bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	private, known = s.private[strings.ToLower(repo)]
	return private, known
}

func (s *Service) setSync(repo string, st repoSync) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncs[strings.ToLower(repo)] = st
	s.bump()
}

func (s *Service) markSecret(lk string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secretAt[lk] = at
}

func (s *Service) syncState(repo string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.syncs[strings.ToLower(repo)].state
}

// cleanOff reports whether repo's last sync found it switched off with
// nothing of ghr's left. ghr writes nothing to an off repository and a
// switch changes the recorded state, so a pass need not read it again.
func (s *Service) cleanOff(repo string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.syncs[strings.ToLower(repo)]
	return ok && st.state == "off" && st.err == ""
}

// ownsVariable reports whether a GHR_ARTIFACT_STORE value is this daemon's:
// one that names its endpoint and bucket.
func ownsVariable(val string, a config.ArtifactStore) bool {
	st, err := artifact.ParseStore(val)
	return err == nil && strings.EqualFold(strings.TrimSuffix(st.Endpoint, "/"), strings.TrimSuffix(a.Endpoint, "/")) && st.Bucket == a.Bucket
}

func foreignText(val string) string {
	if st, err := artifact.ParseStore(val); err == nil {
		return fmt.Sprintf("%s points to %s, bucket %s, which is not this ghr's storage; ghr leaves it alone", artifact.VariableName, st.Endpoint, st.Bucket)
	}
	return artifact.VariableName + " holds a value ghr did not write; ghr leaves it alone"
}

// rowErr is a row's error text: the budget and rate refusals as they are,
// anything else as the daemon words it.
func (s *Service) rowErr(err error, repo string) string {
	var be *ghstorage.BudgetError
	if errors.As(err, &be) || errors.Is(err, errRateLow) {
		return err.Error()
	}
	return s.describe(err, repo)
}

// write runs one GitHub write within the shared write budget.
func (s *Service) write(ctx context.Context, fn func(ctx context.Context) error) error {
	return s.Budget.Do(func() error {
		ctx, cancel := context.WithTimeout(ctx, callTimeout)
		defer cancel()
		return fn(ctx)
	})
}

func (s *Service) getVariable(ctx context.Context, repo string) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return s.GH.GetVariable(ctx, repo, artifact.VariableName)
}

func (s *Service) secretExists(ctx context.Context, repo string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return s.GH.SecretExists(ctx, repo, artifact.SecretName)
}

// syncOne brings repo's variable and secret in line with its switch and
// returns its state. A variable another daemon wrote is never changed. In
// the background, writes wait while the token is below the reserve kept for
// the runner manager, and a switched-off repository already found clean is
// not read again. Call with s.syncMu held.
func (s *Service) syncOne(ctx context.Context, cfg *config.Config, keys Keys, repo string, visErr error, background bool) repoSync {
	a := cfg.ArtifactStore
	on := a.Enabled(repo)
	if !on && background && s.cleanOff(repo) {
		return repoSync{state: "off"}
	}
	val, found, err := s.getVariable(ctx, repo)
	if err != nil {
		// A token without Variables access cannot have let ghr write
		// there either, so there is nothing of ghr's to remove.
		if !on && github.IsKind(err, github.ErrAuth) {
			return repoSync{state: "off"}
		}
		return repoSync{state: "error", err: s.rowErr(err, repo)}
	}
	if found && !ownsVariable(val, a) {
		return repoSync{state: "foreign", err: foreignText(val)}
	}
	if !on {
		if err := s.removeOurs(ctx, repo, found, background); err != nil {
			if !found && github.IsKind(err, github.ErrAuth) {
				return repoSync{state: "off"}
			}
			return repoSync{state: "off", err: s.rowErr(err, repo)}
		}
		return repoSync{state: "off"}
	}
	private, known := s.visibility(repo)
	if !known {
		if visErr != nil {
			return repoSync{state: "error", err: s.rowErr(visErr, repo)}
		}
		return repoSync{state: "error", err: "the token cannot see " + repo + ": give it access to the repository"}
	}
	if !private {
		was := s.syncState(repo)
		st := repoSync{state: "public"}
		if err := s.removeOurs(ctx, repo, found, background); err != nil {
			st.err = s.rowErr(err, repo)
		}
		if was != "public" {
			s.Events.Add("warn", repo, "artifact store: %s is public, so self-hosted storage is off for it: make it private to switch it on", repo)
		}
		return st
	}
	if keys.Job == nil {
		return repoSync{state: "error", err: ErrNoJobKey.Error()}
	}
	want := artifact.Store{V: 1, Endpoint: a.Endpoint, Bucket: a.Bucket, Region: a.RegionOrDefault(), KeyID: keys.Job.ID}.JSON()
	has, err := s.secretExists(ctx, repo)
	if err != nil {
		return repoSync{state: "error", err: s.rowErr(err, repo)}
	}
	cur, _ := artifact.ParseStore(val)
	lk := strings.ToLower(repo)
	s.mu.Lock()
	at, seen := s.secretAt[lk]
	s.mu.Unlock()
	// GitHub never shows a secret, so only ghr's own record shows a job key
	// replaced under the same ID. With no record yet, as after a restart,
	// the key_id is trusted.
	writeSecret := !has || !found || cur.KeyID != keys.Job.ID || (seen && !at.Equal(keys.Job.SetAt))
	if !writeSecret {
		s.markSecret(lk, keys.Job.SetAt)
	}
	writeVar := !found || val != want
	if !writeSecret && !writeVar {
		return repoSync{state: "set"}
	}
	state := "stale"
	if !has || !found {
		state = "missing"
	}
	if background && s.rateLow() {
		return repoSync{state: state, err: errRateLow.Error()}
	}
	var wrote []string
	if writeSecret {
		err := s.write(ctx, func(ctx context.Context) error {
			return s.GH.SetSecret(ctx, repo, artifact.SecretName, artifact.FormatKey(keys.Job.ID, keys.Job.Secret))
		})
		if err != nil {
			return repoSync{state: "error", err: s.rowErr(err, repo)}
		}
		s.markSecret(lk, keys.Job.SetAt)
		wrote = append(wrote, "the "+artifact.SecretName+" secret")
	}
	if writeVar {
		err := s.write(ctx, func(ctx context.Context) error {
			return s.GH.SetVariable(ctx, repo, artifact.VariableName, want)
		})
		if err != nil {
			return repoSync{state: "error", err: s.rowErr(err, repo)}
		}
		wrote = append(wrote, "the "+artifact.VariableName+" variable")
	}
	s.Events.Add("info", repo, "artifact store: wrote %s", strings.Join(wrote, " and "))
	return repoSync{state: "set"}
}

// removeOurs deletes the secret, then the variable, when they exist. found
// tells whether the variable exists; the caller has checked that it is
// ghr's.
func (s *Service) removeOurs(ctx context.Context, repo string, found, background bool) error {
	has, err := s.secretExists(ctx, repo)
	if err != nil {
		return err
	}
	if !has && !found {
		return nil
	}
	if background && s.rateLow() {
		return errRateLow
	}
	var removed []string
	if has {
		err := s.write(ctx, func(ctx context.Context) error {
			_, err := s.GH.DeleteSecret(ctx, repo, artifact.SecretName)
			return err
		})
		if err != nil {
			return err
		}
		removed = append(removed, "the "+artifact.SecretName+" secret")
	}
	if found {
		err := s.write(ctx, func(ctx context.Context) error {
			_, err := s.GH.DeleteVariable(ctx, repo, artifact.VariableName)
			return err
		})
		if err != nil {
			return err
		}
		removed = append(removed, "the "+artifact.VariableName+" variable")
	}
	s.Events.Add("info", repo, "artifact store: removed %s", strings.Join(removed, " and "))
	return nil
}

// SyncRepo applies one covered repository's variable and secret now, with
// the config as it is, and returns its row.
func (s *Service) SyncRepo(ctx context.Context, repo string) (model.ArtifactStoreRepo, error) {
	cfg := s.Config()
	a := cfg.ArtifactStore
	if !a.Configured() {
		return model.ArtifactStoreRepo{}, ErrNotConfigured
	}
	if cfg.Repo(repo) == nil && !cfg.Watched(repo) {
		return model.ArtifactStoreRepo{}, ErrUnknownRepo
	}
	repo = displayName(cfg, repo)
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return model.ArtifactStoreRepo{}, ErrClosed
	}
	keys := s.loadKeys()
	var visErr error
	if a.Enabled(repo) {
		if keys.Job == nil {
			return model.ArtifactStoreRepo{}, ErrNoJobKey
		}
		visErr = s.readVisibility(ctx, cfg.Owner)
	}
	s.syncMu.Lock()
	// A removal's switch-off may have held syncMu while this call waited.
	cfg = s.Config()
	if cfg.Repo(repo) == nil && !cfg.Watched(repo) {
		s.syncMu.Unlock()
		return model.ArtifactStoreRepo{}, ErrUnknownRepo
	}
	st := s.syncOne(ctx, cfg, keys, repo, visErr, false)
	s.setSync(repo, st)
	s.syncMu.Unlock()
	for _, r := range s.Snapshot().Repos {
		if strings.EqualFold(r.Name, repo) {
			return r, nil
		}
	}
	return model.ArtifactStoreRepo{}, ErrUnknownRepo
}

// SwitchOff deletes ghr's variable and secret from repo, whatever its
// switch says. A variable another daemon wrote is left alone. It does
// nothing while self-hosted storage is not set up, since ownership cannot
// be told without the endpoint and bucket.
func (s *Service) SwitchOff(ctx context.Context, repo string) error {
	a := s.Config().ArtifactStore
	if !a.Configured() {
		return nil
	}
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	val, found, err := s.getVariable(ctx, repo)
	if err != nil {
		return err
	}
	if found && !ownsVariable(val, a) {
		s.setSync(repo, repoSync{state: "foreign", err: foreignText(val)})
		return nil
	}
	if err := s.removeOurs(ctx, repo, found, false); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.secretAt, strings.ToLower(repo))
	s.mu.Unlock()
	s.setSync(repo, repoSync{state: "off"})
	return nil
}
