// Package s3test is an in-memory S3 server for tests. It speaks the subset of
// the S3 REST API the s3 package uses, over TLS, with path-style addressing.
package s3test

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/s3"
)

// pageSize is small so that callers exercise paging.
const pageSize = 2

const minPartSize = 5 << 20

type Object struct {
	Body                            []byte
	Meta                            map[string]string
	ContentType, ContentDisposition string
	LastModified                    time.Time
}

type upload struct {
	id, key   string
	initiated time.Time
	template  Object
	parts     map[int][]byte
}

type failure struct {
	status int
	code   string
}

type auth struct {
	cred   s3.Credentials
	region string
}

type Server struct {
	srv    *httptest.Server
	bucket string

	mu       sync.Mutex
	objects  map[string]Object
	uploads  map[string]*upload
	nextID   int
	fail     map[string]failure
	noBatch  bool
	requests []string
	auth     *auth
}

// NewServer starts a fake holding one bucket. Listings return two entries a
// page.
func NewServer(t testing.TB, bucket string) *Server {
	s := &Server{
		bucket:  bucket,
		objects: map[string]Object{},
		uploads: map[string]*upload{},
		fail:    map[string]failure{},
	}
	s.srv = httptest.NewTLSServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *Server) URL() string { return s.srv.URL }

func (s *Server) HTTPClient() *http.Client { return s.srv.Client() }

func (s *Server) Put(key string, o Object) {
	if o.LastModified.IsZero() {
		o.LastModified = time.Now()
	}
	o.LastModified = o.LastModified.UTC().Truncate(time.Second)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = o
}

func (s *Server) Object(key string) (Object, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.objects[key]
	return o, ok
}

func (s *Server) Keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.objects))
	for k := range s.objects {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Uploads returns the object key of every open multipart upload, sorted; a
// key with two open uploads appears twice.
func (s *Server) Uploads() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.uploads))
	for _, u := range s.uploads {
		keys = append(keys, u.key)
	}
	sort.Strings(keys)
	return keys
}

func (s *Server) SeedUpload(key string, initiated time.Time) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.openUpload(key, initiated.UTC().Truncate(time.Second), Object{})
}

var operations = []string{"list", "list-uploads", "delete-objects", "head", "get", "put",
	"create-multipart", "upload-part", "complete", "abort", "delete"}

// FailNext makes the next request of operation op fail with status and code.
// s3.Client.Head sends a GET, so "get" fails it; "head" fails only a HEAD
// request. A name that is not an operation panics, since its failure would
// never fire.
func (s *Server) FailNext(op string, status int, code string) {
	if !slices.Contains(operations, op) {
		panic("s3test: FailNext: unknown operation " + strconv.Quote(op))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail[op] = failure{status: status, code: code}
}

func (s *Server) DisableBatchDelete() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.noBatch = true
}

func (s *Server) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

func (s *Server) CheckAuth(cred s3.Credentials, region string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.auth = &auth{cred: cred, region: region}
}

func (s *Server) openUpload(key string, initiated time.Time, template Object) string {
	s.nextID++
	id := fmt.Sprintf("upload-%06d", s.nextID)
	s.uploads[id] = &upload{id: id, key: key, initiated: initiated, template: template, parts: map[int][]byte{}}
	return id
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "IncompleteBody", err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, r.Method+" "+r.URL.RequestURI())

	if s.auth != nil {
		if status, code, msg := s.auth.verify(r, body); status != 0 {
			writeError(w, r, status, code, msg)
			return
		}
	}
	key, bucketLevel, ok := s.splitPath(r.URL.Path)
	if !ok {
		writeError(w, r, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.")
		return
	}
	q := r.URL.Query()
	op := operation(r.Method, q, bucketLevel)
	if op == "" {
		writeError(w, r, http.StatusNotImplemented, "NotImplemented", "s3test does not implement "+r.Method+" "+r.URL.RequestURI())
		return
	}
	if f, ok := s.fail[op]; ok {
		delete(s.fail, op)
		writeError(w, r, f.status, f.code, f.code+" injected by s3test")
		return
	}
	switch op {
	case "list":
		s.list(w, r, q)
	case "head", "get":
		s.get(w, r, key, op == "get")
	case "put":
		s.put(w, r, key, body)
	case "create-multipart":
		s.createMultipart(w, r, key)
	case "upload-part":
		s.uploadPart(w, r, key, q, body)
	case "complete":
		s.complete(w, r, key, q.Get("uploadId"), body)
	case "abort":
		s.abort(w, r, key, q.Get("uploadId"))
	case "delete-objects":
		s.deleteObjects(w, r, body)
	case "delete":
		delete(s.objects, key)
		w.WriteHeader(http.StatusNoContent)
	case "list-uploads":
		s.listUploads(w, q)
	}
}

func (s *Server) splitPath(p string) (key string, bucketLevel, ok bool) {
	rest, found := strings.CutPrefix(p, "/"+s.bucket)
	switch {
	case !found:
		return "", false, false
	case rest == "" || rest == "/":
		return "", true, true
	case strings.HasPrefix(rest, "/"):
		return rest[1:], false, true
	}
	return "", false, false
}

func operation(method string, q url.Values, bucketLevel bool) string {
	has := func(k string) bool { _, ok := q[k]; return ok }
	if bucketLevel {
		switch {
		case method == http.MethodGet && has("uploads"):
			return "list-uploads"
		case method == http.MethodGet && q.Get("list-type") == "2":
			return "list"
		case method == http.MethodPost && has("delete"):
			return "delete-objects"
		}
		return ""
	}
	switch method {
	case http.MethodHead:
		return "head"
	case http.MethodGet:
		return "get"
	case http.MethodPut:
		if has("partNumber") && has("uploadId") {
			return "upload-part"
		}
		return "put"
	case http.MethodPost:
		if has("uploads") {
			return "create-multipart"
		}
		if has("uploadId") {
			return "complete"
		}
	case http.MethodDelete:
		if has("uploadId") {
			return "abort"
		}
		return "delete"
	}
	return ""
}

type listEntry struct {
	Key          string
	LastModified string
	ETag         string
	Size         int64
	StorageClass string
}

type listResult struct {
	XMLName               xml.Name `xml:"http://s3.amazonaws.com/doc/2006-03-01/ ListBucketResult"`
	Name                  string
	Prefix                string
	KeyCount              int
	MaxKeys               int
	IsTruncated           bool
	ContinuationToken     string `xml:",omitempty"`
	NextContinuationToken string `xml:",omitempty"`
	Contents              []listEntry
}

func (s *Server) list(w http.ResponseWriter, r *http.Request, q url.Values) {
	prefix := q.Get("prefix")
	after := ""
	if tok := q.Get("continuation-token"); tok != "" {
		b, err := base64.RawURLEncoding.DecodeString(tok)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "InvalidArgument", "The continuation token provided is incorrect.")
			return
		}
		after = string(b)
	}
	var keys []string
	for k := range s.objects {
		if strings.HasPrefix(k, prefix) && k > after {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	res := listResult{Name: s.bucket, Prefix: prefix, MaxKeys: pageSize, ContinuationToken: q.Get("continuation-token")}
	if len(keys) > pageSize {
		keys = keys[:pageSize]
		res.IsTruncated = true
		res.NextContinuationToken = base64.RawURLEncoding.EncodeToString([]byte(keys[len(keys)-1]))
	}
	for _, k := range keys {
		o := s.objects[k]
		res.Contents = append(res.Contents, listEntry{
			Key:          k,
			LastModified: o.LastModified.Format("2006-01-02T15:04:05.000Z"),
			ETag:         etag(o.Body),
			Size:         int64(len(o.Body)),
			StorageClass: "STANDARD",
		})
	}
	res.KeyCount = len(res.Contents)
	writeXML(w, http.StatusOK, res)
}

func (s *Server) get(w http.ResponseWriter, r *http.Request, key string, withBody bool) {
	o, ok := s.objects[key]
	if !ok {
		writeError(w, r, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.")
		return
	}
	h := w.Header()
	ct := o.ContentType
	if ct == "" {
		ct = "binary/octet-stream"
	}
	h.Set("Content-Type", ct)
	if o.ContentDisposition != "" {
		h.Set("Content-Disposition", o.ContentDisposition)
	}
	h.Set("Content-Length", strconv.Itoa(len(o.Body)))
	h.Set("ETag", etag(o.Body))
	h.Set("Last-Modified", o.LastModified.Format(http.TimeFormat))
	for k, v := range o.Meta {
		h.Set("X-Amz-Meta-"+k, v)
	}
	w.WriteHeader(http.StatusOK)
	if withBody {
		w.Write(o.Body)
	}
}

func objectFrom(r *http.Request, body []byte) Object {
	o := Object{
		Body:               body,
		ContentType:        r.Header.Get("Content-Type"),
		ContentDisposition: r.Header.Get("Content-Disposition"),
		LastModified:       time.Now().UTC().Truncate(time.Second),
	}
	for k, vs := range r.Header {
		if name, ok := strings.CutPrefix(strings.ToLower(k), "x-amz-meta-"); ok && len(vs) > 0 {
			if o.Meta == nil {
				o.Meta = map[string]string{}
			}
			o.Meta[name] = vs[0]
		}
	}
	return o
}

func (s *Server) put(w http.ResponseWriter, r *http.Request, key string, body []byte) {
	if r.ContentLength < 0 {
		writeError(w, r, http.StatusLengthRequired, "MissingContentLength", "You must provide the Content-Length HTTP header.")
		return
	}
	s.objects[key] = objectFrom(r, body)
	w.Header().Set("ETag", etag(body))
	w.WriteHeader(http.StatusOK)
}

type initiateResult struct {
	XMLName  xml.Name `xml:"http://s3.amazonaws.com/doc/2006-03-01/ InitiateMultipartUploadResult"`
	Bucket   string
	Key      string
	UploadId string
}

func (s *Server) createMultipart(w http.ResponseWriter, r *http.Request, key string) {
	template := objectFrom(r, nil)
	id := s.openUpload(key, time.Now().UTC().Truncate(time.Second), template)
	writeXML(w, http.StatusOK, initiateResult{Bucket: s.bucket, Key: key, UploadId: id})
}

func (s *Server) openFor(w http.ResponseWriter, r *http.Request, key, id string) *upload {
	u, ok := s.uploads[id]
	if !ok || u.key != key {
		writeError(w, r, http.StatusNotFound, "NoSuchUpload", "The specified multipart upload does not exist.")
		return nil
	}
	return u
}

func (s *Server) uploadPart(w http.ResponseWriter, r *http.Request, key string, q url.Values, body []byte) {
	if r.ContentLength < 0 {
		writeError(w, r, http.StatusLengthRequired, "MissingContentLength", "You must provide the Content-Length HTTP header.")
		return
	}
	u := s.openFor(w, r, key, q.Get("uploadId"))
	if u == nil {
		return
	}
	n, err := strconv.Atoi(q.Get("partNumber"))
	if err != nil || n < 1 || n > 10000 {
		writeError(w, r, http.StatusBadRequest, "InvalidArgument", "Part number must be an integer between 1 and 10000, inclusive.")
		return
	}
	u.parts[n] = body
	w.Header().Set("ETag", etag(body))
	w.WriteHeader(http.StatusOK)
}

type completeRequest struct {
	Parts []struct {
		PartNumber int
		ETag       string
	} `xml:"Part"`
}

type completeResult struct {
	XMLName  xml.Name `xml:"http://s3.amazonaws.com/doc/2006-03-01/ CompleteMultipartUploadResult"`
	Location string
	Bucket   string
	Key      string
	ETag     string
}

func (s *Server) complete(w http.ResponseWriter, r *http.Request, key, id string, body []byte) {
	u := s.openFor(w, r, key, id)
	if u == nil {
		return
	}
	var req completeRequest
	if err := xml.Unmarshal(body, &req); err != nil || len(req.Parts) == 0 {
		writeError(w, r, http.StatusBadRequest, "MalformedXML", "The XML you provided was not well-formed or did not validate against our published schema.")
		return
	}
	var all bytes.Buffer
	for i, p := range req.Parts {
		if i > 0 && p.PartNumber <= req.Parts[i-1].PartNumber {
			writeError(w, r, http.StatusBadRequest, "InvalidPartOrder", "The list of parts was not in ascending order.")
			return
		}
		data, ok := u.parts[p.PartNumber]
		if !ok || p.ETag != etag(data) {
			writeError(w, r, http.StatusBadRequest, "InvalidPart", "One or more of the specified parts could not be found.")
			return
		}
		if i < len(req.Parts)-1 && len(data) < minPartSize {
			writeError(w, r, http.StatusBadRequest, "EntityTooSmall", "Your proposed upload is smaller than the minimum allowed object size.")
			return
		}
		all.Write(data)
	}
	o := u.template
	o.Body = all.Bytes()
	o.LastModified = time.Now().UTC().Truncate(time.Second)
	s.objects[key] = o
	delete(s.uploads, id)
	writeXML(w, http.StatusOK, completeResult{Location: s.srv.URL + r.URL.Path, Bucket: s.bucket, Key: key, ETag: etag(o.Body)})
}

func (s *Server) abort(w http.ResponseWriter, r *http.Request, key, id string) {
	if s.openFor(w, r, key, id) == nil {
		return
	}
	delete(s.uploads, id)
	w.WriteHeader(http.StatusNoContent)
}

type deleteRequest struct {
	Quiet   bool
	Objects []struct{ Key string } `xml:"Object"`
}

type deletedEntry struct{ Key string }

type deleteResult struct {
	XMLName xml.Name       `xml:"http://s3.amazonaws.com/doc/2006-03-01/ DeleteResult"`
	Deleted []deletedEntry `xml:"Deleted"`
}

func (s *Server) deleteObjects(w http.ResponseWriter, r *http.Request, body []byte) {
	if s.noBatch {
		writeError(w, r, http.StatusNotImplemented, "NotImplemented", "A header you provided implies functionality that is not implemented.")
		return
	}
	sum := md5.Sum(body)
	if got := r.Header.Get("Content-Md5"); got != base64.StdEncoding.EncodeToString(sum[:]) {
		writeError(w, r, http.StatusBadRequest, "InvalidDigest", "The Content-MD5 you specified was missing or did not match.")
		return
	}
	var req deleteRequest
	if err := xml.Unmarshal(body, &req); err != nil || len(req.Objects) == 0 || len(req.Objects) > 1000 {
		writeError(w, r, http.StatusBadRequest, "MalformedXML", "The XML you provided was not well-formed or did not validate against our published schema.")
		return
	}
	var res deleteResult
	for _, o := range req.Objects {
		delete(s.objects, o.Key)
		if !req.Quiet {
			res.Deleted = append(res.Deleted, deletedEntry{Key: o.Key})
		}
	}
	writeXML(w, http.StatusOK, res)
}

type uploadEntry struct {
	Key       string
	UploadId  string
	Initiated string
}

type listUploadsResult struct {
	XMLName            xml.Name `xml:"http://s3.amazonaws.com/doc/2006-03-01/ ListMultipartUploadsResult"`
	Bucket             string
	KeyMarker          string
	UploadIdMarker     string
	NextKeyMarker      string `xml:",omitempty"`
	NextUploadIdMarker string `xml:",omitempty"`
	MaxUploads         int
	IsTruncated        bool
	Uploads            []uploadEntry `xml:"Upload"`
}

func (s *Server) listUploads(w http.ResponseWriter, q url.Values) {
	keyMarker, idMarker := q.Get("key-marker"), q.Get("upload-id-marker")
	var open []*upload
	for _, u := range s.uploads {
		if u.key > keyMarker || (u.key == keyMarker && u.id > idMarker) {
			open = append(open, u)
		}
	}
	sort.Slice(open, func(i, j int) bool {
		if open[i].key != open[j].key {
			return open[i].key < open[j].key
		}
		return open[i].id < open[j].id
	})
	res := listUploadsResult{Bucket: s.bucket, KeyMarker: keyMarker, UploadIdMarker: idMarker, MaxUploads: pageSize}
	if len(open) > pageSize {
		open = open[:pageSize]
		res.IsTruncated = true
		res.NextKeyMarker = open[len(open)-1].key
		res.NextUploadIdMarker = open[len(open)-1].id
	}
	for _, u := range open {
		res.Uploads = append(res.Uploads, uploadEntry{Key: u.key, UploadId: u.id, Initiated: u.initiated.Format("2006-01-02T15:04:05.000Z")})
	}
	writeXML(w, http.StatusOK, res)
}

func (a *auth) verify(r *http.Request, body []byte) (status int, code, msg string) {
	const denied = "The request signature we calculated does not match the signature you provided. Check your key and signing method."
	q := r.URL.Query()
	if sig := q.Get("X-Amz-Signature"); sig != "" {
		date, err := time.Parse("20060102T150405Z", q.Get("X-Amz-Date"))
		secs, err2 := strconv.Atoi(q.Get("X-Amz-Expires"))
		if err != nil || err2 != nil {
			return http.StatusForbidden, "AuthorizationQueryParametersError", "X-Amz-Date and X-Amz-Expires must be set."
		}
		ttl := time.Duration(secs) * time.Second
		for _, k := range []string{"X-Amz-Algorithm", "X-Amz-Credential", "X-Amz-Date", "X-Amz-Expires", "X-Amz-SignedHeaders", "X-Amz-Signature"} {
			q.Del(k)
		}
		base := "https://" + r.Host + s3.EscapeKey(r.URL.Path)
		if len(q) > 0 {
			base += "?" + q.Encode()
		}
		signed, err := s3.Presign(r.Method, base, a.cred, a.region, ttl, date)
		if err != nil {
			return http.StatusForbidden, "AuthorizationQueryParametersError", err.Error()
		}
		u, err := url.Parse(signed)
		if err != nil || u.Query().Get("X-Amz-Signature") != sig {
			return http.StatusForbidden, "SignatureDoesNotMatch", denied
		}
		if time.Now().After(date.Add(ttl)) {
			return http.StatusForbidden, "AccessDenied", "Request has expired"
		}
		return 0, "", ""
	}

	got := r.Header.Get("Authorization")
	_, list, ok := strings.Cut(got, ",SignedHeaders=")
	if !ok {
		return http.StatusForbidden, "SignatureDoesNotMatch", denied
	}
	list, _, _ = strings.Cut(list, ",")
	date, err := time.Parse("20060102T150405Z", r.Header.Get("X-Amz-Date"))
	if err != nil {
		return http.StatusForbidden, "SignatureDoesNotMatch", denied
	}
	payload := r.Header.Get("X-Amz-Content-Sha256")
	if payload != s3.UnsignedPayload && payload != s3.HashHex(body) {
		return http.StatusBadRequest, "XAmzContentSHA256Mismatch", "The provided 'x-amz-content-sha256' header does not match what was computed."
	}
	clone := &http.Request{Method: r.Method, URL: r.URL, Host: r.Host, Header: http.Header{}}
	for _, h := range strings.Split(list, ";") {
		if h != "host" {
			clone.Header[http.CanonicalHeaderKey(h)] = r.Header.Values(h)
		}
	}
	s3.SignRequest(clone, a.cred, a.region, payload, date)
	if clone.Header.Get("Authorization") != got {
		return http.StatusForbidden, "SignatureDoesNotMatch", denied
	}
	return 0, "", ""
}

func etag(b []byte) string {
	sum := md5.Sum(b)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

type errorBody struct {
	XMLName  xml.Name `xml:"Error"`
	Code     string
	Message  string
	Resource string
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	writeXML(w, status, errorBody{Code: code, Message: msg, Resource: r.URL.Path})
}

func writeXML(w http.ResponseWriter, status int, v any) {
	b, err := xml.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	io.WriteString(w, xml.Header)
	w.Write(b)
}
