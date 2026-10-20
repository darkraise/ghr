package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/artifact"
	"github.com/darkraise/ghr/internal/s3"
	"github.com/darkraise/ghr/internal/s3/s3test"
)

const (
	testKeyID  = "JOBKEY01"
	testSecret = "job-secret-never-printed"
)

type harness struct {
	srv    *s3test.Server
	env    map[string]string
	ws     string
	temp   string
	output string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	srv := s3test.NewServer(t, "gha")
	srv.CheckAuth(s3.Credentials{AccessKeyID: testKeyID, SecretAccessKey: testSecret}, "us-east-1")
	orig := newClient
	newClient = func(store artifact.Store, cred s3.Credentials) (*s3.Client, error) {
		c, err := s3.New(store.Endpoint, store.Bucket, store.Region, cred)
		if err != nil {
			return nil, err
		}
		c.HTTP = srv.HTTPClient()
		return c, nil
	}
	t.Cleanup(func() { newClient = orig })
	dir := t.TempDir()
	h := &harness{srv: srv, ws: filepath.Join(dir, "ws"), temp: filepath.Join(dir, "temp"), output: filepath.Join(dir, "output")}
	for _, d := range []string{h.ws, h.temp} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(h.output, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	store := artifact.Store{V: 1, Endpoint: srv.URL(), Bucket: "gha", Region: "us-east-1", KeyID: testKeyID}
	h.env = map[string]string{
		"GHR_ARTIFACT_STORE": store.JSON(),
		"GHR_ARTIFACT_KEY":   artifact.FormatKey(testKeyID, testSecret),
		"GITHUB_REPOSITORY":  "Darkraise/Demo",
		"GITHUB_RUN_ID":      "100",
		"GITHUB_RUN_ATTEMPT": "1",
		"GITHUB_WORKFLOW":    "CI",
		"GITHUB_JOB":         "build",
		"GITHUB_SHA":         "abc123",
		"GITHUB_REF_NAME":    "main",
		"GITHUB_WORKSPACE":   h.ws,
		"RUNNER_TEMP":        h.temp,
		"GITHUB_OUTPUT":      h.output,
	}
	return h
}

func (h *harness) run(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = run(args, func(k string) string { return h.env[k] }, &out, &errb)
	if strings.Contains(out.String()+errb.String(), testSecret) {
		t.Errorf("the secret reached the output:\n%s\n%s", out.String(), errb.String())
	}
	return code, out.String(), errb.String()
}

func (h *harness) write(t *testing.T, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(h.ws, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func (h *harness) outputs(t *testing.T) map[string]string {
	t.Helper()
	b, err := os.ReadFile(h.output)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			out[k] = v
		}
	}
	return out
}

// tempLeft lists what a command left in RUNNER_TEMP.
func (h *harness) tempLeft(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(h.temp)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func zipEntries(t *testing.T, body []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		b.ReadFrom(rc)
		rc.Close()
		out[f.Name] = b.String()
	}
	return out
}

func digestOf(b []byte) string {
	s := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(s[:])
}

func TestUpload(t *testing.T) {
	tree := map[string]string{
		"dist/a.txt":     "a",
		"dist/sub/b.txt": "b",
		"dist/.env":      "secret-ish",
		"docs/r.md":      "r",
	}
	for _, tc := range []struct {
		name    string
		args    []string
		wantKey string
		want    map[string]string
	}{
		{"directory", []string{"upload", "--name", "dist", "--path", "dist/"},
			"darkraise/demo/100/dist.zip", map[string]string{"a.txt": "a", "sub/b.txt": "b"}},
		{"hidden files", []string{"upload", "--name", "dist", "--path", "dist", "--include-hidden-files"},
			"darkraise/demo/100/dist.zip", map[string]string{".env": "secret-ish", "a.txt": "a", "sub/b.txt": "b"}},
		{"two paths", []string{"upload", "--name", "bundle", "--path", "dist/a.txt\ndocs/\n"},
			"darkraise/demo/100/bundle.zip", map[string]string{"dist/a.txt": "a", "docs/r.md": "r"}},
		{"default name, stored", []string{"upload", "--path", "docs", "--compression-level", "0"},
			"darkraise/demo/100/artifact.zip", map[string]string{"r.md": "r"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.write(t, tree)
			code, stdout, stderr := h.run(t, tc.args...)
			if code != 0 {
				t.Fatalf("code %d\nstdout %s\nstderr %s", code, stdout, stderr)
			}
			if keys := h.srv.Keys(); !reflect.DeepEqual(keys, []string{tc.wantKey}) {
				t.Fatalf("keys %q, want %q", keys, tc.wantKey)
			}
			obj, _ := h.srv.Object(tc.wantKey)
			if got := zipEntries(t, obj.Body); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("entries %v, want %v", got, tc.want)
			}
			name := strings.TrimSuffix(filepath.Base(tc.wantKey), ".zip")
			if obj.ContentType != "application/zip" || obj.ContentDisposition != `attachment; filename="`+name+`.zip"` {
				t.Errorf("content type %q, disposition %q", obj.ContentType, obj.ContentDisposition)
			}
			meta := artifact.ParseMeta(obj.Meta)
			wantMeta := artifact.Meta{RunID: 100, Attempt: 1, Workflow: "CI", Job: "build", SHA: "abc123", Ref: "main",
				Files: len(tc.want), Digest: digestOf(obj.Body), Uploader: "ghr-artifact/dev"}
			if !reflect.DeepEqual(meta, wantMeta) {
				t.Errorf("meta\n got %+v\nwant %+v", meta, wantMeta)
			}
			if out := h.outputs(t); out["object-key"] != tc.wantKey || out["artifact-digest"] != wantMeta.Digest {
				t.Errorf("outputs %v", out)
			}
			if left := h.tempLeft(t); len(left) != 0 {
				t.Errorf("left in RUNNER_TEMP: %v", left)
			}
		})
	}
}

func TestUploadMultipart(t *testing.T) {
	h := newHarness(t)
	data := make([]byte, s3.PartSize+s3.PartSize/2)
	r := rand.New(rand.NewPCG(1, 2))
	for i := range data {
		data[i] = byte(r.Uint32())
	}
	h.write(t, map[string]string{"big/blob.bin": string(data)})
	code, stdout, stderr := h.run(t, "upload", "--name", "big", "--path", "big", "--compression-level", "0")
	if code != 0 {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, stdout, stderr)
	}
	parts := 0
	for _, req := range h.srv.Requests() {
		if strings.Contains(req, "partNumber=") {
			parts++
		}
	}
	if parts != 2 {
		t.Errorf("%d part uploads, want 2:\n%s", parts, strings.Join(h.srv.Requests(), "\n"))
	}
	obj, ok := h.srv.Object("darkraise/demo/100/big.zip")
	if !ok || int64(len(obj.Body)) <= s3.PartSize {
		t.Fatalf("object present %v, %d bytes", ok, len(obj.Body))
	}
	if got := zipEntries(t, obj.Body)["blob.bin"]; got != string(data) {
		t.Errorf("blob.bin differs: %d bytes, want %d", len(got), len(data))
	}
	if d := artifact.ParseMeta(obj.Meta).Digest; d != digestOf(obj.Body) {
		t.Errorf("digest %s, want %s", d, digestOf(obj.Body))
	}
	if ups := h.srv.Uploads(); len(ups) != 0 {
		t.Errorf("open uploads %v", ups)
	}
}

func TestUploadDuplicateName(t *testing.T) {
	const key = "darkraise/demo/100/dist.zip"
	for _, tc := range []struct {
		name            string
		existingAttempt string
		attempt         string
		overwrite       bool
		wantCode        int
	}{
		{"same attempt is refused", "1", "1", false, 1},
		{"same attempt with overwrite replaces", "1", "1", true, 0},
		{"earlier attempt is replaced", "1", "2", false, 0},
		{"no attempt recorded is replaced", "", "1", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.env["GITHUB_RUN_ATTEMPT"] = tc.attempt
			meta := map[string]string{"ghr-run-attempt": tc.existingAttempt}
			if tc.existingAttempt == "" {
				meta = nil
			}
			h.srv.Put(key, s3test.Object{Body: []byte("old"), Meta: meta})
			h.write(t, map[string]string{"dist/a.txt": "new"})
			args := []string{"upload", "--name", "dist", "--path", "dist"}
			if tc.overwrite {
				args = append(args, "--overwrite")
			}
			code, _, stderr := h.run(t, args...)
			if code != tc.wantCode {
				t.Fatalf("code %d, want %d; stderr %s", code, tc.wantCode, stderr)
			}
			obj, _ := h.srv.Object(key)
			if tc.wantCode != 0 {
				want := "ghr-artifact: an artifact named dist was already uploaded by this run attempt; set overwrite: true to replace it\n"
				if stderr != want {
					t.Errorf("stderr %q, want %q", stderr, want)
				}
				if string(obj.Body) != "old" {
					t.Errorf("the object was replaced")
				}
				if len(h.outputs(t)) != 0 {
					t.Errorf("outputs written: %v", h.outputs(t))
				}
				return
			}
			if got := zipEntries(t, obj.Body)["a.txt"]; got != "new" {
				t.Errorf("a.txt %q", got)
			}
			if a := artifact.ParseMeta(obj.Meta).Attempt; strconv.Itoa(a) != tc.attempt {
				t.Errorf("attempt %d, want %s", a, tc.attempt)
			}
		})
	}
}

func TestUploadRetentionDays(t *testing.T) {
	fixed := time.Date(2026, 10, 15, 9, 30, 15, 500, time.UTC)
	orig := now
	now = func() time.Time { return fixed }
	t.Cleanup(func() { now = orig })
	h := newHarness(t)
	h.write(t, map[string]string{"dist/a.txt": "a"})
	if code, _, stderr := h.run(t, "upload", "--name", "dist", "--path", "dist", "--retention-days", "5"); code != 0 {
		t.Fatalf("code %d stderr %s", code, stderr)
	}
	obj, _ := h.srv.Object("darkraise/demo/100/dist.zip")
	exp := artifact.ParseMeta(obj.Meta).Expires
	if want := time.Date(2026, 10, 20, 9, 30, 15, 0, time.UTC); exp == nil || !exp.Equal(want) {
		t.Fatalf("expires %v, want %v", exp, want)
	}
	for _, days := range []string{"-1", "91"} {
		code, _, stderr := h.run(t, "upload", "--name", "dist", "--path", "dist", "--retention-days", days)
		if code != 2 || !strings.Contains(stderr, "--retention-days must be between 1 and 90, or 0") {
			t.Errorf("%s days: code %d stderr %s", days, code, stderr)
		}
	}
}

func TestUploadNoFiles(t *testing.T) {
	for _, tc := range []struct {
		mode       string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{"warn", 0, "::warning::No files were found with the provided path: missing/%0Aalso-missing. No artifacts will be uploaded.\n", ""},
		{"error", 1, "", "ghr-artifact: No files were found with the provided path: missing/\nalso-missing. No artifacts will be uploaded.\n"},
		{"ignore", 0, "No files were found with the provided path: missing/\nalso-missing. No artifacts will be uploaded.\n", ""},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			h := newHarness(t)
			code, stdout, stderr := h.run(t, "upload", "--name", "dist", "--path", "missing/\nalso-missing", "--if-no-files-found", tc.mode)
			if code != tc.wantCode || stdout != tc.wantStdout || stderr != tc.wantStderr {
				t.Errorf("code %d stdout %q stderr %q", code, stdout, stderr)
			}
			if reqs := h.srv.Requests(); len(reqs) != 0 {
				t.Errorf("requests %v", reqs)
			}
			if len(h.outputs(t)) != 0 {
				t.Errorf("outputs written: %v", h.outputs(t))
			}
		})
	}
}

func TestStoreAndKeyErrors(t *testing.T) {
	other := artifact.Store{V: 1, Endpoint: "https://s3.example.sg", Bucket: "gha", Region: "us-east-1", KeyID: testKeyID}
	for _, tc := range []struct {
		name     string
		env      map[string]string
		args     []string
		wantCode int
		want     string
	}{
		{"empty key", map[string]string{"GHR_ARTIFACT_KEY": ""}, nil, 1,
			"ghr-artifact: pass key: ${{ secrets.GHR_ARTIFACT_KEY }} to the action\n"},
		{"key from another job key", map[string]string{"GHR_ARTIFACT_KEY": "OLDKEY:" + testSecret}, nil, 1,
			"ghr-artifact: GHR_ARTIFACT_KEY is out of date; turn the repository off and on in ghr\n"},
		{"malformed key", map[string]string{"GHR_ARTIFACT_KEY": testSecret}, nil, 1,
			"ghr-artifact: GHR_ARTIFACT_KEY: the key is not in the form <access key id>:<secret>\n"},
		{"empty store", map[string]string{"GHR_ARTIFACT_STORE": ""}, nil, 1,
			"ghr-artifact: GHR_ARTIFACT_STORE: the store is empty\n"},
		{"--key overrides the environment", map[string]string{"GHR_ARTIFACT_KEY": ""},
			[]string{"--key", artifact.FormatKey("OLDKEY", testSecret)}, 1,
			"ghr-artifact: GHR_ARTIFACT_KEY is out of date; turn the repository off and on in ghr\n"},
		{"--store overrides the environment", map[string]string{"GHR_ARTIFACT_STORE": ""},
			[]string{"--store", strings.Replace(other.JSON(), `"v":1`, `"v":9`, 1)}, 1,
			"ghr-artifact: GHR_ARTIFACT_STORE: store version 9 is not supported; use a newer ghr-artifact\n"},
		{"invalid name", nil, []string{"--name", "a/b"}, 1, `ghr-artifact: the artifact name is not valid: "a/b". Contains the following character: Forward slash /.`},
		{"no repository", map[string]string{"GITHUB_REPOSITORY": ""}, nil, 1,
			`ghr-artifact: GITHUB_REPOSITORY "" is not owner/repo; run ghr-artifact in a GitHub Actions job` + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.write(t, map[string]string{"dist/a.txt": "a"})
			for k, v := range tc.env {
				h.env[k] = v
			}
			args := append([]string{"upload", "--name", "dist", "--path", "dist"}, tc.args...)
			code, _, stderr := h.run(t, args...)
			if code != tc.wantCode || !strings.HasPrefix(stderr, tc.want) {
				t.Errorf("code %d stderr %q, want %d %q", code, stderr, tc.wantCode, tc.want)
			}
			if reqs := h.srv.Requests(); len(reqs) != 0 {
				t.Errorf("requests before the inputs were checked: %v", reqs)
			}
		})
	}
}

func TestUploadStoreRefusal(t *testing.T) {
	for _, tc := range []struct {
		op, code string
		status   int
		want     string
	}{
		{"head", "AccessDenied", 403, " refused the job key ("},
		{"put", "AccessDenied", 403, " refused the job key (AccessDenied"},
		{"put", "NoSuchBucket", 404, "ghr-artifact: bucket gha does not exist on https://127.0.0.1:"},
		{"put", "InternalError", 500, " answered InternalError"},
	} {
		h := newHarness(t)
		h.write(t, map[string]string{"dist/a.txt": "a"})
		h.srv.FailNext(tc.op, tc.status, tc.code)
		code, _, stderr := h.run(t, "upload", "--name", "dist", "--path", "dist")
		if code != 1 || !strings.Contains(stderr, tc.want) {
			t.Errorf("%s %s: code %d stderr %q", tc.op, tc.code, code, stderr)
		}
		if len(h.outputs(t)) != 0 {
			t.Errorf("%s %s: outputs written", tc.op, tc.code)
		}
	}
}

func TestUsage(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct {
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{nil, 2, "", "usage: ghr-artifact"},
		{[]string{"nope"}, 2, "", `ghr-artifact: unknown command "nope"`},
		{[]string{"version"}, 0, "dev\n", ""},
		{[]string{"help"}, 0, "usage: ghr-artifact", ""},
		{[]string{"upload", "--bogus"}, 2, "", "flag provided but not defined: -bogus"},
		{[]string{"upload", "--path", "x", "extra"}, 2, "", `ghr-artifact: unexpected argument "extra"`},
		{[]string{"upload", "--name", "x"}, 2, "", "ghr-artifact: --path is required"},
		{[]string{"upload", "--path", "x", "--if-no-files-found", "maybe"}, 2, "", "ghr-artifact: --if-no-files-found must be warn, error or ignore"},
		{[]string{"upload", "--path", "x", "--compression-level", "10"}, 2, "", "ghr-artifact: --compression-level must be between 0 and 9"},
	} {
		code, stdout, stderr := h.run(t, tc.args...)
		if code != tc.wantCode || !strings.HasPrefix(stdout, tc.wantStdout) || !strings.Contains(stderr, tc.wantStderr) {
			t.Errorf("%q: code %d stdout %q stderr %q", tc.args, code, stdout, stderr)
		}
	}
}
