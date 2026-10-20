package s3

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// The AWS example credentials and time used by the S3 SigV4 documentation.
var (
	exampleCred = Credentials{AccessKeyID: "AKIAIOSFODNN7EXAMPLE", SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"}
	exampleTime = time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)
)

const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

func TestHashHex(t *testing.T) {
	for in, want := range map[string]string{
		"":                      emptySHA256,
		"Welcome to Amazon S3.": "44ce7dd67c959e0d3524ffac1771dfbba87d2b6b4b4e99e42034a8b803f8b072",
	} {
		if got := HashHex([]byte(in)); got != want {
			t.Errorf("HashHex(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEscapeKey(t *testing.T) {
	for in, want := range map[string]string{
		"darkraise/darkmem/1932/dist.zip": "darkraise/darkmem/1932/dist.zip",
		"test$file.text":                  "test%24file.text",
		"a b/c+d":                         "a%20b/c%2Bd",
		"Az09-_.~":                        "Az09-_.~",
		"a//b/":                           "a//b/",
		"ü&=?#%":                          "%C3%BC%26%3D%3F%23%25",
	} {
		if got := EscapeKey(in); got != want {
			t.Errorf("EscapeKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// Header-signing examples from "Signature Calculations for the Authorization
// Header: Transferring Payload in a Single Chunk" in the Amazon S3 API
// reference.
func TestSignRequestAWSExamples(t *testing.T) {
	for _, tc := range []struct {
		name    string
		method  string
		url     string
		header  map[string]string
		payload string
		want    string
	}{
		{
			name:    "GET object",
			method:  http.MethodGet,
			url:     "https://examplebucket.s3.amazonaws.com/test.txt",
			header:  map[string]string{"Range": "bytes=0-9"},
			payload: emptySHA256,
			want:    "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request,SignedHeaders=host;range;x-amz-content-sha256;x-amz-date,Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41",
		},
		{
			name:   "PUT object",
			method: http.MethodPut,
			url:    "https://examplebucket.s3.amazonaws.com/test$file.text",
			header: map[string]string{
				"Date":                "Fri, 24 May 2013 00:00:00 GMT",
				"X-Amz-Storage-Class": "REDUCED_REDUNDANCY",
			},
			payload: "44ce7dd67c959e0d3524ffac1771dfbba87d2b6b4b4e99e42034a8b803f8b072",
			want:    "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request,SignedHeaders=date;host;x-amz-content-sha256;x-amz-date;x-amz-storage-class,Signature=98ad721746da40c64f1a55b78f14c238d841ea1380cd77a1b5971af0ece108bd",
		},
		{
			name:    "GET bucket lifecycle",
			method:  http.MethodGet,
			url:     "https://examplebucket.s3.amazonaws.com/?lifecycle",
			payload: emptySHA256,
			want:    "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request,SignedHeaders=host;x-amz-content-sha256;x-amz-date,Signature=fea454ca298b7da1c68078a5d1bdbfbbe0d65c699e0f91ac7a200a0136783543",
		},
		{
			name:    "GET bucket list objects",
			method:  http.MethodGet,
			url:     "https://examplebucket.s3.amazonaws.com/?max-keys=2&prefix=J",
			payload: emptySHA256,
			want:    "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request,SignedHeaders=host;x-amz-content-sha256;x-amz-date,Signature=34b48302e7b5fa45bde8084f4b7868a86f0a534bc59db6670ed5711ef69dc6f7",
		},
		{
			name:    "query order does not change the signature",
			method:  http.MethodGet,
			url:     "https://examplebucket.s3.amazonaws.com/?prefix=J&max-keys=2",
			payload: emptySHA256,
			want:    "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request,SignedHeaders=host;x-amz-content-sha256;x-amz-date,Signature=34b48302e7b5fa45bde8084f4b7868a86f0a534bc59db6670ed5711ef69dc6f7",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, tc.url, nil)
			if err != nil {
				t.Fatal(err)
			}
			for k, v := range tc.header {
				req.Header.Set(k, v)
			}
			SignRequest(req, exampleCred, "us-east-1", tc.payload, exampleTime)
			if got := req.Header.Get("Authorization"); got != tc.want {
				t.Fatalf("Authorization\n got %s\nwant %s", got, tc.want)
			}
			if got := req.Header.Get("X-Amz-Date"); got != "20130524T000000Z" {
				t.Fatalf("X-Amz-Date = %q", got)
			}
			if got := req.Header.Get("X-Amz-Content-Sha256"); got != tc.payload {
				t.Fatalf("X-Amz-Content-Sha256 = %q", got)
			}
		})
	}
}

func TestSignRequestReplacesOldSignature(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=0-9")
	SignRequest(req, Credentials{AccessKeyID: "OLD", SecretAccessKey: "old"}, "us-east-1", emptySHA256, exampleTime.Add(time.Hour))
	req.Header.Set("User-Agent", "ghr-test")
	SignRequest(req, exampleCred, "us-east-1", emptySHA256, exampleTime)
	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request,SignedHeaders=host;range;x-amz-content-sha256;x-amz-date,Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("Authorization\n got %s\nwant %s", got, want)
	}
}

// The presigned GET example from "Authenticating Requests: Using Query
// Parameters (AWS Signature Version 4)" in the Amazon S3 API reference.
func TestPresignAWSExample(t *testing.T) {
	got, err := Presign(http.MethodGet, "https://examplebucket.s3.amazonaws.com/test.txt", exampleCred, "us-east-1", 86400*time.Second, exampleTime)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://examplebucket.s3.amazonaws.com/test.txt?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=AKIAIOSFODNN7EXAMPLE%2F20130524%2Fus-east-1%2Fs3%2Faws4_request&X-Amz-Date=20130524T000000Z&X-Amz-Expires=86400&X-Amz-SignedHeaders=host&X-Amz-Signature=aeeed9bbccd4d02ee5c0109b86d86835f995330da4c265957d157751f604d404"
	if got != want {
		t.Fatalf("Presign\n got %s\nwant %s", got, want)
	}
}

func TestPresignTTL(t *testing.T) {
	for _, tc := range []struct {
		ttl     time.Duration
		expires string
		ok      bool
	}{
		{ttl: time.Hour, expires: "X-Amz-Expires=3600&", ok: true},
		{ttl: MaxPresign, expires: "X-Amz-Expires=604800&", ok: true},
		{ttl: 1500 * time.Millisecond, expires: "X-Amz-Expires=2&", ok: true},
		{ttl: 0},
		{ttl: -time.Second},
		{ttl: MaxPresign + time.Second},
	} {
		got, err := Presign(http.MethodGet, "https://s3.example.sg/artifacts/o/r/1/dist.zip", exampleCred, "us-east-1", tc.ttl, exampleTime)
		if !tc.ok {
			if err == nil || !strings.Contains(err.Error(), "at most 7 days") {
				t.Errorf("ttl %s: url %q err %v, want an error", tc.ttl, got, err)
			}
			continue
		}
		if err != nil || !strings.Contains(got, tc.expires) {
			t.Errorf("ttl %s: url %q err %v, want %s", tc.ttl, got, err, tc.expires)
		}
	}
}

func TestCanonicalQuerySortsByKeyThenValue(t *testing.T) {
	q := map[string][]string{"a-b": {"1"}, "a": {"2", "1"}, "a.c": {"x"}, "a1": {""}}
	want := "a=1&a=2&a-b=1&a.c=x&a1="
	if got := canonicalQuery(q); got != want {
		t.Fatalf("canonicalQuery = %q, want %q", got, want)
	}
}

func TestPresignKeepsEncodedPath(t *testing.T) {
	got, err := Presign(http.MethodGet, "https://s3.example.sg/artifacts/o/r/1/my%20file%2Bv2.zip", exampleCred, "us-east-1", time.Hour, exampleTime)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "https://s3.example.sg/artifacts/o/r/1/my%20file%2Bv2.zip?X-Amz-Algorithm=") {
		t.Fatalf("Presign = %s", got)
	}
}
