// Package history stores finished jobs as JSON lines.
package history

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

type Store struct {
	Path string
	mu   sync.Mutex
}

// Append records e unless an entry with the same instance ID and run ID is
// already stored, so a finalization repeated after a crash adds no duplicate.
func (s *Store) Append(e model.HistoryEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readAll()
	if err != nil {
		return err
	}
	for _, x := range all {
		if x.ID == e.ID && x.RunID == e.RunID {
			return nil
		}
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.Path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	// A crash mid-write can leave a fragment with no trailing newline; start a
	// new line so this record is not merged into the fragment and lost.
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if st.Size() > 0 {
		last := make([]byte, 1)
		if _, err := f.ReadAt(last, st.Size()-1); err != nil {
			return err
		}
		if last[0] != '\n' {
			line = append([]byte{'\n'}, line...)
		}
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return f.Close()
}

func (s *Store) readAll() ([]model.HistoryEntry, error) {
	f, err := os.Open(s.Path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []model.HistoryEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var e model.HistoryEntry
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			out = append(out, e)
		}
	}
	return out, sc.Err()
}

// Query returns entries by FinishedAt, newest first, filtered by repo and
// conclusion when non-empty. limit <= 0 means no limit. Lines are appended in
// cleanup order, which is not finish order.
func (s *Store) Query(repo, conclusion string, limit int) ([]model.HistoryEntry, error) {
	s.mu.Lock()
	all, err := s.readAll()
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(all, func(a, b int) bool { return all[a].FinishedAt.After(all[b].FinishedAt) })
	var out []model.HistoryEntry
	for _, e := range all {
		if (repo == "" || e.Repo == repo) && (conclusion == "" || e.Conclusion == conclusion) {
			out = append(out, e)
			if limit > 0 && len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

// Prune drops entries that finished before cutoff, rewriting the file atomically.
func (s *Store) Prune(cutoff time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readAll()
	if err != nil || all == nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".history-*.jsonl")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	w := bufio.NewWriter(tmp)
	for _, e := range all {
		if e.FinishedAt.Before(cutoff) {
			continue
		}
		line, _ := json.Marshal(e)
		w.Write(append(line, '\n'))
	}
	if err := w.Flush(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.Path)
}
