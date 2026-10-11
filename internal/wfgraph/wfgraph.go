// Package wfgraph reads a workflow file's job dependencies and lays the
// run's jobs out as a graph.
package wfgraph

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/darkraise/ghr/internal/model"
)

var (
	ErrInvalid  = errors.New("wfgraph: invalid workflow")
	ErrMismatch = errors.New("wfgraph: a job matches no workflow job")
)

// Limits on what a workflow file may ask of the daemon. Build compiles one
// pattern per job from its label, and an alias lets thousands of jobs share
// one very long label, so both are bounded here.
const (
	maxJobs  = 1000
	maxLabel = 512
)

// Job is one workflow job: Name is its YAML name, "" when it has none.
type Job struct {
	Key, Name    string
	Needs        []string
	Matrix, Uses bool
}

type Workflow struct {
	Jobs []Job
}

type APIJob struct {
	ID   int64
	Name string
}

// needsList takes needs as one job ID or a list of them.
type needsList []string

func (n *needsList) UnmarshalYAML(v *yaml.Node) error {
	if v.Kind == yaml.ScalarNode {
		*n = []string{v.Value}
		return nil
	}
	var list []string
	if err := v.Decode(&list); err != nil {
		return err
	}
	*n = list
	return nil
}

// strategy tells whether a job's strategy has a matrix. A strategy written as
// one expression counts as having one: what it expands to is not in the file.
type strategy struct {
	matrix bool
}

func (s *strategy) UnmarshalYAML(v *yaml.Node) error {
	if v.Kind == yaml.ScalarNode {
		s.matrix = true
		return nil
	}
	var m struct {
		Matrix any `yaml:"matrix"`
	}
	if err := v.Decode(&m); err != nil {
		return err
	}
	s.matrix = m.Matrix != nil
	return nil
}

type rawJob struct {
	Name     string    `yaml:"name"`
	Needs    needsList `yaml:"needs"`
	Strategy strategy  `yaml:"strategy"`
	Uses     string    `yaml:"uses"`
}

// Parse reads the jobs of a workflow file in file order. Each job body is
// decoded on its own, which resolves aliases and merge keys.
func Parse(data []byte) (*Workflow, error) {
	var doc struct {
		Jobs yaml.Node `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	jobs := &doc.Jobs
	for jobs.Kind == yaml.AliasNode {
		jobs = jobs.Alias
	}
	if jobs.Kind != yaml.MappingNode || len(jobs.Content) == 0 {
		return nil, fmt.Errorf("%w: no jobs", ErrInvalid)
	}
	if len(jobs.Content)/2 > maxJobs {
		return nil, fmt.Errorf("%w: more than %d jobs", ErrInvalid, maxJobs)
	}
	w := &Workflow{}
	seen := map[string]bool{}
	for i := 0; i+1 < len(jobs.Content); i += 2 {
		key := jobs.Content[i].Value
		if seen[key] {
			return nil, fmt.Errorf("%w: job %s appears twice", ErrInvalid, key)
		}
		seen[key] = true
		var raw rawJob
		if err := jobs.Content[i+1].Decode(&raw); err != nil {
			return nil, fmt.Errorf("%w: job %s: %v", ErrInvalid, jobs.Content[i].Value, err)
		}
		if len(jobs.Content[i].Value) > maxLabel || len(raw.Name) > maxLabel {
			return nil, fmt.Errorf("%w: a job key or name is longer than %d bytes", ErrInvalid, maxLabel)
		}
		w.Jobs = append(w.Jobs, Job{Key: jobs.Content[i].Value, Name: raw.Name, Needs: raw.Needs,
			Matrix: raw.Strategy.matrix, Uses: raw.Uses != ""})
	}
	if err := validate(w); err != nil {
		return nil, err
	}
	return w, nil
}

// validate refuses a needs that names an unknown job or forms a cycle;
// GitHub refuses such files before a run exists.
func validate(w *Workflow) error {
	index := map[string]int{}
	for i, j := range w.Jobs {
		index[j.Key] = i
	}
	for _, j := range w.Jobs {
		for _, n := range j.Needs {
			if _, ok := index[n]; !ok {
				return fmt.Errorf("%w: job %s needs unknown job %s", ErrInvalid, j.Key, n)
			}
		}
	}
	const (
		unseen = iota
		visiting
		done
	)
	state := make([]int, len(w.Jobs))
	var visit func(i int) error
	visit = func(i int) error {
		switch state[i] {
		case visiting:
			return fmt.Errorf("%w: needs cycle through job %s", ErrInvalid, w.Jobs[i].Key)
		case done:
			return nil
		}
		state[i] = visiting
		for _, n := range w.Jobs[i].Needs {
			if err := visit(index[n]); err != nil {
				return err
			}
		}
		state[i] = done
		return nil
	}
	for i := range w.Jobs {
		if err := visit(i); err != nil {
			return err
		}
	}
	return nil
}

var exprRe = regexp.MustCompile(`\$\{\{.*?\}\}`)

type pattern struct {
	label  string
	prefix string
	// literal is how many bytes of the label are literal text, not expressions.
	literal int
	re      *regexp.Regexp
}

// better reports whether p is the closer match than q for a name both match:
// the longer literal prefix wins, then the more literal text in total.
func (p pattern) better(q pattern) bool {
	if len(p.prefix) != len(q.prefix) {
		return len(p.prefix) > len(q.prefix)
	}
	return p.literal > q.literal
}

// newPattern matches the names GitHub gives a workflow job's runs: each
// expression stands for any text, a matrix job without an expression in its
// name may get " (…)", and a reusable-workflow caller " / …".
func newPattern(j Job) pattern {
	label := j.Name
	if label == "" {
		label = j.Key
	}
	lits := exprRe.Split(label, -1)
	quoted := make([]string, len(lits))
	literal := 0
	for i, l := range lits {
		quoted[i] = regexp.QuoteMeta(l)
		literal += len(l)
	}
	expr := strings.Join(quoted, ".*?")
	if j.Matrix && len(lits) == 1 {
		expr += `(?: \(.*\))?`
	}
	if j.Uses {
		expr += `(?: / .*)?`
	}
	return pattern{label: label, prefix: lits[0], literal: literal, re: regexp.MustCompile(`(?s)^` + expr + `$`)}
}

// Build lays the run's jobs out per workflow job. A job goes to the workflow
// job whose name is its exact text, otherwise to the matching pattern with
// the longest literal prefix; among equal prefixes the pattern with the most
// literal text in total wins, and a remaining tie goes to the earlier job.
// Workflow jobs with no run are dropped and their needs pass through to what
// they need.
func Build(w *Workflow, jobs []APIJob) ([]model.GraphNode, error) {
	if err := validate(w); err != nil {
		return nil, err
	}
	pats := make([]pattern, len(w.Jobs))
	for i, j := range w.Jobs {
		pats[i] = newPattern(j)
	}
	ids := make([][]int64, len(w.Jobs))
	for _, a := range jobs {
		best := -1
		for i, p := range pats {
			if p.label == a.Name {
				best = i
				break
			}
		}
		if best < 0 {
			for i, p := range pats {
				if p.re.MatchString(a.Name) && (best < 0 || p.better(pats[best])) {
					best = i
				}
			}
		}
		if best < 0 {
			return nil, fmt.Errorf("%w: %s", ErrMismatch, a.Name)
		}
		ids[best] = append(ids[best], a.ID)
	}

	index := map[string]int{}
	for i, j := range w.Jobs {
		index[j.Key] = i
	}
	needs := make([][]string, len(w.Jobs))
	resolved := make([]bool, len(w.Jobs))
	var effective func(i int) []string
	effective = func(i int) []string {
		if resolved[i] {
			return needs[i]
		}
		out := []string{}
		for _, n := range w.Jobs[i].Needs {
			k := index[n]
			if len(ids[k]) > 0 {
				out = append(out, n)
			} else {
				out = append(out, effective(k)...)
			}
		}
		slices.Sort(out)
		out = slices.Compact(out)
		needs[i], resolved[i] = out, true
		return out
	}
	cols := map[string]int{}
	var column func(i int) int
	column = func(i int) int {
		if c, ok := cols[w.Jobs[i].Key]; ok {
			return c
		}
		c := 0
		for _, n := range effective(i) {
			c = max(c, column(index[n])+1)
		}
		cols[w.Jobs[i].Key] = c
		return c
	}

	nodes := []model.GraphNode{}
	for i, j := range w.Jobs {
		if len(ids[i]) == 0 {
			continue
		}
		name := j.Name
		if name == "" {
			name = j.Key
		}
		nodes = append(nodes, model.GraphNode{Key: j.Key, Name: name, Column: column(i), Needs: effective(i),
			JobIDs: ids[i], Group: len(ids[i]) > 1 || j.Matrix || j.Uses})
	}
	sort.SliceStable(nodes, func(a, b int) bool { return nodes[a].Column < nodes[b].Column })
	return nodes, nil
}
