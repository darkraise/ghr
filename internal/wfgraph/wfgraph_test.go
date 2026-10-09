package wfgraph

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/darkraise/ghr/internal/model"
)

const ciYAML = `
name: ci
on: [push, pull_request]
jobs:
  pick-runner:
    name: Pick runner
    runs-on: ubuntu-latest
  backend: &backend
    name: Backend tests
    needs: pick-runner
    runs-on: ubuntu-latest
  frontend-tests:
    name: Frontend tests (${{ matrix.shard }}/2)
    needs: [pick-runner]
    strategy:
      matrix:
        shard: [1, 2]
  e2e:
    name: E2E
    strategy: { matrix: { suite: [api] } }
  lint:
    <<: *backend
    name: Lint
  docs:
    needs: [backend]
    if: false
  release:
    needs: [docs, frontend-tests]
    uses: ./.github/workflows/release.yml
`

func TestParse(t *testing.T) {
	w, err := Parse([]byte(ciYAML))
	if err != nil {
		t.Fatal(err)
	}
	want := []Job{
		{Key: "pick-runner", Name: "Pick runner"},
		{Key: "backend", Name: "Backend tests", Needs: []string{"pick-runner"}},
		{Key: "frontend-tests", Name: "Frontend tests (${{ matrix.shard }}/2)", Needs: []string{"pick-runner"}, Matrix: true},
		{Key: "e2e", Name: "E2E", Matrix: true},
		{Key: "lint", Name: "Lint", Needs: []string{"pick-runner"}},
		{Key: "docs", Needs: []string{"backend"}},
		{Key: "release", Needs: []string{"docs", "frontend-tests"}, Uses: true},
	}
	if !reflect.DeepEqual(w.Jobs, want) {
		t.Fatalf("jobs\n%+v\nwant\n%+v", w.Jobs, want)
	}
}

func TestParseRefusesInvalidFiles(t *testing.T) {
	for name, src := range map[string]string{
		"not YAML":      "jobs: [",
		"no jobs":       "name: ci\non: push\n",
		"jobs a list":   "jobs: [a, b]\n",
		"unknown needs": "jobs:\n  a:\n    needs: b\n",
		"cycle":         "jobs:\n  a:\n    needs: c\n  b:\n    needs: a\n  c:\n    needs: [b]\n",
		"self cycle":    "jobs:\n  a:\n    needs: a\n",
		"bad needs":     "jobs:\n  a:\n    needs: {x: 1}\n",
		"duplicate job": "jobs:\n  a: {runs-on: x}\n  a: {runs-on: y}\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(src)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("err %v", err)
			}
		})
	}
}

func TestParseBoundsWhatBuildWouldCompile(t *testing.T) {
	long := strings.Repeat("x", maxLabel+1)
	var many strings.Builder
	many.WriteString("jobs:\n")
	for i := 0; i <= maxJobs; i++ {
		fmt.Fprintf(&many, "  j%d: {runs-on: x}\n", i)
	}
	for name, src := range map[string]string{
		"a long name":          "jobs:\n  a:\n    name: " + long + "\n",
		"a long key":           "jobs:\n  " + long + ": {runs-on: x}\n",
		"a long name by alias": "jobs:\n  a: {name: &n " + long + "}\n  b: {name: *n}\n",
		"too many jobs":        many.String(),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(src)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("Parse err = %v, want ErrInvalid", err)
			}
		})
	}
	ok := "jobs:\n  a:\n    name: " + strings.Repeat("x", maxLabel) + "\n"
	if _, err := Parse([]byte(ok)); err != nil {
		t.Fatalf("a name of %d bytes: %v", maxLabel, err)
	}
}

func TestBuildLaysOutTheRun(t *testing.T) {
	w, err := Parse([]byte(ciYAML))
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := Build(w, []APIJob{
		{1001, "Pick runner"},
		{1002, "Backend tests"},
		{1003, "Frontend tests (1/2)"},
		{1004, "Frontend tests (2/2)"},
		{1005, "E2E (api)"},
		{1006, "release / publish"},
		{1007, "Lint"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []model.GraphNode{
		{Key: "pick-runner", Name: "Pick runner", Column: 0, Needs: []string{}, JobIDs: []int64{1001}},
		{Key: "e2e", Name: "E2E", Column: 0, Needs: []string{}, JobIDs: []int64{1005}, Group: true},
		{Key: "backend", Name: "Backend tests", Column: 1, Needs: []string{"pick-runner"}, JobIDs: []int64{1002}},
		{Key: "frontend-tests", Name: "Frontend tests (${{ matrix.shard }}/2)", Column: 1, Needs: []string{"pick-runner"}, JobIDs: []int64{1003, 1004}, Group: true},
		{Key: "lint", Name: "Lint", Column: 1, Needs: []string{"pick-runner"}, JobIDs: []int64{1007}},
		// docs never ran, so release needs what docs needed.
		{Key: "release", Name: "release", Column: 2, Needs: []string{"backend", "frontend-tests"}, JobIDs: []int64{1006}, Group: true},
	}
	if !reflect.DeepEqual(nodes, want) {
		t.Fatalf("nodes\n%+v\nwant\n%+v", nodes, want)
	}
}

func TestBuildMatching(t *testing.T) {
	for _, tc := range []struct {
		name string
		jobs []Job
		api  string
		want string // the key the API job lands on; "" means ErrMismatch
	}{
		{"expression stands for any text", []Job{{Key: "t", Name: "Test (${{ matrix.os }}, ${{ matrix.v }})"}}, "Test (linux, 1.26)", "t"},
		{"unexpanded name of a skipped matrix job", []Job{{Key: "t", Name: "Test (${{ matrix.os }})"}}, "Test (${{ matrix.os }})", "t"},
		{"matrix suffix", []Job{{Key: "e2e", Name: "E2E", Matrix: true}}, "E2E (api, chrome)", "e2e"},
		{"no suffix without a matrix", []Job{{Key: "b", Name: "Build"}}, "Build (x)", ""},
		{"reusable workflow caller", []Job{{Key: "rel", Uses: true}}, "rel / publish", "rel"},
		{"key when there is no name", []Job{{Key: "lint"}}, "lint", "lint"},
		{"longest literal prefix wins", []Job{{Key: "any", Name: "Test ${{ matrix.os }}"}, {Key: "linux", Name: "Test linux ${{ matrix.v }}"}}, "Test linux 1.26", "linux"},
		{"ties go to the earlier job", []Job{{Key: "a", Name: "Test ${{ matrix.x }}"}, {Key: "b", Name: "Test ${{ matrix.y }}"}}, "Test 1", "a"},
		{"exact text wins over a longer prefix", []Job{{Key: "a", Name: "Deploy ${{ matrix.env }}"}, {Key: "b", Name: "Deploy $${{ matrix.n }}"}}, "Deploy ${{ matrix.env }}", "a"},
		{"no workflow job matches", []Job{{Key: "a", Name: "Build"}}, "Deploy", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nodes, err := Build(&Workflow{Jobs: tc.jobs}, []APIJob{{ID: 1, Name: tc.api}})
			if tc.want == "" {
				if !errors.Is(err, ErrMismatch) {
					t.Fatalf("err %v nodes %+v", err, nodes)
				}
				return
			}
			if err != nil || len(nodes) != 1 || nodes[0].Key != tc.want {
				t.Fatalf("nodes %+v err %v", nodes, err)
			}
		})
	}
}

func TestBuildRefusesAnInvalidWorkflow(t *testing.T) {
	w := &Workflow{Jobs: []Job{{Key: "a", Needs: []string{"b"}}, {Key: "b", Needs: []string{"a"}}}}
	if _, err := Build(w, []APIJob{{1, "a"}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err %v", err)
	}
}
