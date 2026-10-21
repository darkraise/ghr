package artifactstore

import (
	"context"
	"errors"
	"strings"

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

// retireRepos reads the owner's repositories' visibility, then removes ghr's
// old GHR_ARTIFACT_STORE variable and GHR_ARTIFACT_KEY secret from every
// covered repository not yet retired. It stops while the token is below the
// reserve kept for the runner manager.
func (s *Service) retireRepos(cfg *config.Config) {
	s.pruneRetired(cfg)
	if s.rateLow() {
		return
	}
	// A failed read keeps the last visibility for the rows.
	_ = s.readVisibility(s.ctx, cfg.Owner)
	for _, repo := range covered(cfg) {
		if s.ctx.Err() != nil || s.rateLow() {
			return
		}
		cur := s.Config()
		if !cur.ArtifactStore.Configured() {
			return
		}
		if !s.isRetired(repo) {
			_ = s.retireOne(s.ctx, cur, repo, true)
		}
	}
}

// Retire removes ghr's old variable and secret from repo now, by the pass's
// rules but without waiting for the rate reserve. It does nothing once repo
// is retired, or while self-hosted storage is not set up, since ownership
// cannot be told without the endpoint and bucket.
func (s *Service) Retire(ctx context.Context, repo string) error {
	cfg := s.Config()
	if !cfg.ArtifactStore.Configured() || s.isRetired(repo) {
		return nil
	}
	return s.retireOne(ctx, cfg, repo, false)
}

// retireOne deletes repo's GHR_ARTIFACT_KEY secret, then its
// GHR_ARTIFACT_STORE variable, when the variable is this ghr's. GitHub never
// shows a secret, so the secret counts as ghr's only beside ghr's variable.
// A variable that is missing, another ghr's, or refused to the token leaves
// the repository retired with nothing deleted. In the background the deletes
// wait while the token is below the reserve.
func (s *Service) retireOne(ctx context.Context, cfg *config.Config, repo string, background bool) error {
	val, found, err := s.getVariable(ctx, repo)
	switch {
	case refused(err):
		s.setRetired(repo)
		return nil
	case err != nil:
		return s.retireFailed(ctx, cfg, repo, err)
	case !found || !ownsVariable(val, cfg.ArtifactStore):
		s.setRetired(repo)
		return nil
	}
	if background && s.rateLow() {
		return s.retireFailed(ctx, cfg, repo, errRateLow)
	}
	var hadSecret bool
	err = s.write(ctx, func(ctx context.Context) error {
		var err error
		hadSecret, err = s.GH.DeleteSecret(ctx, repo, artifact.SecretName)
		return err
	})
	if err != nil {
		return s.retireFailed(ctx, cfg, repo, err)
	}
	err = s.write(ctx, func(ctx context.Context) error {
		_, err := s.GH.DeleteVariable(ctx, repo, artifact.VariableName)
		return err
	})
	if err != nil {
		return s.retireFailed(ctx, cfg, repo, err)
	}
	what := "ghr's old " + artifact.VariableName + " variable"
	if hadSecret {
		what = "ghr's old " + artifact.SecretName + " secret and " + artifact.VariableName + " variable"
	}
	s.Events.Add("info", repo, "artifact store: removed %s from %s", what, repo)
	s.setRetired(repo)
	return nil
}

// refused reports whether GitHub refused the token access. A 401 means the
// token itself failed, which a later pass retries.
func refused(err error) bool {
	var e *github.APIError
	return errors.As(err, &e) && e.Kind == github.ErrAuth && e.Status == 403
}

// retireFailed writes a warning only when repo's failure text changes, so a
// failure that repeats every pass does not flood the event ring, and
// returns err.
func (s *Service) retireFailed(ctx context.Context, cfg *config.Config, repo string, err error) error {
	if ctx.Err() != nil {
		return err
	}
	msg := s.rowErr(err, repo)
	lk := strings.ToLower(repo)
	s.mu.Lock()
	same := s.retireErr[lk] == msg
	s.retireErr[lk] = msg
	s.mu.Unlock()
	if !same {
		full := cfg.Owner + "/" + repo
		s.Events.Add("warn", repo, "artifact store: cannot remove ghr's old GHR_ARTIFACT_STORE variable or GHR_ARTIFACT_KEY secret from %s: %s; delete them with gh variable delete GHR_ARTIFACT_STORE -R %s and gh secret delete GHR_ARTIFACT_KEY -R %s",
			repo, msg, full, full)
	}
	return err
}

func (s *Service) isRetired(repo string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.retired[strings.ToLower(repo)]
}

func (s *Service) setRetired(repo string) {
	lk := strings.ToLower(repo)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retired[lk] = true
	delete(s.retireErr, lk)
}

// pruneRetired forgets every repository cfg no longer covers, so one that is
// covered again is read again.
func (s *Service) pruneRetired(cfg *config.Config) {
	keep := map[string]bool{}
	for _, repo := range covered(cfg) {
		keep[strings.ToLower(repo)] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for lk := range s.retired {
		if !keep[lk] {
			delete(s.retired, lk)
		}
	}
	for lk := range s.retireErr {
		if !keep[lk] {
			delete(s.retireErr, lk)
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

// ownsVariable reports whether a GHR_ARTIFACT_STORE value is this daemon's:
// one that names its endpoint and bucket.
func ownsVariable(val string, a config.ArtifactStore) bool {
	st, err := artifact.ParseStore(val)
	return err == nil && strings.EqualFold(strings.TrimSuffix(st.Endpoint, "/"), strings.TrimSuffix(a.Endpoint, "/")) && st.Bucket == a.Bucket
}

// rowErr is a failure's text: the budget and rate refusals as they are,
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

// SyncRepo returns repo's row.
func (s *Service) SyncRepo(_ context.Context, repo string) (model.ArtifactStoreRepo, error) {
	if r, ok := s.Row(repo); ok {
		return r, nil
	}
	return model.ArtifactStoreRepo{}, ErrUnknownRepo
}

// SwitchOff retires repo.
func (s *Service) SwitchOff(ctx context.Context, repo string) error { return s.Retire(ctx, repo) }
