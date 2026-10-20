package s3test

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/s3"
)

var testCred = s3.Credentials{AccessKeyID: "AKIDTEST", SecretAccessKey: "secret-test"}

type reply struct {
	status int
	header http.Header
	body   string
}

func (r reply) code() string {
	var e struct{ Code string }
	xml.Unmarshal([]byte(r.body), &e)
	return e.Code
}

// send makes one request signed with testCred, as the s3 client would.
func send(t *testing.T, s *Server, method, path string, header http.Header, body []byte) reply {
	t.Helper()
	req, err := http.NewRequest(method, s.URL()+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body == nil {
		req.Body = http.NoBody
		req.ContentLength = 0
	}
	for k, v := range header {
		req.Header[k] = v
	}
	s3.SignRequest(req, testCred, "us-east-1", s3.HashHex(body), time.Now())
	return do(t, s, req)
}

func do(t *testing.T, s *Server, req *http.Request) reply {
	t.Helper()
	resp, err := s.HTTPClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return reply{status: resp.StatusCode, header: resp.Header, body: string(b)}
}

func TestListPages(t *testing.T) {
	s := NewServer(t, "artifacts")
	for _, k := range []string{"o/r/1/a.zip", "o/r/1/b.zip", "o/r/2/c.zip", "o/r/2/d.zip", "o/r/3/e.zip", "other/x.zip"} {
		s.Put(k, Object{Body: []byte(k)})
	}
	var keys []string
	token := ""
	for page := 0; ; page++ {
		if page > 5 {
			t.Fatal("listing never ended")
		}
		q := url.Values{"list-type": {"2"}, "prefix": {"o/r/"}}
		if token != "" {
			q.Set("continuation-token", token)
		}
		r := send(t, s, http.MethodGet, "/artifacts?"+q.Encode(), nil, nil)
		if r.status != http.StatusOK {
			t.Fatalf("page %d: %d %s", page, r.status, r.body)
		}
		var res struct {
			IsTruncated           bool
			NextContinuationToken string
			Contents              []struct {
				Key  string
				Size int64
				ETag string
			}
		}
		if err := xml.Unmarshal([]byte(r.body), &res); err != nil {
			t.Fatal(err)
		}
		if len(res.Contents) > 2 {
			t.Fatalf("page %d holds %d entries", page, len(res.Contents))
		}
		for _, c := range res.Contents {
			if c.Size != int64(len(c.Key)) || c.ETag != etag([]byte(c.Key)) {
				t.Fatalf("entry %+v", c)
			}
			keys = append(keys, c.Key)
		}
		if !res.IsTruncated {
			break
		}
		token = res.NextContinuationToken
	}
	if got := strings.Join(keys, ","); got != "o/r/1/a.zip,o/r/1/b.zip,o/r/2/c.zip,o/r/2/d.zip,o/r/3/e.zip" {
		t.Fatalf("keys %s", got)
	}
	if n := len(s.Requests()); n != 3 {
		t.Fatalf("%d requests, want 3 pages", n)
	}
}

func TestGetAndHeadServeObject(t *testing.T) {
	s := NewServer(t, "artifacts")
	modified := time.Date(2026, 10, 15, 8, 30, 0, 0, time.UTC)
	s.Put("o/r/1/dist.zip", Object{
		Body:               []byte("zip bytes"),
		Meta:               map[string]string{"ghr-run-id": "1"},
		ContentType:        "application/zip",
		ContentDisposition: `attachment; filename="dist.zip"`,
		LastModified:       modified,
	})
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		r := send(t, s, method, "/artifacts/o/r/1/dist.zip", nil, nil)
		if r.status != http.StatusOK {
			t.Fatalf("%s: %d", method, r.status)
		}
		for k, want := range map[string]string{
			"Content-Type":          "application/zip",
			"Content-Disposition":   `attachment; filename="dist.zip"`,
			"Content-Length":        "9",
			"ETag":                  etag([]byte("zip bytes")),
			"Last-Modified":         "Thu, 15 Oct 2026 08:30:00 GMT",
			"X-Amz-Meta-Ghr-Run-Id": "1",
		} {
			if got := r.header.Get(k); got != want {
				t.Errorf("%s %s = %q, want %q", method, k, got, want)
			}
		}
		wantBody := "zip bytes"
		if method == http.MethodHead {
			wantBody = ""
		}
		if r.body != wantBody {
			t.Errorf("%s body %q", method, r.body)
		}
	}
	if r := send(t, s, http.MethodGet, "/artifacts/o/r/1/missing.zip", nil, nil); r.status != http.StatusNotFound || r.code() != "NoSuchKey" {
		t.Fatalf("missing key: %d %s", r.status, r.body)
	}
	if r := send(t, s, http.MethodGet, "/elsewhere/o/r/1/dist.zip", nil, nil); r.status != http.StatusNotFound || r.code() != "NoSuchBucket" {
		t.Fatalf("other bucket: %d %s", r.status, r.body)
	}
}

func TestPutStoresObject(t *testing.T) {
	s := NewServer(t, "artifacts")
	h := http.Header{}
	h.Set("Content-Type", "application/zip")
	h.Set("Content-Disposition", `attachment; filename="my file.zip"`)
	h.Set("X-Amz-Meta-Ghr-Sha", "abc123")
	r := send(t, s, http.MethodPut, "/artifacts/o/r/1/my%20file.zip", h, []byte("data"))
	if r.status != http.StatusOK || r.header.Get("ETag") != etag([]byte("data")) {
		t.Fatalf("put: %d %v %s", r.status, r.header, r.body)
	}
	o, ok := s.Object("o/r/1/my file.zip")
	if !ok || string(o.Body) != "data" || o.ContentType != "application/zip" ||
		o.ContentDisposition != `attachment; filename="my file.zip"` || o.Meta["ghr-sha"] != "abc123" || o.LastModified.IsZero() {
		t.Fatalf("object %+v ok %v", o, ok)
	}
	if got := strings.Join(s.Requests(), "|"); got != "PUT /artifacts/o/r/1/my%20file.zip" {
		t.Fatalf("requests %q", got)
	}

	req, err := http.NewRequest(http.MethodPut, s.URL()+"/artifacts/o/r/1/chunked.zip", io.MultiReader(strings.NewReader("x")))
	if err != nil {
		t.Fatal(err)
	}
	if r := do(t, s, req); r.status != http.StatusLengthRequired || r.code() != "MissingContentLength" {
		t.Fatalf("chunked put: %d %s", r.status, r.body)
	}
}

func TestMultipartUpload(t *testing.T) {
	s := NewServer(t, "artifacts")
	create := func(key string) string {
		t.Helper()
		h := http.Header{}
		h.Set("Content-Type", "application/zip")
		h.Set("X-Amz-Meta-Ghr-Files", "3")
		r := send(t, s, http.MethodPost, "/artifacts/"+key+"?uploads=", h, nil)
		var res struct{ UploadId string }
		if err := xml.Unmarshal([]byte(r.body), &res); r.status != http.StatusOK || err != nil || res.UploadId == "" {
			t.Fatalf("create: %d %s", r.status, r.body)
		}
		return res.UploadId
	}
	part := func(key, id string, n int, data []byte) string {
		t.Helper()
		r := send(t, s, http.MethodPut, fmt.Sprintf("/artifacts/%s?partNumber=%d&uploadId=%s", key, n, id), nil, data)
		if r.status != http.StatusOK || r.header.Get("ETag") == "" {
			t.Fatalf("part %d: %d %s", n, r.status, r.body)
		}
		return r.header.Get("ETag")
	}
	complete := func(key, id string, etags ...string) reply {
		t.Helper()
		var b strings.Builder
		b.WriteString("<CompleteMultipartUpload>")
		for i, e := range etags {
			fmt.Fprintf(&b, "<Part><PartNumber>%d</PartNumber><ETag>%s</ETag></Part>", i+1, e)
		}
		b.WriteString("</CompleteMultipartUpload>")
		return send(t, s, http.MethodPost, "/artifacts/"+key+"?uploadId="+id, nil, []byte(b.String()))
	}

	first := bytes.Repeat([]byte("a"), minPartSize)
	id := create("o/r/1/big.zip")
	if got := strings.Join(s.Uploads(), ","); got != "o/r/1/big.zip" {
		t.Fatalf("uploads %q", got)
	}
	e1 := part("o/r/1/big.zip", id, 1, first)
	e2 := part("o/r/1/big.zip", id, 2, []byte("tail"))
	if r := complete("o/r/1/big.zip", id, e1, e2); r.status != http.StatusOK || !strings.Contains(r.body, "CompleteMultipartUploadResult") {
		t.Fatalf("complete: %d %s", r.status, r.body)
	}
	o, ok := s.Object("o/r/1/big.zip")
	if !ok || !bytes.Equal(o.Body, append(append([]byte(nil), first...), "tail"...)) || o.ContentType != "application/zip" || o.Meta["ghr-files"] != "3" {
		t.Fatalf("assembled object: ok %v size %d type %q meta %v", ok, len(o.Body), o.ContentType, o.Meta)
	}
	if len(s.Uploads()) != 0 {
		t.Fatalf("uploads after complete %v", s.Uploads())
	}

	small := create("o/r/1/small.zip")
	s1 := part("o/r/1/small.zip", small, 1, []byte("too small"))
	s2 := part("o/r/1/small.zip", small, 2, []byte("tail"))
	if r := complete("o/r/1/small.zip", small, s1, s2); r.status != http.StatusBadRequest || r.code() != "EntityTooSmall" {
		t.Fatalf("small part: %d %s", r.status, r.body)
	}
	if r := complete("o/r/1/small.zip", small, `"wrong"`, s2); r.status != http.StatusBadRequest || r.code() != "InvalidPart" {
		t.Fatalf("wrong etag: %d %s", r.status, r.body)
	}
	if r := send(t, s, http.MethodDelete, "/artifacts/o/r/1/small.zip?uploadId="+small, nil, nil); r.status != http.StatusNoContent {
		t.Fatalf("abort: %d %s", r.status, r.body)
	}
	if len(s.Uploads()) != 0 {
		t.Fatalf("uploads after abort %v", s.Uploads())
	}
	if r := send(t, s, http.MethodDelete, "/artifacts/o/r/1/small.zip?uploadId="+small, nil, nil); r.status != http.StatusNotFound || r.code() != "NoSuchUpload" {
		t.Fatalf("second abort: %d %s", r.status, r.body)
	}
}

func deleteBody(keys ...string) ([]byte, http.Header) {
	var b strings.Builder
	b.WriteString("<Delete>")
	for _, k := range keys {
		fmt.Fprintf(&b, "<Object><Key>%s</Key></Object>", k)
	}
	b.WriteString("</Delete>")
	sum := md5.Sum([]byte(b.String()))
	h := http.Header{}
	h.Set("Content-MD5", base64.StdEncoding.EncodeToString(sum[:]))
	return []byte(b.String()), h
}

func TestDeletes(t *testing.T) {
	s := NewServer(t, "artifacts")
	for _, k := range []string{"a", "b", "c", "d"} {
		s.Put(k, Object{Body: []byte(k)})
	}
	body, h := deleteBody("a", "b", "missing")
	r := send(t, s, http.MethodPost, "/artifacts?delete=", h, body)
	if r.status != http.StatusOK || strings.Count(r.body, "<Deleted>") != 3 {
		t.Fatalf("batch delete: %d %s", r.status, r.body)
	}
	if r := send(t, s, http.MethodPost, "/artifacts?delete=", nil, body); r.status != http.StatusBadRequest || r.code() != "InvalidDigest" {
		t.Fatalf("no Content-MD5: %d %s", r.status, r.body)
	}
	s.DisableBatchDelete()
	if r := send(t, s, http.MethodPost, "/artifacts?delete=", h, body); r.status != http.StatusNotImplemented || r.code() != "NotImplemented" {
		t.Fatalf("batch disabled: %d %s", r.status, r.body)
	}
	if r := send(t, s, http.MethodDelete, "/artifacts/c", nil, nil); r.status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", r.status, r.body)
	}
	if r := send(t, s, http.MethodDelete, "/artifacts/missing", nil, nil); r.status != http.StatusNoContent {
		t.Fatalf("delete missing: %d %s", r.status, r.body)
	}
	if got := strings.Join(s.Keys(), ","); got != "d" {
		t.Fatalf("keys %q", got)
	}
}

func TestListUploadsPages(t *testing.T) {
	s := NewServer(t, "artifacts")
	initiated := time.Date(2026, 10, 14, 1, 2, 3, 0, time.UTC)
	ids := map[string]bool{
		s.SeedUpload("o/r/1/a.zip", initiated): true,
		s.SeedUpload("o/r/1/a.zip", initiated): true,
		s.SeedUpload("o/r/2/b.zip", initiated): true,
	}
	seen := map[string]bool{}
	q := url.Values{"uploads": {""}}
	for page := 0; ; page++ {
		if page > 3 {
			t.Fatal("listing never ended")
		}
		r := send(t, s, http.MethodGet, "/artifacts?"+q.Encode(), nil, nil)
		var res struct {
			IsTruncated                       bool
			NextKeyMarker, NextUploadIdMarker string
			Uploads                           []struct{ Key, UploadId, Initiated string } `xml:"Upload"`
		}
		if err := xml.Unmarshal([]byte(r.body), &res); r.status != http.StatusOK || err != nil {
			t.Fatalf("page %d: %d %s", page, r.status, r.body)
		}
		for _, u := range res.Uploads {
			if !ids[u.UploadId] || seen[u.UploadId] || u.Initiated != "2026-10-14T01:02:03.000Z" {
				t.Fatalf("upload %+v", u)
			}
			seen[u.UploadId] = true
		}
		if !res.IsTruncated {
			break
		}
		q.Set("key-marker", res.NextKeyMarker)
		q.Set("upload-id-marker", res.NextUploadIdMarker)
	}
	if len(seen) != 3 {
		t.Fatalf("saw %d uploads", len(seen))
	}
	if got := strings.Join(s.Uploads(), ","); got != "o/r/1/a.zip,o/r/1/a.zip,o/r/2/b.zip" {
		t.Fatalf("Uploads %q", got)
	}
}

func TestFailNextFailsOneRequest(t *testing.T) {
	for _, tc := range []struct {
		op           string
		method, path string
	}{
		{"list", http.MethodGet, "/artifacts?list-type=2"},
		{"head", http.MethodHead, "/artifacts/k"},
		{"get", http.MethodGet, "/artifacts/k"},
		{"put", http.MethodPut, "/artifacts/k"},
		{"create-multipart", http.MethodPost, "/artifacts/k?uploads="},
		{"upload-part", http.MethodPut, "/artifacts/k?partNumber=1&uploadId=UPLOAD"},
		{"complete", http.MethodPost, "/artifacts/k?uploadId=UPLOAD"},
		{"abort", http.MethodDelete, "/artifacts/k?uploadId=UPLOAD"},
		{"delete-objects", http.MethodPost, "/artifacts?delete="},
		{"delete", http.MethodDelete, "/artifacts/k"},
		{"list-uploads", http.MethodGet, "/artifacts?uploads="},
	} {
		t.Run(tc.op, func(t *testing.T) {
			s := NewServer(t, "artifacts")
			s.Put("k", Object{Body: []byte("v")})
			s.FailNext(tc.op, http.StatusServiceUnavailable, "SlowDown")
			path := strings.Replace(tc.path, "UPLOAD", s.SeedUpload("k", time.Now()), 1)
			r := send(t, s, tc.method, path, nil, nil)
			if r.status != http.StatusServiceUnavailable {
				t.Fatalf("first request: %d %s", r.status, r.body)
			}
			if tc.method != http.MethodHead && r.code() != "SlowDown" {
				t.Fatalf("first request code %q", r.code())
			}
			if r := send(t, s, tc.method, path, nil, nil); r.status == http.StatusServiceUnavailable {
				t.Fatalf("second request failed too: %s", r.body)
			}
		})
	}
}

func TestFailNextRefusesAnUnknownOperation(t *testing.T) {
	s := NewServer(t, "artifacts")
	defer func() {
		if recover() == nil {
			t.Fatal("FailNext accepted an operation the server never names")
		}
	}()
	s.FailNext("head-object", http.StatusServiceUnavailable, "SlowDown")
}

func TestFailNextWithStatusOK(t *testing.T) {
	s := NewServer(t, "artifacts")
	s.FailNext("complete", http.StatusOK, "InternalError")
	id := s.SeedUpload("k", time.Now())
	r := send(t, s, http.MethodPost, "/artifacts/k?uploadId="+id, nil, []byte("<CompleteMultipartUpload/>"))
	if r.status != http.StatusOK || r.code() != "InternalError" {
		t.Fatalf("complete: %d %s", r.status, r.body)
	}
}

func TestCheckAuth(t *testing.T) {
	s := NewServer(t, "artifacts")
	s.Put("o/r/1/my file.zip", Object{Body: []byte("data")})
	s.CheckAuth(testCred, "us-east-1")
	path := "/artifacts/o/r/1/my%20file.zip"

	if r := send(t, s, http.MethodGet, path, nil, nil); r.status != http.StatusOK || r.body != "data" {
		t.Fatalf("signed: %d %s", r.status, r.body)
	}
	unsigned, err := http.NewRequest(http.MethodGet, s.URL()+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r := do(t, s, unsigned); r.status != http.StatusForbidden || r.code() != "SignatureDoesNotMatch" {
		t.Fatalf("unsigned: %d %s", r.status, r.body)
	}
	for name, cred := range map[string]s3.Credentials{
		"wrong secret": {AccessKeyID: testCred.AccessKeyID, SecretAccessKey: "other"},
		"wrong key id": {AccessKeyID: "AKIDOTHER", SecretAccessKey: testCred.SecretAccessKey},
	} {
		req, err := http.NewRequest(http.MethodGet, s.URL()+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		s3.SignRequest(req, cred, "us-east-1", s3.HashHex(nil), time.Now())
		if r := do(t, s, req); r.status != http.StatusForbidden || r.code() != "SignatureDoesNotMatch" {
			t.Fatalf("%s: %d %s", name, r.status, r.body)
		}
	}
	tampered, err := http.NewRequest(http.MethodGet, s.URL()+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	s3.SignRequest(tampered, testCred, "us-east-1", s3.HashHex(nil), time.Now())
	tampered.Header.Set("Range", "bytes=0-1")
	tampered.Header.Set("Authorization", strings.Replace(tampered.Header.Get("Authorization"), "SignedHeaders=", "SignedHeaders=range;", 1))
	if r := do(t, s, tampered); r.status != http.StatusForbidden {
		t.Fatalf("tampered: %d %s", r.status, r.body)
	}
	badHash, err := http.NewRequest(http.MethodPut, s.URL()+path, strings.NewReader("new"))
	if err != nil {
		t.Fatal(err)
	}
	s3.SignRequest(badHash, testCred, "us-east-1", s3.HashHex([]byte("other")), time.Now())
	if r := do(t, s, badHash); r.status != http.StatusBadRequest || r.code() != "XAmzContentSHA256Mismatch" {
		t.Fatalf("payload hash mismatch: %d %s", r.status, r.body)
	}

	for _, tc := range []struct {
		name   string
		cred   s3.Credentials
		signed time.Time
		path   string
		status int
		code   string
	}{
		{"presigned", testCred, time.Now(), path, http.StatusOK, ""},
		{"expired", testCred, time.Now().Add(-2 * time.Hour), path, http.StatusForbidden, "AccessDenied"},
		{"wrong secret", s3.Credentials{AccessKeyID: testCred.AccessKeyID, SecretAccessKey: "other"}, time.Now(), path, http.StatusForbidden, "SignatureDoesNotMatch"},
	} {
		link, err := s3.Presign(http.MethodGet, s.URL()+tc.path, tc.cred, "us-east-1", time.Hour, tc.signed)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(http.MethodGet, link, nil)
		if err != nil {
			t.Fatal(err)
		}
		r := do(t, s, req)
		if r.status != tc.status || (tc.code != "" && r.code() != tc.code) {
			t.Fatalf("%s: %d %s", tc.name, r.status, r.body)
		}
	}
	link, err := s3.Presign(http.MethodGet, s.URL()+path, testCred, "us-east-1", time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodGet, strings.Replace(link, "my%20file.zip", "other.zip", 1), nil)
	if err != nil {
		t.Fatal(err)
	}
	if r := do(t, s, req); r.status != http.StatusForbidden || r.code() != "SignatureDoesNotMatch" {
		t.Fatalf("link moved to another key: %d %s", r.status, r.body)
	}
}
