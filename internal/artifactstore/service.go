package artifactstore

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/darkraise/ghr/internal/artifact"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/fsutil"
	"github.com/darkraise/ghr/internal/ghstorage"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/s3"
)

// GitHub is the part of the GitHub client the service uses.
type GitHub interface {
	ListUserRepos(ctx context.Context) ([]github.UserRepo, error)
	ListRuns(ctx context.Context, repo, status string) ([]github.Run, error)
	GetVariable(ctx context.Context, repo, name string) (string, bool, error)
	SetVariable(ctx context.Context, repo, name, value string) error
	DeleteVariable(ctx context.Context, repo, name string) (bool, error)
	SecretExists(ctx context.Context, repo, name string) (bool, error)
	SetSecret(ctx context.Context, repo, name, value string) error
	DeleteSecret(ctx context.Context, repo, name string) (bool, error)
	RateRemaining() int
}

// Bucket is the part of the S3 client the service uses; *s3.Client is one.
type Bucket interface {
	List(ctx context.Context, prefix string) ([]s3.Object, error)
	Head(ctx context.Context, key string) (s3.Head, error)
	Delete(ctx context.Context, keys []string) ([]string, error)
	ListUploads(ctx context.Context) ([]s3.Upload, error)
	Abort(ctx context.Context, key, uploadID string) error
	PresignGet(key string, ttl time.Duration) (string, time.Time, error)
}

var (
	ErrBusy          = errors.New("an artifact store check is already running")
	ErrClosed        = errors.New("ghr is shutting down")
	ErrNotConfigured = errors.New("self-hosted storage is not set up")
	ErrNoAdminKey    = errors.New("no admin key set")
	ErrNoJobKey      = errors.New("set the job key first")
	ErrUnknownRepo   = errors.New("not a covered repository")
)

// BadInput is a request the service refuses as given; the daemon answers 400
// with its text.
type BadInput string

func (e BadInput) Error() string { return string(e) }

const (
	InventoryInterval = 15 * time.Minute
	// HeadWorkers is how many objects an inventory reads the details of at
	// once.
	HeadWorkers = 4
	// MaxItems is how many of a repository's newest objects the snapshot
	// carries.
	MaxItems = 500
	// MaxDeletes is how many objects one cleanup pass deletes; the rest wait
	// for the next pass.
	MaxDeletes = 100
	// UploadAbortAge is how old an unfinished multipart upload is before
	// cleanup aborts it.
	UploadAbortAge = 24 * time.Hour
	// MaxDeletions is how many deletions the snapshot keeps.
	MaxDeletions = 200
	// deletedTTL is how long a deleted object is kept out of listings that
	// started before its delete.
	deletedTTL = time.Hour
	// listTimeout bounds one listing of the whole bucket, every page included.
	listTimeout = 5 * time.Minute
	// callTimeout bounds every other S3 or GitHub call.
	callTimeout  = time.Minute
	cacheVersion = 1
)

// Service keeps the self-hosted artifact storage snapshot and each switched-on
// repository's variable and secret. Start it once; Close stops it.
type Service struct {
	GH        GitHub
	Config    func() *config.Config
	Events    *events.Ring
	KeysPath  string
	CachePath string
	Budget    *ghstorage.Budget                                                 // nil means a budget of its own
	NewBucket func(a config.ArtifactStore, cred s3.Credentials) (Bucket, error) // nil means s3.New
	Now       func() time.Time                                                  // nil means time.Now
	Describe  func(err error, repo string) string                               // a repository's error text; nil means err.Error()
	Wake      func()                                                            // called after every snapshot change; must not block

	version atomic.Int64

	mu        sync.Mutex
	closed    bool
	running   bool // a pass runs
	keys      Keys
	keysErr   string
	checkedAt time.Time
	listedFor string // the endpoint and bucket the listing came from
	listErr   string
	objects   []object              // the last good listing
	cache     map[string]cacheEntry // object key → its details; nil until the first inventory loads it
	private   map[string]bool       // lower-case repo → private, from the last visibility read
	syncs     map[string]repoSync   // lower-case repo → its variable and secret state
	secretAt  map[string]time.Time  // lower-case repo → SetAt of the job key ghr last wrote to, or found current in, its secret
	deleted   map[string]time.Time  // object key → when deleted
	deletions []model.Deletion      // newest first
	op        *model.GitHubOperation
	opRunning bool

	cleanupErr map[string]string // lower-case repo, or "" for the bucket → the failure the last cleanup event was written for; only the pass touches it

	keysMu sync.Mutex // one key file change at a time
	syncMu sync.Mutex // one caller writes a repository's variable and secret at a time

	ctx    context.Context
	cancel context.CancelFunc
	kick   chan struct{}
	wg     sync.WaitGroup
}

// object is one listed object. Repo is the lower-case repository of an
// artifact key under the owner, and empty for any other key.
type object struct {
	s3.Object
	Repo   string
	RunID  int64
	Name   string
	Meta   artifact.Meta
	raw    map[string]string // the user metadata as read
	Headed bool              // the metadata was read
}

type cacheEntry struct {
	Key          string            `json:"key"`
	ETag         string            `json:"etag"`
	LastModified time.Time         `json:"last_modified"`
	Size         int64             `json:"size"`
	Meta         map[string]string `json:"meta"`
}

func (c cacheEntry) matches(o s3.Object) bool {
	return c.ETag == o.ETag && c.Size == o.Size && c.LastModified.Equal(o.LastModified)
}

type cacheFile struct {
	Version int          `json:"version"`
	Entries []cacheEntry `json:"entries"`
}

type repoSync struct {
	state string
	err   string
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) describe(err error, repo string) string {
	if s.Describe != nil {
		return s.Describe(err, repo)
	}
	return err.Error()
}

// bump marks a snapshot change. It may run under s.mu: Wake must not block.
func (s *Service) bump() {
	s.version.Add(1)
	if s.Wake != nil {
		s.Wake()
	}
}

func (s *Service) init() {
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.kick = make(chan struct{}, 1)
	s.private = map[string]bool{}
	s.syncs = map[string]repoSync{}
	s.secretAt = map[string]time.Time{}
	s.deleted = map[string]time.Time{}
	s.cleanupErr = map[string]string{}
	if s.Budget == nil {
		s.Budget = &ghstorage.Budget{Now: s.Now}
	}
}

// Start reads the keys and runs a pass at once, then one every
// InventoryInterval.
func (s *Service) Start() {
	s.init()
	s.loadKeys()
	s.kick <- struct{}{}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.loop()
	}()
}

func (s *Service) loop() {
	t := time.NewTimer(InventoryInterval)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.kick:
		case <-t.C:
		}
		s.pass()
		t.Reset(InventoryInterval)
	}
}

// Kick asks for a pass; kicks during a pass coalesce into one after it.
func (s *Service) Kick() {
	if s.kick == nil {
		return
	}
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// Refresh asks for a pass now; ErrBusy while one runs.
func (s *Service) Refresh() error {
	configured := s.Config().ArtifactStore.Configured()
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.closed:
		return ErrClosed
	case !configured:
		return ErrNotConfigured
	case s.running:
		return ErrBusy
	}
	s.Kick()
	return nil
}

// Version rises on every snapshot change.
func (s *Service) Version() int64 { return s.version.Load() }

// Close stops the passes and any operation; Wait returns once they stopped.
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *Service) Wait() { s.wg.Wait() }

// PolicyChanged shows a saved policy at once, and asks for a pass when the
// endpoint or bucket is not the one the listing came from.
func (s *Service) PolicyChanged() {
	a := s.Config().ArtifactStore
	s.mu.Lock()
	moved := a.Configured() && s.listedFor != connKey(a)
	s.bump()
	s.mu.Unlock()
	if moved {
		s.Kick()
	}
}

// KeysChanged rereads the key file, clears what the old admin key's answers
// said and asks for a pass with the new keys.
func (s *Service) KeysChanged() {
	s.loadKeys()
	s.mu.Lock()
	s.listErr = ""
	s.bump()
	s.mu.Unlock()
	s.Kick()
}

// SetKey replaces the admin or the job key in the key file.
func (s *Service) SetKey(which, id, secret string) error {
	if which != "admin" && which != "job" {
		return BadInput(`which must be "admin" or "job"`)
	}
	id, secret, err := CheckKeyInput(id, secret)
	if err != nil {
		return BadInput(err.Error())
	}
	s.keysMu.Lock()
	defer s.keysMu.Unlock()
	k, err := LoadKeys(s.KeysPath)
	if err != nil {
		return fmt.Errorf("cannot read the artifact store keys: %w", err)
	}
	key := &Key{ID: id, Secret: secret, SetAt: s.now()}
	if which == "admin" {
		k.Admin = key
	} else {
		k.Job = key
	}
	if err := SaveKeys(s.KeysPath, k); err != nil {
		return fmt.Errorf("cannot save the artifact store keys: %w", err)
	}
	s.Events.Add("info", "", "artifact store: %s key set (%s)", which, ShortID(id))
	s.KeysChanged()
	return nil
}

// loadKeys reads the key file. An unreadable file keeps the last keys read
// and shows its error.
func (s *Service) loadKeys() Keys {
	k, err := LoadKeys(s.KeysPath)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		msg := "cannot read the artifact store keys: " + err.Error()
		if s.keysErr != msg {
			s.keysErr = msg
			s.bump()
		}
		return s.keys
	}
	if s.keysErr != "" || !sameKey(s.keys.Admin, k.Admin) || !sameKey(s.keys.Job, k.Job) {
		s.bump()
	}
	s.keys, s.keysErr = k, ""
	return k
}

func sameKey(a, b *Key) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.ID == b.ID && a.Secret == b.Secret && a.SetAt.Equal(b.SetAt)
}

func connKey(a config.ArtifactStore) string { return a.Endpoint + " " + a.Bucket }

func (s *Service) bucket(a config.ArtifactStore, k *Key) (Bucket, error) {
	cred := s3.Credentials{AccessKeyID: k.ID, SecretAccessKey: k.Secret}
	if s.NewBucket != nil {
		return s.NewBucket(a, cred)
	}
	c, err := s3.New(a.Endpoint, a.Bucket, a.RegionOrDefault(), cred)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// pass syncs the repositories' variables and secrets, then lists the bucket
// and runs the cleanup. Sync runs without the admin key, since jobs do not
// need it; the cleanup runs only after a listing that succeeded.
func (s *Service) pass() {
	cfg := s.Config()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()
	if !cfg.ArtifactStore.Configured() {
		return
	}
	keys := s.loadKeys()
	s.syncRepos(cfg, keys)
	if keys.Admin == nil {
		return
	}
	b, err := s.bucket(cfg.ArtifactStore, keys.Admin)
	if err != nil {
		s.setListErr(err.Error())
		return
	}
	if s.inventory(cfg, b) {
		s.cleanup(cfg, b)
	}
}

func (s *Service) setListErr(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() == nil {
		s.listErr = msg
		s.bump()
	}
}

// inventory lists the whole bucket and reads the details of new or changed
// objects. It reports whether the cleanup may run on the listing: not after
// a failed listing, which keeps the last one with the error, nor after one
// where an object vanished before its details were read, since a bucket
// deleted after the listing answers those reads the same way.
func (s *Service) inventory(cfg *config.Config, b Bucket) bool {
	ctx, cancel := context.WithTimeout(s.ctx, listTimeout)
	listed, err := b.List(ctx, "")
	cancel()
	if err != nil {
		s.setListErr("cannot list bucket " + cfg.ArtifactStore.Bucket + ": " + err.Error())
		return false
	}
	s.mu.Lock()
	cache := s.cache
	s.mu.Unlock()
	if cache == nil {
		cache = s.loadCache()
	}
	classified := classify(listed, cfg.Owner)
	n := len(classified)
	objs := s.details(s.ctx, b, classified, cache)
	if s.ctx.Err() != nil {
		return false
	}
	next := map[string]cacheEntry{}
	for _, o := range objs {
		if o.Headed {
			next[o.Key] = cacheEntry{Key: o.Key, ETag: o.ETag, LastModified: o.LastModified, Size: o.Size, Meta: o.raw}
		}
	}
	s.saveCache(next)
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return false
	}
	for k, at := range s.deleted {
		if now.Sub(at) >= deletedTTL {
			delete(s.deleted, k)
		}
	}
	s.cache = next
	s.objects = slices.DeleteFunc(objs, s.isDeleted)
	s.checkedAt, s.listErr, s.listedFor = now, "", connKey(cfg.ArtifactStore)
	s.bump()
	if vanished := n - len(objs); vanished > 0 {
		log.Printf("artifact store: cleanup waits for the next pass: %d objects vanished after the listing", vanished)
		return false
	}
	return true
}

// isDeleted reports whether o was deleted after it was last written, so a
// listing under way does not bring it back. Call with s.mu held.
func (s *Service) isDeleted(o object) bool {
	at, ok := s.deleted[o.Key]
	return ok && !o.LastModified.After(at)
}

// classify reads each key: an artifact key under owner gets its repository,
// run and name; any other key stays without them.
func classify(listed []s3.Object, owner string) []object {
	out := make([]object, 0, len(listed))
	for _, o := range listed {
		obj := object{Object: o}
		if ow, repo, run, name, ok := artifact.ParseObjectKey(o.Key); ok && strings.EqualFold(ow, owner) {
			obj.Repo, obj.RunID, obj.Name = strings.ToLower(repo), run, name
		}
		out = append(out, obj)
	}
	return out
}

// details fills each artifact object's metadata from cache when its ETag,
// time and size still match, and otherwise reads it, HeadWorkers at a time.
// An object gone since the listing is dropped; one whose read failed stays
// without details and is read again next pass.
func (s *Service) details(ctx context.Context, b Bucket, objs []object, cache map[string]cacheEntry) []object {
	gone := make([]bool, len(objs))
	sem := make(chan struct{}, HeadWorkers)
	var wg sync.WaitGroup
	for i := range objs {
		o := &objs[i]
		if o.Repo == "" {
			continue
		}
		if c, ok := cache[o.Key]; ok && c.matches(o.Object) {
			o.raw, o.Meta, o.Headed = c.Meta, artifact.ParseMeta(c.Meta), true
			continue
		}
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			hctx, cancel := context.WithTimeout(ctx, callTimeout)
			defer cancel()
			h, err := b.Head(hctx, o.Key)
			switch {
			case s3.IsNotFound(err):
				gone[i] = true
			case err != nil:
				log.Printf("artifact store: cannot read the details of %s: %v", o.Key, err)
			default:
				o.raw, o.Meta, o.Headed = h.Meta, artifact.ParseMeta(h.Meta), true
			}
		}()
	}
	wg.Wait()
	out := objs[:0]
	for i, o := range objs {
		if !gone[i] {
			out = append(out, o)
		}
	}
	return out
}

// loadCache reads the metadata cache file. A missing file is an empty cache;
// an unreadable one is discarded with a warning and rebuilt by this pass.
func (s *Service) loadCache() map[string]cacheEntry {
	out := map[string]cacheEntry{}
	if s.CachePath == "" {
		return out
	}
	b, err := os.ReadFile(s.CachePath)
	if errors.Is(err, fs.ErrNotExist) {
		return out
	}
	var f cacheFile
	if err == nil {
		err = json.Unmarshal(b, &f)
	}
	if err == nil && f.Version != cacheVersion {
		err = fmt.Errorf("unknown version %d", f.Version)
	}
	if err != nil {
		s.Events.Add("warn", "", "artifact store: discarded the metadata cache %s (%v); every object is read again", s.CachePath, err)
		return out
	}
	for _, e := range f.Entries {
		out[e.Key] = e
	}
	return out
}

func (s *Service) saveCache(c map[string]cacheEntry) {
	if s.CachePath == "" {
		return
	}
	f := cacheFile{Version: cacheVersion, Entries: make([]cacheEntry, 0, len(c))}
	for _, k := range slices.Sorted(maps.Keys(c)) {
		f.Entries = append(f.Entries, c[k])
	}
	b, err := json.Marshal(f)
	if err == nil {
		err = fsutil.WritePrivate(s.CachePath, b)
	}
	if err != nil {
		log.Printf("artifact store: cannot save the metadata cache %s: %v", s.CachePath, err)
	}
}

// errorText is the snapshot's error: the key file's, the missing admin key,
// or the last listing's. Call with s.mu held.
func (s *Service) errorText() string {
	switch {
	case s.keysErr != "":
		return s.keysErr
	case s.keys.Admin == nil:
		return ErrNoAdminKey.Error()
	}
	return s.listErr
}

// listingFor reports whether the last listing came from a's bucket. Call
// with s.mu held.
func (s *Service) listingFor(a config.ArtifactStore) bool {
	return !s.checkedAt.IsZero() && s.listedFor == connKey(a)
}

func keyInfo(k *Key) *model.ArtifactKeyInfo {
	if k == nil {
		return nil
	}
	return &model.ArtifactKeyInfo{ID: ShortID(k.ID), SetAt: k.SetAt}
}

func capacityBytes(a config.ArtifactStore) int64 { return int64(a.CapacityGB) * 1_000_000_000 }

// covered lists the covered repositories as the config spells them.
func covered(cfg *config.Config) []string {
	var out []string
	for _, r := range cfg.Repos {
		out = append(out, r.Name)
	}
	return append(out, cfg.WatchRepos...)
}

// displayName is repo as the config spells it, or repo itself for a
// repository that is not covered.
func displayName(cfg *config.Config, repo string) string {
	for _, name := range covered(cfg) {
		if strings.EqualFold(name, repo) {
			return name
		}
	}
	return repo
}

func isCovered(cfg *config.Config, repo string) bool {
	return slices.ContainsFunc(covered(cfg), func(name string) bool { return strings.EqualFold(name, repo) })
}

func itemOf(o object) model.ArtifactStoreItem {
	return model.ArtifactStoreItem{Key: o.Key, Name: o.Name, RunID: o.RunID, Attempt: o.Meta.Attempt,
		Workflow: o.Meta.Workflow, Job: o.Meta.Job, Branch: o.Meta.Ref, SHA: o.Meta.SHA, Bytes: o.Size,
		Files: o.Meta.Files, Digest: o.Meta.Digest, UploadedAt: o.LastModified, ExpiresAt: o.Meta.Expires}
}

func newestFirst(a, b model.ArtifactStoreItem) int {
	if c := b.UploadedAt.Compare(a.UploadedAt); c != 0 {
		return c
	}
	return strings.Compare(a.Key, b.Key)
}

// Snapshot is the last listing with the repositories' sync states, the
// operation and the deletions. The covered repositories and any repository
// with objects get a row; each row's switch and cleanup come from the
// current config, so a saved policy shows at once.
func (s *Service) Snapshot() model.ArtifactStore {
	cfg := s.Config()
	a := cfg.ArtifactStore
	s.mu.Lock()
	defer s.mu.Unlock()
	out := model.ArtifactStore{Repos: []model.ArtifactStoreRepo{}, Deletions: append([]model.Deletion{}, s.deletions...)}
	if s.op != nil {
		op := *s.op
		op.Failed = append([]model.OperationFailure{}, s.op.Failed...)
		out.Operation = &op
	}
	if !a.Configured() {
		return out
	}
	out.Configured, out.Endpoint, out.Bucket, out.Region = true, a.Endpoint, a.Bucket, a.RegionOrDefault()
	out.AdminKey, out.JobKey = keyInfo(s.keys.Admin), keyInfo(s.keys.Job)
	out.Error = s.errorText()
	out.CapacityBytes = capacityBytes(a)
	rows := map[string]*model.ArtifactStoreRepo{}
	var order []string
	add := func(name string) *model.ArtifactStoreRepo {
		key := strings.ToLower(name)
		if r, ok := rows[key]; ok {
			return r
		}
		rows[key] = &model.ArtifactStoreRepo{Name: name, Items: []model.ArtifactStoreItem{}}
		order = append(order, key)
		return rows[key]
	}
	for _, name := range covered(cfg) {
		add(name)
	}
	if s.listingFor(a) {
		at := s.checkedAt
		out.CheckedAt = &at
		for _, o := range s.objects {
			out.UsedBytes += o.Size
			if o.Repo == "" {
				out.OtherBytes += o.Size
				continue
			}
			out.Count++
			r := add(o.Repo)
			r.Bytes += o.Size
			r.Count++
			r.Items = append(r.Items, itemOf(o))
		}
	}
	for _, key := range order {
		r := rows[key]
		r.Private = s.private[key]
		r.Enabled = a.Enabled(r.Name) && isCovered(cfg, r.Name)
		st := s.syncs[key]
		r.State, r.StateError = st.state, st.err
		if r.State == "" {
			r.State = "off"
			if r.Enabled {
				r.State = "missing"
			}
		}
		age, src := a.CleanupFor(r.Name)
		r.Cleanup, r.CleanupSource = config.CleanupText(age), src
		slices.SortStableFunc(r.Items, newestFirst)
		if len(r.Items) > MaxItems {
			r.Items = r.Items[:MaxItems]
		}
		out.Repos = append(out.Repos, *r)
	}
	slices.SortStableFunc(out.Repos, func(a, b model.ArtifactStoreRepo) int {
		if c := cmp.Compare(b.Bytes, a.Bytes); c != 0 {
			return c
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return out
}

// Summary is the status line's numbers, nil while self-hosted storage is not
// set up.
func (s *Service) Summary() *model.ArtifactStoreSummary {
	cfg := s.Config()
	a := cfg.ArtifactStore
	if !a.Configured() {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := &model.ArtifactStoreSummary{CapacityBytes: capacityBytes(a), Error: s.errorText()}
	if s.listingFor(a) {
		for _, o := range s.objects {
			out.UsedBytes += o.Size
		}
	}
	for _, name := range covered(cfg) {
		if a.Enabled(name) {
			out.ReposOn++
		}
	}
	return out
}
