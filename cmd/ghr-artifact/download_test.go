package main

import (
	"archive/zip"
	"bytes"
	"context"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/darkraise/ghr/internal/artifact"
	"github.com/darkraise/ghr/internal/s3"
	"github.com/darkraise/ghr/internal/s3/s3test"
)

// seed stores a zip of files under the run's key for name, with the digest
// ghr-artifact records; a non-empty digest replaces it.
func (h *harness) seed(t *testing.T, runID int64, name string, files map[string]string, digest string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for rel, body := range files {
		w, err := zw.Create(rel)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if digest == "" {
		digest = digestOf(buf.Bytes())
	}
	meta := artifact.Meta{RunID: runID, Attempt: 1, Files: len(files), Digest: digest}
	h.srv.Put(artifact.ObjectKey("darkraise", "demo", runID, name), s3test.Object{Body: buf.Bytes(), Meta: meta.Map()})
}

// tree lists every file under dir as "/"-separated path → content.
func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	return out
}

func TestDownload(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		wantPath string
		want     map[string]string
	}{
		{"by name into the workspace", []string{"--name", "dist-a"}, "",
			map[string]string{"a.txt": "a"}},
		{"by name into a path", []string{"--name", "dist-a", "--path", "out"}, "out",
			map[string]string{"a.txt": "a"}},
		{"pattern gives each artifact its directory", []string{"--pattern", "dist-*", "--path", "out"}, "out",
			map[string]string{"dist-a/a.txt": "a", "dist-b/sub/b.txt": "b"}},
		{"pattern with merge-multiple", []string{"--pattern", "dist-*", "--path", "out", "--merge-multiple"}, "out",
			map[string]string{"a.txt": "a", "sub/b.txt": "b"}},
		{"pattern with one match still gets a directory", []string{"--pattern", "other", "--path", "out"}, "out",
			map[string]string{"other/o.txt": "o"}},
		{"everything", []string{"--path", "out"}, "out",
			map[string]string{"dist-a/a.txt": "a", "dist-b/sub/b.txt": "b", "other/o.txt": "o"}},
		{"name wins over pattern", []string{"--name", "other", "--pattern", "dist-*", "--path", "out"}, "out",
			map[string]string{"o.txt": "o"}},
		{"another run", []string{"--name", "old", "--run-id", "42", "--path", "out"}, "out",
			map[string]string{"old.txt": "old"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.seed(t, 100, "dist-a", map[string]string{"a.txt": "a"}, "")
			h.seed(t, 100, "dist-b", map[string]string{"sub/b.txt": "b"}, "")
			h.seed(t, 100, "other", map[string]string{"o.txt": "o"}, "")
			h.seed(t, 42, "old", map[string]string{"old.txt": "old"}, "")
			code, stdout, stderr := h.run(t, append([]string{"download"}, tc.args...)...)
			if code != 0 {
				t.Fatalf("code %d\nstdout %s\nstderr %s", code, stdout, stderr)
			}
			dir := filepath.Join(h.ws, tc.wantPath)
			if got := tree(t, dir); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("files %v, want %v", got, tc.want)
			}
			if out := h.outputs(t); !reflect.DeepEqual(out, map[string]string{"download-path": filepath.Clean(dir)}) {
				t.Errorf("outputs %v", out)
			}
			if left := h.tempLeft(t); len(left) != 0 {
				t.Errorf("left in RUNNER_TEMP: %v", left)
			}
		})
	}
}

func TestDownloadRelativeWorkspace(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		wantPath string
	}{
		{"default path", nil, ""},
		{"relative path", []string{"--path", "out"}, "out"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.seed(t, 100, "dist", map[string]string{"a.txt": "a"}, "")
			t.Chdir(filepath.Dir(h.ws))
			h.env["GITHUB_WORKSPACE"] = filepath.Base(h.ws)
			code, stdout, stderr := h.run(t, append([]string{"download", "--name", "dist"}, tc.args...)...)
			if code != 0 {
				t.Fatalf("code %d\nstdout %s\nstderr %s", code, stdout, stderr)
			}
			dir := filepath.Join(h.ws, tc.wantPath)
			if got := tree(t, h.ws); !reflect.DeepEqual(got, map[string]string{filepath.ToSlash(filepath.Join(tc.wantPath, "a.txt")): "a"}) {
				t.Errorf("files %v", got)
			}
			if out := h.outputs(t); !reflect.DeepEqual(out, map[string]string{"download-path": dir}) {
				t.Errorf("outputs %v, want download-path %s", out, dir)
			}
		})
	}
}

func TestDownloadMissing(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"unknown name", []string{"--name", "nope"}, "No artifact matched the name nope in run 100 in the artifact store.\n"},
		{"pattern without a match", []string{"--pattern", "nope-*"}, "No artifact matched the pattern nope-* in run 100 in the artifact store.\n"},
		{"empty run", []string{"--run-id", "7"}, "No artifact matched run 7 in the artifact store.\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.seed(t, 100, "dist", map[string]string{"a.txt": "a"}, "")
			code, stdout, stderr := h.run(t, append([]string{"download", "--path", "out"}, tc.args...)...)
			if code != 3 || stdout != tc.want || stderr != "" {
				t.Errorf("code %d stdout %q stderr %q", code, stdout, stderr)
			}
			if out := h.outputs(t); !reflect.DeepEqual(out, map[string]string{"missing": "true"}) {
				t.Errorf("outputs %v", out)
			}
			if _, err := os.Stat(filepath.Join(h.ws, "out")); err == nil {
				t.Errorf("the path was created")
			}
		})
	}
}

func TestDownloadDigestMismatch(t *testing.T) {
	h := newHarness(t)
	h.seed(t, 100, "dist", map[string]string{"a.txt": "a"}, "sha256:"+strings.Repeat("0", 64))
	code, _, stderr := h.run(t, "download", "--name", "dist", "--path", "out")
	if code != 1 || stderr != "ghr-artifact: dist.zip is damaged: its digest does not match; upload it again\n" {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(h.ws, "out", "a.txt")); err == nil {
		t.Errorf("a damaged artifact was extracted")
	}
	if len(h.outputs(t)) != 0 {
		t.Errorf("outputs written: %v", h.outputs(t))
	}
	if left := h.tempLeft(t); len(left) != 0 {
		t.Errorf("left in RUNNER_TEMP: %v", left)
	}
}

func TestDownloadStoreErrorsDoNotFallBack(t *testing.T) {
	for _, tc := range []struct {
		args     []string
		op, code string
		status   int
		want     string
	}{
		{[]string{"--name", "dist"}, "get", "NoSuchBucket", 404, "ghr-artifact: bucket gha does not exist on https://127.0.0.1:"},
		{[]string{"--name", "dist"}, "get", "AccessDenied", 403, " refused the job key (AccessDenied"},
		{nil, "list", "NoSuchBucket", 404, "ghr-artifact: bucket gha does not exist on https://127.0.0.1:"},
		{nil, "list", "AccessDenied", 403, " refused the job key (AccessDenied"},
	} {
		h := newHarness(t)
		h.seed(t, 100, "dist", map[string]string{"a.txt": "a"}, "")
		h.srv.FailNext(tc.op, tc.status, tc.code)
		code, _, stderr := h.run(t, append([]string{"download", "--path", "out"}, tc.args...)...)
		if code != 1 || !strings.Contains(stderr, tc.want) {
			t.Errorf("%s %s: code %d stderr %q", tc.op, tc.code, code, stderr)
		}
		if len(h.outputs(t)) != 0 {
			t.Errorf("%s %s: outputs written: %v", tc.op, tc.code, h.outputs(t))
		}
	}
}

func TestDescribeA403(t *testing.T) {
	st := artifact.Settings{Endpoint: "https://s3.example.com", Bucket: "gha"}
	for _, tc := range []struct {
		name string
		err  *s3.Error
		want string
	}{
		{"a proxy's page", &s3.Error{Status: 403},
			"https://s3.example.com answered HTTP 403 without an S3 error; a proxy in front of the store may have blocked the request"},
		{"the store's refusal", &s3.Error{Status: 403, Code: "AccessDenied", Message: "Access Denied."},
			"https://s3.example.com refused the job key (AccessDenied: Access Denied.); check the job key's policy on the store"},
	} {
		if got := describe(tc.err, st).Error(); !strings.HasPrefix(got, tc.want) {
			t.Errorf("%s: %q, want it to start %q", tc.name, got, tc.want)
		}
	}
}

func TestDescribeAStop(t *testing.T) {
	st := artifact.Settings{Endpoint: "https://s3.example.com", Bucket: "gha"}
	for _, tc := range []struct {
		err  error
		want string
	}{
		{&url.Error{Op: "Get", URL: st.Endpoint, Err: context.Canceled}, "stopped by a signal, as when the job is cancelled"},
		{&url.Error{Op: "Put", URL: st.Endpoint, Err: context.DeadlineExceeded}, "gave up after 6h0m0s"},
	} {
		if got := describe(tc.err, st).Error(); got != tc.want {
			t.Errorf("%v: %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestDownloadSkipsUnsafeNames(t *testing.T) {
	h := newHarness(t)
	h.seed(t, 100, "dist", map[string]string{"a.txt": "a"}, "")
	h.seed(t, 100, "..", map[string]string{"evil.txt": "x"}, "")
	h.srv.Put("darkraise/demo/100/nested/x.zip", s3test.Object{Body: []byte("x")})
	code, _, stderr := h.run(t, "download", "--path", "out")
	if code != 0 {
		t.Fatalf("code %d stderr %s", code, stderr)
	}
	if got := tree(t, h.ws); !reflect.DeepEqual(got, map[string]string{"out/dist/a.txt": "a"}) {
		t.Fatalf("files %v", got)
	}
}

func TestDownloadUsage(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct {
		args       []string
		wantCode   int
		wantStderr string
	}{
		{[]string{"download", "--run-id", "x"}, 2, `ghr-artifact: --run-id "x" is not a run ID`},
		{[]string{"download", "--pattern", "["}, 2, `ghr-artifact: --pattern "[" is not a valid glob`},
		{[]string{"download", "--name", "a:b"}, 1, `ghr-artifact: the artifact name is not valid: "a:b".`},
		{[]string{"download", "--store", "{}"}, 2, "flag provided but not defined: -store"},
		{[]string{"download", "--key", "a:b"}, 2, "flag provided but not defined: -key"},
		{[]string{"download", "--settings", "/nonexistent/artifact-store.json"}, 1,
			"ghr-artifact: ghr's artifact store settings /nonexistent/artifact-store.json do not exist; this job did not run on a ghr runner switched on for self-hosted storage\n"},
	} {
		code, _, stderr := h.run(t, tc.args...)
		if code != tc.wantCode || !strings.HasPrefix(stderr, tc.wantStderr) {
			t.Errorf("%q: code %d stderr %q", tc.args, code, stderr)
		}
	}
	if reqs := h.srv.Requests(); len(reqs) != 0 {
		t.Errorf("requests %v", reqs)
	}
}
