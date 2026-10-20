// Package s3 is a small S3 client: AWS Signature Version 4, path-style
// addressing, and the object, multipart and listing calls ghr needs.
package s3

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Credentials struct{ AccessKeyID, SecretAccessKey string }

const (
	UnsignedPayload = "UNSIGNED-PAYLOAD"
	PartSize        = 16 << 20 // bytes; Upload uses one PUT below it
	PartConcurrency = 4
	MaxPresign      = 7 * 24 * time.Hour
)

const (
	algorithm  = "AWS4-HMAC-SHA256"
	service    = "s3"
	timeFormat = "20060102T150405Z"
	dateFormat = "20060102"
)

// SignRequest signs req with SigV4 for service "s3". It signs the host and
// every header already on req except Authorization and User-Agent, so set
// all headers before calling it.
func SignRequest(req *http.Request, cred Credentials, region, payloadHash string, now time.Time) {
	now = now.UTC()
	amzDate := now.Format(timeFormat)
	req.Header.Del("Authorization")
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	signed, headers := canonicalHeaders(req)
	canonical := strings.Join([]string{
		req.Method,
		canonicalURI(req.URL),
		canonicalQuery(req.URL.Query()),
		headers,
		signed,
		payloadHash,
	}, "\n")
	scope := credentialScope(now, region)
	sig := signature(cred.SecretAccessKey, now, region, stringToSign(amzDate, scope, canonical))
	req.Header.Set("Authorization", algorithm+" Credential="+cred.AccessKeyID+"/"+scope+
		",SignedHeaders="+signed+",Signature="+sig)
}

// Presign returns rawURL with a query-string SigV4 signature that only signs
// the host header and lasts ttl.
func Presign(method, rawURL string, cred Credentials, region string, ttl time.Duration, now time.Time) (string, error) {
	if ttl <= 0 || ttl > MaxPresign {
		return "", fmt.Errorf("s3: a signed link must last more than 0 seconds and at most 7 days, not %s", ttl)
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("s3: sign %q: %w", rawURL, err)
	}
	now = now.UTC()
	amzDate := now.Format(timeFormat)
	scope := credentialScope(now, region)
	q := u.Query()
	q.Set("X-Amz-Algorithm", algorithm)
	q.Set("X-Amz-Credential", cred.AccessKeyID+"/"+scope)
	q.Set("X-Amz-Date", amzDate)
	q.Set("X-Amz-Expires", strconv.FormatInt(int64((ttl+time.Second-1)/time.Second), 10))
	q.Set("X-Amz-SignedHeaders", "host")
	query := canonicalQuery(q)
	canonical := strings.Join([]string{
		method,
		canonicalURI(u),
		query,
		"host:" + u.Host + "\n",
		"host",
		UnsignedPayload,
	}, "\n")
	sig := signature(cred.SecretAccessKey, now, region, stringToSign(amzDate, scope, canonical))
	u.RawQuery = query + "&X-Amz-Signature=" + sig
	return u.String(), nil
}

func HashHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func EscapeKey(key string) string {
	return escape(key, true)
}

func escape(s string, keepSlash bool) string {
	const hexDigits = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if unreserved(c) || (keepSlash && c == '/') {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hexDigits[c>>4])
		b.WriteByte(hexDigits[c&15])
	}
	return b.String()
}

func unreserved(c byte) bool {
	return 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' ||
		c == '-' || c == '_' || c == '.' || c == '~'
}

func canonicalURI(u *url.URL) string {
	if u.Path == "" {
		return "/"
	}
	return EscapeKey(u.Path)
}

// canonicalQuery is also the query the client sends, so the wire form and the
// signed form never differ.
func canonicalQuery(q url.Values) string {
	pairs := make([]string, 0, len(q))
	for k, vs := range q {
		ek := escape(k, false)
		for _, v := range vs {
			pairs = append(pairs, ek+"="+escape(v, false))
		}
	}
	sort.Strings(pairs)
	return strings.Join(pairs, "&")
}

func canonicalHeaders(req *http.Request) (signed, canonical string) {
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	values := map[string]string{"host": host}
	for k, vs := range req.Header {
		lk := strings.ToLower(k)
		if lk == "authorization" || lk == "user-agent" {
			continue
		}
		trimmed := make([]string, len(vs))
		for i, v := range vs {
			trimmed[i] = strings.Join(strings.Fields(v), " ")
		}
		values[lk] = strings.Join(trimmed, ",")
	}
	names := make([]string, 0, len(values))
	for k := range values {
		names = append(names, k)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, k := range names {
		b.WriteString(k + ":" + values[k] + "\n")
	}
	return strings.Join(names, ";"), b.String()
}

func credentialScope(now time.Time, region string) string {
	return now.Format(dateFormat) + "/" + region + "/" + service + "/aws4_request"
}

func stringToSign(amzDate, scope, canonical string) string {
	sum := sha256.Sum256([]byte(canonical))
	return algorithm + "\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(sum[:])
}

func signature(secret string, now time.Time, region, toSign string) string {
	key := hmacSHA256([]byte("AWS4"+secret), now.Format(dateFormat))
	key = hmacSHA256(key, region)
	key = hmacSHA256(key, service)
	key = hmacSHA256(key, "aws4_request")
	return hex.EncodeToString(hmacSHA256(key, toSign))
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}
