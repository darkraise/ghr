package s3

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	Endpoint string // "https://host[:port]", no path
	Bucket   string
	Region   string
	Cred     Credentials
	HTTP     *http.Client     // nil means http.DefaultClient
	Now      func() time.Time // nil means time.Now
}

func New(endpoint, bucket, region string, cred Credentials) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, errors.New("s3: the endpoint is not a valid URL; use https://host[:port]")
	}
	if u.Scheme != "https" || u.Host == "" {
		return nil, fmt.Errorf("s3: the endpoint %q must be an https:// address", u.Redacted())
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.User != nil {
		return nil, fmt.Errorf("s3: the endpoint %q must be only https://host[:port], without a path, query or user info", u.Redacted())
	}
	if bucket == "" {
		return nil, errors.New("s3: the bucket name is empty")
	}
	if region == "" {
		return nil, errors.New("s3: the region is empty")
	}
	if cred.AccessKeyID == "" || cred.SecretAccessKey == "" {
		return nil, errors.New("s3: the access key ID or secret is empty")
	}
	return &Client{Endpoint: "https://" + u.Host, Bucket: bucket, Region: region, Cred: cred}, nil
}

type Object struct {
	Key          string
	Size         int64
	ETag         string
	LastModified time.Time
}

// Head describes a stored object. Meta keys are lower-case, without the
// "x-amz-meta-" prefix.
type Head struct {
	Size                            int64
	ETag                            string
	LastModified                    time.Time
	Meta                            map[string]string
	ContentType, ContentDisposition string
}

type PutOptions struct {
	ContentType, ContentDisposition string
	Meta                            map[string]string
}

type Upload struct {
	Key, UploadID string
	Initiated     time.Time
}

type Error struct {
	Status        int
	Code, Message string
}

func (e *Error) Error() string {
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	if e.Code == "" {
		return fmt.Sprintf("s3: %d %s", e.Status, msg)
	}
	return fmt.Sprintf("s3: %d %s: %s", e.Status, e.Code, msg)
}

func IsNotFound(err error) bool {
	var e *Error
	if !errors.As(err, &e) {
		return false
	}
	switch e.Code {
	case "NoSuchKey", "NoSuchBucket", "NotFound":
		return true
	}
	return e.Status == http.StatusNotFound
}

const maxErrorBody = 64 << 10

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// objectURL builds a path-style URL; an empty key addresses the bucket.
func (c *Client) objectURL(key string, q url.Values) string {
	s := c.Endpoint + "/" + EscapeKey(c.Bucket)
	if key != "" {
		s += "/" + EscapeKey(key)
	}
	if len(q) > 0 {
		s += "?" + canonicalQuery(q)
	}
	return s
}

type call struct {
	method  string
	key     string
	query   url.Values
	header  http.Header
	body    io.ReadSeeker
	size    int64
	payload string // SHA-256 hex of the body, or UnsignedPayload
}

// do sends a signed request. A status of 300 or more comes back as *Error.
func (c *Client) do(ctx context.Context, k call) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, k.method, c.objectURL(k.key, k.query), nil)
	if err != nil {
		return nil, err
	}
	if k.body != nil && k.size > 0 {
		start, err := k.body.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, err
		}
		req.Body = io.NopCloser(io.LimitReader(k.body, k.size))
		req.ContentLength = k.size
		req.GetBody = func() (io.ReadCloser, error) {
			if _, err := k.body.Seek(start, io.SeekStart); err != nil {
				return nil, err
			}
			return io.NopCloser(io.LimitReader(k.body, k.size)), nil
		}
	} else if k.method == http.MethodPut || k.method == http.MethodPost {
		req.Body = http.NoBody
	}
	for name, vs := range k.header {
		req.Header[name] = vs
	}
	payload := k.payload
	if payload == "" {
		payload = HashHex(nil)
	}
	SignRequest(req, c.Cred, c.Region, payload, c.now())
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, readError(resp)
	}
	return resp, nil
}

func readError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	e := &Error{Status: resp.StatusCode}
	var body struct{ Code, Message string }
	if xml.Unmarshal(b, &body) == nil {
		e.Code, e.Message = body.Code, body.Message
	}
	return e
}

// doXML sends k and decodes a 2xx XML answer into out. S3 can answer 200 with
// an <Error> body, which comes back as *Error.
func (c *Client) doXML(ctx context.Context, k call, out any) error {
	resp, err := c.do(ctx, k)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var probe struct {
		XMLName       xml.Name
		Code, Message string
	}
	if xml.Unmarshal(b, &probe) == nil && probe.XMLName.Local == "Error" {
		return &Error{Status: resp.StatusCode, Code: probe.Code, Message: probe.Message}
	}
	if out == nil {
		return nil
	}
	if err := xml.Unmarshal(b, out); err != nil {
		return fmt.Errorf("s3: unreadable answer to %s %s: %w", k.method, c.objectURL(k.key, k.query), err)
	}
	return nil
}

func xmlBody(v any) ([]byte, error) {
	b, err := xml.Marshal(v)
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), b...), nil
}

type listResult struct {
	IsTruncated           bool
	NextContinuationToken string
	Contents              []struct {
		Key          string
		Size         int64
		ETag         string
		LastModified time.Time
	}
}

func (c *Client) List(ctx context.Context, prefix string) ([]Object, error) {
	var out []Object
	token := ""
	for {
		q := url.Values{"list-type": {"2"}}
		if prefix != "" {
			q.Set("prefix", prefix)
		}
		if token != "" {
			q.Set("continuation-token", token)
		}
		var res listResult
		if err := c.doXML(ctx, call{method: http.MethodGet, query: q}, &res); err != nil {
			return nil, err
		}
		for _, o := range res.Contents {
			out = append(out, Object{Key: o.Key, Size: o.Size, ETag: strings.Trim(o.ETag, `"`), LastModified: o.LastModified.UTC()})
		}
		if !res.IsTruncated {
			return out, nil
		}
		if res.NextContinuationToken == "" || res.NextContinuationToken == token {
			return nil, errors.New("s3: the bucket listing stopped early without a continuation token")
		}
		token = res.NextContinuationToken
	}
}

func headFrom(resp *http.Response) Head {
	h := Head{
		Size:               resp.ContentLength,
		ETag:               strings.Trim(resp.Header.Get("ETag"), `"`),
		ContentType:        resp.Header.Get("Content-Type"),
		ContentDisposition: resp.Header.Get("Content-Disposition"),
		Meta:               map[string]string{},
	}
	if h.Size < 0 {
		h.Size, _ = strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	}
	if t, err := http.ParseTime(resp.Header.Get("Last-Modified")); err == nil {
		h.LastModified = t.UTC()
	}
	var dec mime.WordDecoder
	for name, vs := range resp.Header {
		if k, ok := strings.CutPrefix(strings.ToLower(name), "x-amz-meta-"); ok && len(vs) > 0 {
			v := vs[0]
			if d, err := dec.DecodeHeader(v); err == nil {
				v = d
			}
			h.Meta[k] = v
		}
	}
	return h
}

// Head reads an object's details from the headers of a GET whose body is
// never read, because proxies such as Cloudflare's refuse signed HEAD requests
// and drop Range headers.
func (c *Client) Head(ctx context.Context, key string) (Head, error) {
	resp, err := c.do(ctx, call{method: http.MethodGet, key: key})
	if err != nil {
		return Head{}, err
	}
	resp.Body.Close()
	return headFrom(resp), nil
}

// Get streams the object; the caller closes the body.
func (c *Client) Get(ctx context.Context, key string) (io.ReadCloser, Head, error) {
	resp, err := c.do(ctx, call{method: http.MethodGet, key: key})
	if err != nil {
		return nil, Head{}, err
	}
	return resp.Body, headFrom(resp), nil
}

// putHeader carries the object's headers. Metadata values outside printable
// ASCII are RFC 2047 encoded, because S3 only stores ASCII header values;
// Head decodes them.
func putHeader(opt PutOptions) http.Header {
	h := http.Header{}
	if opt.ContentType != "" {
		h.Set("Content-Type", opt.ContentType)
	}
	if opt.ContentDisposition != "" {
		h.Set("Content-Disposition", opt.ContentDisposition)
	}
	for k, v := range opt.Meta {
		h.Set("X-Amz-Meta-"+strings.ToLower(k), mime.QEncoding.Encode("utf-8", v))
	}
	return h
}

func (c *Client) Put(ctx context.Context, key string, body io.ReadSeeker, size int64, opt PutOptions) error {
	resp, err := c.do(ctx, call{method: http.MethodPut, key: key, header: putHeader(opt), body: body, size: size, payload: UnsignedPayload})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

type deleteRequest struct {
	XMLName xml.Name `xml:"Delete"`
	Quiet   bool
	Objects []deleteObject `xml:"Object"`
}

type deleteObject struct{ Key string }

type deleteResult struct {
	Deleted []struct{ Key string } `xml:"Deleted"`
	Errors  []struct {
		Key, Code, Message string
	} `xml:"Error"`
}

const deleteBatch = 1000

// Delete removes keys and returns the ones it removed. A key that did not
// exist counts as removed; a missing bucket fails the call. It stops at a
// failed request; per-key failures
// inside a batch answer are skipped and the first one is returned at the end.
func (c *Client) Delete(ctx context.Context, keys []string) (deleted []string, err error) {
	var firstKeyErr error
	batch := true
	for start := 0; start < len(keys); start += deleteBatch {
		chunk := keys[start:min(start+deleteBatch, len(keys))]
		if batch {
			done, keyErr, err := c.deleteBatch(ctx, chunk)
			var e *Error
			if errors.As(err, &e) && (e.Status == http.StatusNotImplemented || e.Code == "NotImplemented") {
				batch = false
			} else {
				deleted = append(deleted, done...)
				if err != nil {
					return deleted, err
				}
				if firstKeyErr == nil {
					firstKeyErr = keyErr
				}
				continue
			}
		}
		for _, key := range chunk {
			resp, err := c.do(ctx, call{method: http.MethodDelete, key: key})
			if err != nil && (!IsNotFound(err) || bucketMissing(err)) {
				return deleted, err
			}
			if resp != nil {
				resp.Body.Close()
			}
			deleted = append(deleted, key)
		}
	}
	return deleted, firstKeyErr
}

func bucketMissing(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == "NoSuchBucket"
}

func (c *Client) deleteBatch(ctx context.Context, keys []string) (deleted []string, keyErr, err error) {
	req := deleteRequest{Objects: make([]deleteObject, len(keys))}
	for i, k := range keys {
		req.Objects[i].Key = k
	}
	body, err := xmlBody(req)
	if err != nil {
		return nil, nil, err
	}
	sum := md5.Sum(body)
	h := http.Header{}
	h.Set("Content-MD5", base64.StdEncoding.EncodeToString(sum[:]))
	h.Set("Content-Type", "application/xml")
	var res deleteResult
	k := call{method: http.MethodPost, query: url.Values{"delete": {""}}, header: h, body: bytes.NewReader(body), size: int64(len(body)), payload: HashHex(body)}
	if err := c.doXML(ctx, k, &res); err != nil {
		return nil, nil, err
	}
	for _, d := range res.Deleted {
		deleted = append(deleted, d.Key)
	}
	if len(res.Errors) > 0 {
		first := res.Errors[0]
		keyErr = &Error{Status: http.StatusOK, Code: first.Code, Message: fmt.Sprintf("%s: %s (%d of %d objects not deleted)", first.Key, first.Message, len(res.Errors), len(keys))}
	}
	return deleted, keyErr, nil
}

type listUploadsResult struct {
	IsTruncated        bool
	NextKeyMarker      string
	NextUploadIdMarker string
	Uploads            []struct {
		Key       string
		UploadId  string
		Initiated time.Time
	} `xml:"Upload"`
}

func (c *Client) ListUploads(ctx context.Context) ([]Upload, error) {
	var out []Upload
	q := url.Values{"uploads": {""}}
	for {
		var res listUploadsResult
		if err := c.doXML(ctx, call{method: http.MethodGet, query: q}, &res); err != nil {
			return nil, err
		}
		for _, u := range res.Uploads {
			out = append(out, Upload{Key: u.Key, UploadID: u.UploadId, Initiated: u.Initiated.UTC()})
		}
		if !res.IsTruncated {
			return out, nil
		}
		if res.NextKeyMarker == "" || (res.NextKeyMarker == q.Get("key-marker") && res.NextUploadIdMarker == q.Get("upload-id-marker")) {
			return nil, errors.New("s3: the multipart upload listing stopped early without a marker")
		}
		q.Set("key-marker", res.NextKeyMarker)
		q.Set("upload-id-marker", res.NextUploadIdMarker)
	}
}

func (c *Client) Abort(ctx context.Context, key, uploadID string) error {
	resp, err := c.do(ctx, call{method: http.MethodDelete, key: key, query: url.Values{"uploadId": {uploadID}}})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (c *Client) PresignGet(key string, ttl time.Duration) (string, time.Time, error) {
	now := c.now()
	link, err := Presign(http.MethodGet, c.objectURL(key, nil), c.Cred, c.Region, ttl, now)
	if err != nil {
		return "", time.Time{}, err
	}
	return link, now.Add(ttl), nil
}
