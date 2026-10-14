// Package history stores finished jobs as JSON lines.
package history

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

// maxLine is the longest line readAll decodes. An entry ghr writes is a few
// hundred bytes.
const maxLine = 1 << 20

type Store struct {
	Path     string
	mu       sync.Mutex
	writes   int64
	onChange func()
	skipped  int // lines the last read skipped; readAll logs a change
	cache    *snapshot
}

// snapshot is the parsed content of the file as it was when fi described it.
type snapshot struct {
	fi      os.FileInfo
	entries []model.HistoryEntry
	skipped int
}

// current reports whether fi still describes the file the snapshot was read
// from. A rewrite that leaves the same size, modification time and file
// unchanged is not noticed.
func (c *snapshot) current(fi os.FileInfo) bool {
	return c != nil && os.SameFile(c.fi, fi) && c.fi.Size() == fi.Size() && c.fi.ModTime().Equal(fi.ModTime())
}

// SetOnChange installs fn, which Append and Prune call after a counted write,
// once the store's lock is released; nil removes it.
func (s *Store) SetOnChange(fn func()) {
	s.mu.Lock()
	s.onChange = fn
	s.mu.Unlock()
}

// Writes counts the appends that wrote a line and the prunes that replaced
// the file.
func (s *Store) Writes() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writes
}

// counted bumps the write counter when wrote is set and returns the hook to
// call once s.mu is released. s.mu is held.
func (s *Store) counted(wrote bool) func() {
	if !wrote {
		return nil
	}
	s.writes++
	return s.onChange
}

// Append records e unless an entry with the same instance ID and run ID is
// already stored, so a finalization repeated after a crash adds no duplicate.
func (s *Store) Append(e model.HistoryEntry) error {
	s.mu.Lock()
	wrote, err := s.append(e)
	fn := s.counted(wrote)
	s.mu.Unlock()
	if fn != nil {
		fn()
	}
	return err
}

func (s *Store) append(e model.HistoryEntry) (bool, error) {
	all, _, err := s.readAll()
	if err != nil {
		return false, err
	}
	for _, x := range all {
		if x.ID == e.ID && x.RunID == e.RunID {
			return false, nil
		}
	}
	line, err := json.Marshal(e)
	if err != nil {
		return false, err
	}
	f, err := os.OpenFile(s.Path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0o600)
	if err != nil {
		return false, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return false, err
	}
	warm := s.cache.current(st)
	if st.Mode().Perm()&^0o600 != 0 {
		if err := f.Chmod(0o600); err != nil {
			return false, err
		}
	}
	// A crash mid-write can leave a fragment with no trailing newline; start a
	// new line so this record is not merged into the fragment and lost.
	if st.Size() > 0 {
		last := make([]byte, 1)
		if _, err := f.ReadAt(last, st.Size()-1); err != nil {
			return false, err
		}
		if last[0] != '\n' {
			line = append([]byte{'\n'}, line...)
		}
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return false, err
	}
	if err := f.Sync(); err != nil {
		return true, err
	}
	after, err := f.Stat()
	if err != nil {
		return true, err
	}
	if err := f.Close(); err != nil {
		return true, err
	}
	if warm {
		s.cache = &snapshot{fi: after, entries: append(s.cache.entries, e), skipped: s.cache.skipped}
	}
	return true, nil
}

// readAll returns a copy of the stored entries and how many lines it
// skipped: a line longer than maxLine, or one that is not valid JSON. It
// parses the file only when it differs from the one the last call parsed. It
// logs that number when it differs from the previous read's and is not zero.
// s.mu is held.
func (s *Store) readAll() ([]model.HistoryEntry, int, error) {
	fi, err := os.Stat(s.Path)
	if os.IsNotExist(err) {
		s.skipped, s.cache = 0, nil
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	if s.cache.current(fi) {
		return slices.Clone(s.cache.entries), s.cache.skipped, nil
	}
	s.cache = nil
	f, err := os.Open(s.Path)
	if os.IsNotExist(err) {
		s.skipped = 0
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	// Taken before the read: a file that grows during it then no longer
	// matches and is read again.
	fi, err = f.Stat()
	if err != nil {
		return nil, 0, err
	}
	var out []model.HistoryEntry
	skipped := 0
	r := bufio.NewReaderSize(f, 64*1024)
	var line []byte
	for {
		// A line arrives in buffer-sized pieces; once it is too long the rest
		// of it is read and thrown away.
		line = line[:0]
		long := false
		var rerr error
		for {
			var chunk []byte
			chunk, rerr = r.ReadSlice('\n')
			if !long {
				line = append(line, chunk...)
				long = len(bytes.TrimSuffix(line, []byte{'\n'})) > maxLine
			}
			if rerr != bufio.ErrBufferFull {
				break
			}
		}
		if rerr != nil && rerr != io.EOF {
			return nil, 0, rerr
		}
		if rerr == io.EOF && len(line) == 0 {
			break
		}
		var e model.HistoryEntry
		if !long && json.Unmarshal(bytes.TrimSuffix(line, []byte{'\n'}), &e) == nil {
			out = append(out, e)
		} else {
			skipped++
		}
		if rerr == io.EOF {
			break
		}
	}
	if skipped != s.skipped {
		s.skipped = skipped
		if skipped > 0 {
			log.Printf("history: %s: skipped %d line(s) longer than 1 MiB or not valid JSON", s.Path, skipped)
		}
	}
	s.cache = &snapshot{fi: fi, entries: out, skipped: skipped}
	return slices.Clone(out), skipped, nil
}

// Query returns entries by FinishedAt, newest first, filtered by repo (in any
// case) and conclusion when non-empty, and to entries finished at or after
// since when since is not zero. Conclusion "failure" matches every conclusion
// model.Failed accepts; any other matches exactly. limit <= 0 means no limit. Lines are appended in cleanup
// order, which is not finish order.
func (s *Store) Query(repo, conclusion string, since time.Time, limit int) ([]model.HistoryEntry, error) {
	s.mu.Lock()
	all, _, err := s.readAll()
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(all, func(a, b int) bool { return all[a].FinishedAt.After(all[b].FinishedAt) })
	var out []model.HistoryEntry
	for _, e := range all {
		if (repo == "" || strings.EqualFold(e.Repo, repo)) && matchConclusion(e.Conclusion, conclusion) &&
			(since.IsZero() || !e.FinishedAt.Before(since)) {
			out = append(out, e)
			if limit > 0 && len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

func matchConclusion(got, want string) bool {
	if want == "failure" {
		return model.Failed(got)
	}
	return want == "" || got == want
}

// Prune drops entries that finished before cutoff and lines the read skipped.
// It replaces the file atomically, and only when there is something to drop.
func (s *Store) Prune(cutoff time.Time) error {
	s.mu.Lock()
	wrote, err := s.prune(cutoff)
	fn := s.counted(wrote)
	s.mu.Unlock()
	if fn != nil {
		fn()
	}
	return err
}

func (s *Store) prune(cutoff time.Time) (bool, error) {
	all, skipped, err := s.readAll()
	if err != nil {
		return false, err
	}
	keep := make([]model.HistoryEntry, 0, len(all))
	for _, e := range all {
		if !e.FinishedAt.Before(cutoff) {
			keep = append(keep, e)
		}
	}
	if len(keep) == len(all) && skipped == 0 {
		return false, nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".history-*.jsonl")
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp.Name())
	w := bufio.NewWriter(tmp)
	for _, e := range keep {
		line, _ := json.Marshal(e)
		w.Write(append(line, '\n'))
	}
	if err := w.Flush(); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return false, err
	}
	fi, statErr := tmp.Stat()
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(tmp.Name(), s.Path); err != nil {
		return false, err
	}
	s.skipped, s.cache = 0, nil
	if statErr == nil {
		s.cache = &snapshot{fi: fi, entries: keep}
	}
	return true, nil
}
