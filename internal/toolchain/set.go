package toolchain

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"
)

// Entry is one preset item: a tool and the spec resolved when it installs.
type Entry struct{ Tool, Spec string }

// Popular is the preset setup.sh installs on a first install.
var Popular = []Entry{
	{"node", "22"}, {"node", "24"},
	{"dotnet", "8.0"}, {"dotnet", "10.0"},
	{"python", "3.13"}, {"python", "3.14"},
	{"go", "latest"},
	{"java", "21"}, {"java", "25"},
}

const availableTTL = time.Hour

// ownedDirs are the tool cache folders an installer owns, plus .tmp.
var ownedDirs = map[string]bool{"node": true, "go": true, "Python": true, javaDir: true, "dotnet": true, ".tmp": true}

type cachedChoices struct {
	at time.Time
	cs []Choice
}

// Set is every installer, keyed by the tool name ghr uses.
type Set struct {
	e     *Env
	tools map[string]Installer
	now   func() time.Time

	mu    sync.Mutex
	avail map[string]cachedChoices
}

func New(e *Env) *Set {
	return &Set{
		e:   e,
		now: time.Now,
		tools: map[string]Installer{
			"node": newNode(e), "go": newGo(e), "python": newPython(e), "java": newJava(e), "dotnet": newDotnet(e),
		},
		avail: map[string]cachedChoices{},
	}
}

func (s *Set) Tools() []string {
	out := make([]string, 0, len(s.tools))
	for t := range s.tools {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func (s *Set) Get(tool string) (Installer, error) {
	i, ok := s.tools[tool]
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnknownTool, tool)
	}
	return i, nil
}

// Available is the installer's list, kept for an hour; errors are not kept.
func (s *Set) Available(ctx context.Context, tool string) ([]Choice, error) {
	i, err := s.Get(tool)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	c, ok := s.avail[tool]
	s.mu.Unlock()
	if ok && s.now().Sub(c.at) < availableTTL {
		return c.cs, nil
	}
	cs, err := i.Available(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.avail[tool] = cachedChoices{s.now(), cs}
	s.mu.Unlock()
	return cs, nil
}

// Installed lists every installer's versions, tools in name order.
func (s *Set) Installed() ([]Installed, error) {
	var out []Installed
	for _, t := range s.Tools() {
		got, err := s.tools[t].Installed()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", t, err)
		}
		out = append(out, got...)
	}
	return out, nil
}

// Other names the tool cache folders no installer owns, such as PyPy or
// Ruby that jobs' own setup-* steps added.
func (s *Set) Other() ([]string, error) {
	es, err := os.ReadDir(s.e.Root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range es {
		if e.IsDir() && !ownedDirs[e.Name()] {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

func (s *Set) Root() string { return s.e.Root }

func (s *Set) CleanTmp() error { return s.e.CleanTmp() }
