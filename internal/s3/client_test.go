package s3_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/endpoint"
	"github.com/darkraise/ghr/internal/s3"
	"github.com/darkraise/ghr/internal/s3/s3test"
)

var testCred = s3.Credentials{AccessKeyID: "AKIDTEST", SecretAccessKey: "secret-test"}

// newClient returns a client for a fake that checks every signature.
func newClient(t *testing.T) (*s3.Client, *s3test.Server) {
	t.Helper()
	srv := s3test.NewServer(t, "artifacts")
	srv.CheckAuth(testCred, "us-east-1")
	c, err := s3.New(srv.URL(), "artifacts", "us-east-1", testCred)
	if err != nil {
		t.Fatal(err)
	}
	c.HTTP = srv.HTTPClient()
	return c, srv
}

func md5Hex(b []byte) string {
	sum := md5.Sum(b)
	return hex.EncodeToString(sum[:])
}

func pattern(n int64) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}

func TestNew(t *testing.T) {
	for _, tc := range []struct {
		endpoint, bucket, region string
		cred                     s3.Credentials
		want                     string // Endpoint, or a fragment of the error
		ok                       bool
	}{
		{"https://s3.example.sg", "artifacts", "us-east-1", testCred, "https://s3.example.sg", true},
		{"https://s3.example.sg:9000", "artifacts", "us-east-1", testCred, "https://s3.example.sg:9000", true},
		{"HTTP://192.168.0.105:9000", "artifacts", "us-east-1", testCred, "http://192.168.0.105:9000", true},
		{"https://s3.example.sg:9000/", "artifacts", "us-east-1", testCred, "s3: " + endpoint.MsgExtra, false},
		{"http://s3.example.sg", "artifacts", "us-east-1", testCred, "s3: " + endpoint.MsgHTTPName, false},
		{"s3.example.sg", "artifacts", "us-east-1", testCred, "s3: " + endpoint.MsgForm, false},
		{"https://s3.example.sg/artifacts", "artifacts", "us-east-1", testCred, "s3: " + endpoint.MsgExtra, false},
		{"https://s3.example.sg?x=1", "artifacts", "us-east-1", testCred, "s3: " + endpoint.MsgExtra, false},
		{"https://me:hunter2@s3.example.sg", "artifacts", "us-east-1", testCred, "s3: " + endpoint.MsgExtra, false},
		{"https://s3 example.sg", "artifacts", "us-east-1", testCred, "s3: " + endpoint.MsgForm, false},
		{"https://s3.example.sg", "", "us-east-1", testCred, "bucket name is empty", false},
		{"https://s3.example.sg", "artifacts", "", testCred, "region is empty", false},
		{"https://s3.example.sg", "artifacts", "us-east-1", s3.Credentials{AccessKeyID: "AKID"}, "access key ID or secret is empty", false},
	} {
		c, err := s3.New(tc.endpoint, tc.bucket, tc.region, tc.cred)
		if tc.ok {
			if err != nil || c.Endpoint != tc.want || c.Bucket != tc.bucket || c.Region != tc.region || c.Cred != tc.cred {
				t.Errorf("New(%q) = %+v, %v", tc.endpoint, c, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("New(%q, %q, %q) error %v, want %q", tc.endpoint, tc.bucket, tc.region, err, tc.want)
		}
		if err != nil && strings.Contains(err.Error(), "hunter2") {
			t.Errorf("error shows the endpoint password: %v", err)
		}
	}
}

func TestNewFollowsTheEndpointRule(t *testing.T) {
	b, err := os.ReadFile("../endpoint/testdata/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		Endpoint string `json:"endpoint"`
		Error    string `json:"error"`
	}
	if err := json.Unmarshal(b, &vectors); err != nil {
		t.Fatal(err)
	}
	for _, v := range vectors {
		c, err := s3.New(v.Endpoint, "artifacts", "us-east-1", testCred)
		if v.Error != "" {
			if err == nil || err.Error() != "s3: "+v.Error {
				t.Errorf("New(%q) error %v, want %q", v.Endpoint, err, "s3: "+v.Error)
			}
			continue
		}
		if err != nil {
			t.Errorf("New(%q): %v", v.Endpoint, err)
			continue
		}
		// An accepted endpoint has no path, so only the scheme's case may change.
		if !strings.EqualFold(c.Endpoint, v.Endpoint) || !(strings.HasPrefix(c.Endpoint, "https://") || strings.HasPrefix(c.Endpoint, "http://")) {
			t.Errorf("New(%q).Endpoint = %q", v.Endpoint, c.Endpoint)
		}
	}
}

func TestPlainHTTPEndpoint(t *testing.T) {
	ctx := context.Background()
	srv := s3test.NewPlainServer(t, "artifacts")
	srv.CheckAuth(testCred, "us-east-1")
	if !strings.HasPrefix(srv.URL(), "http://") {
		t.Fatalf("plain server URL %s", srv.URL())
	}
	c, err := s3.New(srv.URL(), "artifacts", "us-east-1", testCred)
	if err != nil {
		t.Fatal(err)
	}
	if c.Endpoint != srv.URL() {
		t.Fatalf("Endpoint %s, want %s", c.Endpoint, srv.URL())
	}
	var schemes []string
	next := srv.HTTPClient().Transport
	c.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		schemes = append(schemes, r.URL.Scheme)
		return next.RoundTrip(r)
	})}
	body := []byte("zip bytes")
	if err := c.Put(ctx, "o/r/1/dist.zip", bytes.NewReader(body), int64(len(body)), s3.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Head(ctx, "o/r/1/dist.zip"); err != nil {
		t.Fatal(err)
	}
	if len(schemes) != 2 || schemes[0] != "http" || schemes[1] != "http" {
		t.Fatalf("request schemes %q, want two http requests", schemes)
	}
	link, _, err := c.PresignGet("o/r/1/dist.zip", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(link, "http://") {
		t.Fatalf("presigned link %s does not start with http://", link)
	}
	resp, err := srv.HTTPClient().Get(link)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(got) != "zip bytes" {
		t.Fatalf("download %d %q", resp.StatusCode, got)
	}
}

func TestErrorsAndNotFound(t *testing.T) {
	ctx := context.Background()
	c, srv := newClient(t)
	srv.Put("o/r/1/dist.zip", s3test.Object{Body: []byte("x")})

	_, err := c.Head(ctx, "o/r/1/missing.zip")
	var e *s3.Error
	if !errors.As(err, &e) || e.Status != http.StatusNotFound || !s3.IsNotFound(err) || err.Error() != "s3: 404 NoSuchKey: The specified key does not exist." {
		t.Fatalf("head missing: %v", err)
	}
	_, _, err = c.Get(ctx, "o/r/1/missing.zip")
	if !errors.As(err, &e) || e.Code != "NoSuchKey" || !s3.IsNotFound(err) {
		t.Fatalf("get missing: %v", err)
	}
	srv.FailNext("get", http.StatusForbidden, "AccessDenied")
	_, _, err = c.Get(ctx, "o/r/1/dist.zip")
	if !errors.As(err, &e) || e.Status != http.StatusForbidden || e.Code != "AccessDenied" || s3.IsNotFound(err) ||
		err.Error() != "s3: 403 AccessDenied: AccessDenied injected by s3test" {
		t.Fatalf("get denied: %v", err)
	}

	for _, tc := range []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("s3: 404 Not Found"), false},
		{&s3.Error{Status: http.StatusNotFound}, true},
		{fmt.Errorf("head: %w", &s3.Error{Status: http.StatusOK, Code: "NoSuchBucket"}), true},
		{&s3.Error{Status: http.StatusBadRequest, Code: "NotFound"}, true},
		{&s3.Error{Status: http.StatusInternalServerError, Code: "InternalError"}, false},
	} {
		if got := s3.IsNotFound(tc.err); got != tc.want {
			t.Errorf("IsNotFound(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestListReadsEveryPage(t *testing.T) {
	c, srv := newClient(t)
	modified := time.Date(2026, 10, 15, 8, 30, 0, 0, time.UTC)
	keys := []string{"o/r/1/a.zip", "o/r/1/b.zip", "o/r/2/c.zip", "o/r/2/d.zip", "o/r/3/e.zip"}
	for _, k := range append([]string{"other/r/1/x.zip"}, keys...) {
		srv.Put(k, s3test.Object{Body: []byte(k), LastModified: modified})
	}
	objs, err := c.List(context.Background(), "o/r/")
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != len(keys) {
		t.Fatalf("objects %+v", objs)
	}
	for i, o := range objs {
		want := s3.Object{Key: keys[i], Size: int64(len(keys[i])), ETag: md5Hex([]byte(keys[i])), LastModified: modified}
		if o != want {
			t.Fatalf("object %d = %+v, want %+v", i, o, want)
		}
	}
	reqs := srv.Requests()
	if len(reqs) != 3 || reqs[0] != "GET /artifacts?list-type=2&prefix=o%2Fr%2F" ||
		!strings.HasPrefix(reqs[1], "GET /artifacts?continuation-token=") || !strings.HasSuffix(reqs[2], "&list-type=2&prefix=o%2Fr%2F") {
		t.Fatalf("requests %q", reqs)
	}
}

func TestPutHeadGetRoundTrip(t *testing.T) {
	ctx := context.Background()
	c, srv := newClient(t)
	key := "o/r/1/my file+ü.zip"
	opt := s3.PutOptions{
		ContentType:        "application/zip",
		ContentDisposition: `attachment; filename="my file+ü.zip"`,
		Meta:               map[string]string{"ghr-run-id": "42", "GHR-Workflow": "Build ✓"},
	}
	if err := c.Put(ctx, key, strings.NewReader("zip bytes"), 9, opt); err != nil {
		t.Fatal(err)
	}
	stored, ok := srv.Object(key)
	if !ok || string(stored.Body) != "zip bytes" || stored.Meta["ghr-workflow"] == "Build ✓" {
		t.Fatalf("stored %+v ok %v; non-ASCII metadata must be encoded on the wire", stored, ok)
	}
	want := s3.Head{
		Size:               9,
		ETag:               md5Hex([]byte("zip bytes")),
		LastModified:       stored.LastModified,
		Meta:               map[string]string{"ghr-run-id": "42", "ghr-workflow": "Build ✓"},
		ContentType:        "application/zip",
		ContentDisposition: `attachment; filename="my file+ü.zip"`,
	}
	check := func(name string, h s3.Head) {
		t.Helper()
		if h.Size != want.Size || h.ETag != want.ETag || !h.LastModified.Equal(want.LastModified) || h.ContentType != want.ContentType ||
			h.ContentDisposition != want.ContentDisposition || len(h.Meta) != 2 || h.Meta["ghr-run-id"] != "42" || h.Meta["ghr-workflow"] != "Build ✓" {
			t.Fatalf("%s = %+v, want %+v", name, h, want)
		}
	}
	h, err := c.Head(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	check("Head", h)
	body, h, err := c.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(body)
	body.Close()
	if err != nil || string(b) != "zip bytes" {
		t.Fatalf("body %q err %v", b, err)
	}
	check("Get", h)
	const path = "/artifacts/o/r/1/my%20file%2B%C3%BC.zip"
	if got := strings.Join(srv.Requests(), "|"); got != "PUT "+path+"|GET "+path+"|GET "+path {
		t.Fatalf("requests %q", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type sentRequest struct{ method, rangeHeader, signedHeaders string }

// recordRequests makes c note the method, Range header and signed header list
// of every request it sends.
func recordRequests(c *s3.Client) *[]sentRequest {
	var sent []sentRequest
	next := c.HTTP.Transport
	c.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		_, signed, _ := strings.Cut(r.Header.Get("Authorization"), "SignedHeaders=")
		signed, _, _ = strings.Cut(signed, ",")
		sent = append(sent, sentRequest{r.Method, r.Header.Get("Range"), signed})
		return next.RoundTrip(r)
	})}
	return &sent
}

func TestHeadIsAPlainSignedGet(t *testing.T) {
	c, srv := newClient(t)
	sent := recordRequests(c)
	srv.Put("o/r/1/dist.zip", s3test.Object{Body: pattern(1000)})
	if _, err := c.Head(context.Background(), "o/r/1/dist.zip"); err != nil {
		t.Fatal(err)
	}
	if len(*sent) != 1 || (*sent)[0].method != http.MethodGet || (*sent)[0].rangeHeader != "" ||
		(*sent)[0].signedHeaders != "host;x-amz-content-sha256;x-amz-date" {
		t.Fatalf("sent %+v, want one GET without Range signed over host, x-amz-content-sha256 and x-amz-date", *sent)
	}
	for _, r := range srv.Requests() {
		if strings.HasPrefix(r, "HEAD ") {
			t.Fatalf("requests %q include a HEAD", srv.Requests())
		}
	}
}

func TestHeadAsksForAnUncompressedAnswer(t *testing.T) {
	body := pattern(1000)
	// Compresses whenever the client accepts gzip, as a compressing proxy does.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("Content-Encoding", "gzip")
			zw := gzip.NewWriter(w)
			zw.Write(body)
			zw.Close()
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	c, err := s3.New(srv.URL, "artifacts", "us-east-1", testCred)
	if err != nil {
		t.Fatal(err)
	}
	c.HTTP = srv.Client()
	h, err := c.Head(context.Background(), "o/r/1/dist.zip")
	if err != nil {
		t.Fatal(err)
	}
	if h.Size != int64(len(body)) {
		t.Fatalf("size %d, want %d", h.Size, len(body))
	}
}

func TestHeadOfAMultiByteObject(t *testing.T) {
	c, srv := newClient(t)
	body := pattern(70000)
	modified := time.Date(2026, 10, 15, 8, 30, 0, 0, time.UTC)
	srv.Put("o/r/1/dist.zip", s3test.Object{
		Body:               body,
		Meta:               map[string]string{"ghr-run-id": "7"},
		ContentType:        "application/zip",
		ContentDisposition: `attachment; filename="dist.zip"`,
		LastModified:       modified,
	})
	h, err := c.Head(context.Background(), "o/r/1/dist.zip")
	if err != nil {
		t.Fatal(err)
	}
	if h.Size != 70000 || h.ETag != md5Hex(body) || !h.LastModified.Equal(modified) || h.ContentType != "application/zip" ||
		h.ContentDisposition != `attachment; filename="dist.zip"` || len(h.Meta) != 1 || h.Meta["ghr-run-id"] != "7" {
		t.Fatalf("head %+v", h)
	}
}

func TestHeadOfAnEmptyObject(t *testing.T) {
	c, srv := newClient(t)
	sent := recordRequests(c)
	srv.Put("o/r/1/empty.zip", s3test.Object{ContentType: "application/zip", Meta: map[string]string{"ghr-run-id": "7"}})
	h, err := c.Head(context.Background(), "o/r/1/empty.zip")
	if err != nil {
		t.Fatal(err)
	}
	if h.Size != 0 || h.ETag != md5Hex(nil) || h.ContentType != "application/zip" || h.Meta["ghr-run-id"] != "7" {
		t.Fatalf("head %+v", h)
	}
	if len(*sent) != 1 || (*sent)[0].method != http.MethodGet {
		t.Fatalf("sent %+v, want one GET", *sent)
	}
}

func TestHeadOfAMissingKey(t *testing.T) {
	c, _ := newClient(t)
	_, err := c.Head(context.Background(), "o/r/1/missing.zip")
	var e *s3.Error
	if !errors.As(err, &e) || e.Status != http.StatusNotFound || e.Code != "NoSuchKey" || !s3.IsNotFound(err) {
		t.Fatalf("head missing: %v", err)
	}
}

// stubServer starts a TLS server answering every request with handler and
// returns a client for it.
func stubServer(t *testing.T, handler http.HandlerFunc) *s3.Client {
	t.Helper()
	ts := httptest.NewTLSServer(handler)
	t.Cleanup(ts.Close)
	c, err := s3.New(ts.URL, "artifacts", "us-east-1", testCred)
	if err != nil {
		t.Fatal(err)
	}
	c.HTTP = ts.Client()
	return c
}

type countingBody struct {
	io.ReadCloser
	read *int
}

func (b countingBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	*b.read += n
	return n, err
}

func TestHeadDoesNotReadTheBody(t *testing.T) {
	body := pattern(4096)
	c := stubServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"abc"`)
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Write(body)
	})
	read := 0
	next := c.HTTP.Transport
	c.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		resp, err := next.RoundTrip(r)
		if resp != nil {
			resp.Body = countingBody{resp.Body, &read}
		}
		return resp, err
	})}
	h, err := c.Head(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	if h.Size != 4096 || h.ETag != "abc" || read != 0 {
		t.Fatalf("head %+v after reading %d body bytes, want size 4096 and none read", h, read)
	}
}

func TestUploadBelowPartSizeIsOnePut(t *testing.T) {
	for _, size := range []int64{0, 1000, s3.PartSize - 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			c, srv := newClient(t)
			data := pattern(size)
			if err := c.Upload(context.Background(), "o/r/1/dist.zip", bytes.NewReader(data), size, s3.PutOptions{ContentType: "application/zip"}); err != nil {
				t.Fatal(err)
			}
			o, ok := srv.Object("o/r/1/dist.zip")
			if !ok || !bytes.Equal(o.Body, data) || o.ContentType != "application/zip" {
				t.Fatalf("stored ok %v size %d type %q", ok, len(o.Body), o.ContentType)
			}
			if got := strings.Join(srv.Requests(), "|"); got != "PUT /artifacts/o/r/1/dist.zip" {
				t.Fatalf("requests %q", got)
			}
		})
	}
}

// partGate holds every part request until want of them are in flight, so a
// sequential upload shows up as a maximum of one.
type partGate struct {
	base     http.RoundTripper
	want     int
	mu       sync.Mutex
	inFlight int
	max      int
	all      chan struct{}
}

func (g *partGate) RoundTrip(r *http.Request) (*http.Response, error) {
	if !r.URL.Query().Has("partNumber") {
		return g.base.RoundTrip(r)
	}
	g.mu.Lock()
	g.inFlight++
	g.max = max(g.max, g.inFlight)
	if g.inFlight == g.want {
		close(g.all)
	}
	g.mu.Unlock()
	select {
	case <-g.all:
	case <-time.After(5 * time.Second):
	}
	defer func() {
		g.mu.Lock()
		g.inFlight--
		g.mu.Unlock()
	}()
	return g.base.RoundTrip(r)
}

func TestUploadRefusesMoreThanTenThousandParts(t *testing.T) {
	c, srv := newClient(t)
	size := int64(10000*s3.PartSize + 1)
	err := c.Upload(context.Background(), "o/r/1/dist.zip", bytes.NewReader(nil), size, s3.PutOptions{})
	if err == nil || !strings.Contains(err.Error(), "larger than the 10000 parts of 16 MiB an upload can hold") {
		t.Fatalf("err %v", err)
	}
	if got := srv.Requests(); len(got) != 0 {
		t.Fatalf("requests %q before refusing", got)
	}
}

func TestUploadMultipart(t *testing.T) {
	c, srv := newClient(t)
	gate := &partGate{base: srv.HTTPClient().Transport, want: 3, all: make(chan struct{})}
	c.HTTP = &http.Client{Transport: gate}
	size := int64(2*s3.PartSize + 1)
	data := pattern(size)
	opt := s3.PutOptions{ContentType: "application/zip", ContentDisposition: `attachment; filename="big.zip"`, Meta: map[string]string{"ghr-files": "3"}}
	if err := c.Upload(context.Background(), "o/r/1/big.zip", bytes.NewReader(data), size, opt); err != nil {
		t.Fatal(err)
	}
	o, ok := srv.Object("o/r/1/big.zip")
	if !ok || !bytes.Equal(o.Body, data) || o.ContentType != "application/zip" || o.ContentDisposition != `attachment; filename="big.zip"` || o.Meta["ghr-files"] != "3" {
		t.Fatalf("stored ok %v size %d type %q disposition %q meta %v", ok, len(o.Body), o.ContentType, o.ContentDisposition, o.Meta)
	}
	if len(srv.Uploads()) != 0 {
		t.Fatalf("open uploads %v", srv.Uploads())
	}
	if gate.max != 3 {
		t.Fatalf("%d parts were in flight at once, want 3", gate.max)
	}
	reqs := srv.Requests()
	if len(reqs) != 5 || reqs[0] != "POST /artifacts/o/r/1/big.zip?uploads=" || !strings.HasPrefix(reqs[4], "POST /artifacts/o/r/1/big.zip?uploadId=") {
		t.Fatalf("requests %q", reqs)
	}
	for _, r := range reqs[1:4] {
		if !strings.HasPrefix(r, "PUT /artifacts/o/r/1/big.zip?partNumber=") {
			t.Fatalf("requests %q", reqs)
		}
	}
}

func TestUploadAbortsOnFailure(t *testing.T) {
	for _, tc := range []struct {
		op     string
		status int
	}{
		{"upload-part", http.StatusInternalServerError},
		{"complete", http.StatusOK},
		{"complete", http.StatusBadRequest},
	} {
		t.Run(fmt.Sprintf("%s %d", tc.op, tc.status), func(t *testing.T) {
			c, srv := newClient(t)
			srv.FailNext(tc.op, tc.status, "InternalError")
			size := int64(2*s3.PartSize + 1)
			err := c.Upload(context.Background(), "o/r/1/big.zip", bytes.NewReader(pattern(size)), size, s3.PutOptions{})
			var e *s3.Error
			if !errors.As(err, &e) || e.Code != "InternalError" {
				t.Fatalf("Upload error %v", err)
			}
			if len(srv.Uploads()) != 0 {
				t.Fatalf("open uploads after a failure %v", srv.Uploads())
			}
			if _, ok := srv.Object("o/r/1/big.zip"); ok {
				t.Fatal("a failed upload left an object")
			}
			reqs := srv.Requests()
			if last := reqs[len(reqs)-1]; !strings.HasPrefix(last, "DELETE /artifacts/o/r/1/big.zip?uploadId=") {
				t.Fatalf("last request %q, want the abort", last)
			}
		})
	}
}

func TestDelete(t *testing.T) {
	many := make([]string, 1001)
	for i := range many {
		many[i] = fmt.Sprintf("o/r/1/%04d.zip", i)
	}
	for _, tc := range []struct {
		name     string
		noBatch  bool
		keys     []string
		requests []string
	}{
		{"batch", false, []string{"o/r/1/a.zip", "o/r/1/b c.zip", "o/r/1/missing.zip"}, []string{"POST /artifacts?delete="}},
		{"two batches", false, many, []string{"POST /artifacts?delete=", "POST /artifacts?delete="}},
		{"one by one when batches are not implemented", true, []string{"o/r/1/a.zip", "o/r/1/b c.zip", "o/r/1/missing.zip"}, []string{
			"POST /artifacts?delete=",
			"DELETE /artifacts/o/r/1/a.zip",
			"DELETE /artifacts/o/r/1/b%20c.zip",
			"DELETE /artifacts/o/r/1/missing.zip",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, srv := newClient(t)
			for _, k := range tc.keys {
				if !strings.Contains(k, "missing") {
					srv.Put(k, s3test.Object{Body: []byte(k)})
				}
			}
			srv.Put("o/r/2/keep.zip", s3test.Object{Body: []byte("keep")})
			if tc.noBatch {
				srv.DisableBatchDelete()
			}
			deleted, err := c.Delete(context.Background(), tc.keys)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(deleted, "|") != strings.Join(tc.keys, "|") {
				t.Fatalf("deleted %d keys %q, want %d", len(deleted), deleted[:min(len(deleted), 5)], len(tc.keys))
			}
			if got := strings.Join(srv.Keys(), ","); got != "o/r/2/keep.zip" {
				t.Fatalf("keys left %q", got)
			}
			if got := strings.Join(srv.Requests(), "|"); got != strings.Join(tc.requests, "|") {
				t.Fatalf("requests %q", srv.Requests())
			}
		})
	}
}

func TestDeleteStopsAtFailedRequest(t *testing.T) {
	c, srv := newClient(t)
	srv.Put("a", s3test.Object{Body: []byte("a")})
	srv.FailNext("delete-objects", http.StatusForbidden, "AccessDenied")
	deleted, err := c.Delete(context.Background(), []string{"a"})
	var e *s3.Error
	if len(deleted) != 0 || !errors.As(err, &e) || e.Code != "AccessDenied" {
		t.Fatalf("deleted %q err %v", deleted, err)
	}
	if got := strings.Join(srv.Keys(), ","); got != "a" {
		t.Fatalf("keys %q", got)
	}
}

func TestDeleteOneByOneFailsOnAMissingBucket(t *testing.T) {
	c, srv := newClient(t)
	srv.DisableBatchDelete()
	srv.FailNext("delete", http.StatusNotFound, "NoSuchBucket")
	deleted, err := c.Delete(context.Background(), []string{"a"})
	var e *s3.Error
	if len(deleted) != 0 || !errors.As(err, &e) || e.Code != "NoSuchBucket" {
		t.Fatalf("deleted %q err %v, want nothing deleted and NoSuchBucket", deleted, err)
	}
}

func TestListUploadsAndAbort(t *testing.T) {
	ctx := context.Background()
	c, srv := newClient(t)
	initiated := time.Date(2026, 10, 14, 1, 2, 3, 0, time.UTC)
	a := srv.SeedUpload("o/r/1/a.zip", initiated)
	srv.SeedUpload("o/r/1/b.zip", initiated)
	srv.SeedUpload("o/r/2/c.zip", initiated.Add(time.Hour))
	ups, err := c.ListUploads(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ups) != 3 || ups[0] != (s3.Upload{Key: "o/r/1/a.zip", UploadID: a, Initiated: initiated}) ||
		ups[2].Key != "o/r/2/c.zip" || !ups[2].Initiated.Equal(initiated.Add(time.Hour)) {
		t.Fatalf("uploads %+v", ups)
	}
	if n := len(srv.Requests()); n != 2 {
		t.Fatalf("%d list requests, want 2 pages", n)
	}
	if err := c.Abort(ctx, "o/r/1/a.zip", a); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(srv.Uploads(), ","); got != "o/r/1/b.zip,o/r/2/c.zip" {
		t.Fatalf("uploads after abort %q", got)
	}
	err = c.Abort(ctx, "o/r/1/a.zip", a)
	var e *s3.Error
	if !errors.As(err, &e) || e.Code != "NoSuchUpload" {
		t.Fatalf("second abort: %v", err)
	}
}

func TestPresignGet(t *testing.T) {
	c, srv := newClient(t)
	now := time.Now().UTC().Truncate(time.Second)
	c.Now = func() time.Time { return now }
	key := "o/r/1/my dist.zip"
	srv.Put(key, s3test.Object{Body: []byte("zip bytes"), ContentDisposition: `attachment; filename="my dist.zip"`})

	link, expires, err := c.PresignGet(key, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !expires.Equal(now.Add(time.Hour)) || !strings.HasPrefix(link, srv.URL()+"/artifacts/o/r/1/my%20dist.zip?") || !strings.Contains(link, "X-Amz-Expires=3600") {
		t.Fatalf("link %s expires %s", link, expires)
	}
	resp, err := srv.HTTPClient().Get(link)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(b) != "zip bytes" || resp.Header.Get("Content-Disposition") != `attachment; filename="my dist.zip"` {
		t.Fatalf("download %d %q %v", resp.StatusCode, b, resp.Header)
	}
	if _, _, err := c.PresignGet(key, s3.MaxPresign+time.Second); err == nil {
		t.Fatal("a link longer than 7 days was signed")
	}
}

func TestExpiredContextSendsNothing(t *testing.T) {
	big := int64(2*s3.PartSize + 1)
	for _, tc := range []struct {
		name string
		call func(context.Context, *s3.Client) error
	}{
		{"List", func(ctx context.Context, c *s3.Client) error { _, err := c.List(ctx, ""); return err }},
		{"Head", func(ctx context.Context, c *s3.Client) error { _, err := c.Head(ctx, "k"); return err }},
		{"Get", func(ctx context.Context, c *s3.Client) error { _, _, err := c.Get(ctx, "k"); return err }},
		{"Put", func(ctx context.Context, c *s3.Client) error {
			return c.Put(ctx, "k", strings.NewReader("v"), 1, s3.PutOptions{})
		}},
		{"Upload", func(ctx context.Context, c *s3.Client) error {
			return c.Upload(ctx, "k", bytes.NewReader(pattern(big)), big, s3.PutOptions{})
		}},
		{"Delete", func(ctx context.Context, c *s3.Client) error { _, err := c.Delete(ctx, []string{"k"}); return err }},
		{"ListUploads", func(ctx context.Context, c *s3.Client) error { _, err := c.ListUploads(ctx); return err }},
		{"Abort", func(ctx context.Context, c *s3.Client) error { return c.Abort(ctx, "k", "id") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, srv := newClient(t)
			srv.Put("k", s3test.Object{Body: []byte("v")})
			ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			defer cancel()
			if err := tc.call(ctx, c); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("error %v, want the context deadline", err)
			}
			if reqs := srv.Requests(); len(reqs) != 0 {
				t.Fatalf("requests %q", reqs)
			}
		})
	}
}
