package artifact

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseStore(t *testing.T) {
	good := `{"v":1,"endpoint":"https://s3.example.sg","bucket":"gha","region":"us-east-1","key_id":"JOB1"}`
	for _, tc := range []struct {
		name, in, wantErr string
	}{
		{"valid", good, ""},
		{"unknown fields are ignored", `{"v":1,"endpoint":"https://s3.example.sg:9000","bucket":"gha","region":"r","key_id":"K","extra":true}`, ""},
		{"empty", "  ", "the store is empty"},
		{"not JSON", "{", "the store is not valid JSON"},
		{"version", `{"v":2,"endpoint":"https://s3.example.sg","bucket":"gha","region":"r","key_id":"K"}`, "store version 2 is not supported"},
		{"no version", `{"endpoint":"https://s3.example.sg","bucket":"gha","region":"r","key_id":"K"}`, "store version 0 is not supported"},
		{"http", `{"v":1,"endpoint":"http://s3.example.sg","bucket":"gha","region":"r","key_id":"K"}`, `the store endpoint "http://s3.example.sg" is not an https:// URL`},
		{"no host", `{"v":1,"endpoint":"https://","bucket":"gha","region":"r","key_id":"K"}`, "is not an https:// URL"},
		{"no bucket", `{"v":1,"endpoint":"https://s3.example.sg","region":"r","key_id":"K"}`, "the store names no bucket"},
		{"no region", `{"v":1,"endpoint":"https://s3.example.sg","bucket":"gha","key_id":"K"}`, "the store names no region"},
		{"no key id", `{"v":1,"endpoint":"https://s3.example.sg","bucket":"gha","region":"r"}`, "the store names no key_id"},
	} {
		_, err := ParseStore(tc.in)
		if tc.wantErr == "" && err != nil {
			t.Errorf("%s: %v", tc.name, err)
		}
		if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
			t.Errorf("%s: error %v, want %q", tc.name, err, tc.wantErr)
		}
	}
}

func TestStoreJSONRoundTrip(t *testing.T) {
	s := Store{V: 1, Endpoint: "https://s3.example.sg", Bucket: "gha", Region: "us-east-1", KeyID: "JOB1"}
	want := `{"v":1,"endpoint":"https://s3.example.sg","bucket":"gha","region":"us-east-1","key_id":"JOB1"}`
	if got := s.JSON(); got != want {
		t.Fatalf("JSON %s, want %s", got, want)
	}
	back, err := ParseStore(s.JSON())
	if err != nil || back != s {
		t.Fatalf("round trip %+v %v", back, err)
	}
}

func TestParseKey(t *testing.T) {
	const secret = "pa:ss/word"
	if got := FormatKey("JOB1", secret); got != "JOB1:pa:ss/word" {
		t.Fatalf("FormatKey %q", got)
	}
	cred, err := ParseKey(" JOB1:" + secret + "\n")
	if err != nil || cred.AccessKeyID != "JOB1" || cred.SecretAccessKey != secret {
		t.Fatalf("ParseKey %+v %v", cred, err)
	}
	for _, in := range []string{"", "JOB1", ":" + secret, "JOB1:", "nocolon-secret"} {
		_, err := ParseKey(in)
		if err == nil {
			t.Errorf("%q: accepted", in)
			continue
		}
		if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "nocolon") {
			t.Errorf("%q: error repeats the input: %v", in, err)
		}
	}
}

func TestValidName(t *testing.T) {
	for _, ok := range []string{"dist", "my artifact", "build-1.2.3_linux", "日本", ".", "a.b"} {
		if err := ValidName(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, tc := range []struct{ name, label string }{
		{`a"b`, `Double quote "`},
		{"a:b", "Colon :"},
		{"a<b", "Less than <"},
		{"a>b", "Greater than >"},
		{"a|b", "Vertical bar |"},
		{"a*b", "Asterisk *"},
		{"a?b", "Question mark ?"},
		{"a\rb", `Carriage return \r`},
		{"a\nb", `Line feed \n`},
		{`a\b`, `Backslash \`},
		{"a/b", "Forward slash /"},
	} {
		err := ValidName(tc.name)
		if err == nil {
			t.Errorf("%q accepted", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), "Contains the following character: "+tc.label+".") {
			t.Errorf("%q: %v", tc.name, err)
		}
		if !strings.Contains(err.Error(), "Invalid characters include: Double quote \", Colon :, Less than <, Greater than >, Vertical bar |, Asterisk *, Question mark ?, Carriage return \\r, Line feed \\n, Backslash \\, Forward slash /.") {
			t.Errorf("%q: list missing: %v", tc.name, err)
		}
	}
	if err := ValidName(""); err == nil || err.Error() != "the artifact name is empty" {
		t.Errorf("empty: %v", err)
	}
}

func TestValidFilePath(t *testing.T) {
	for _, ok := range []string{"a.txt", "sub/b.txt", `odd\name`} {
		if err := validFilePath(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "a:b.txt", "sub/what?.txt", "x\ny"} {
		if err := validFilePath(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if err := validFilePath("a|b"); err == nil || !strings.Contains(err.Error(), "Contains the following character: Vertical bar |.") {
		t.Errorf("message %v", err)
	}
}

func TestObjectKeys(t *testing.T) {
	if got := RepoPrefix("Darkraise", "DarkMem"); got != "darkraise/darkmem/" {
		t.Errorf("RepoPrefix %q", got)
	}
	if got := RunPrefix("Darkraise", "DarkMem", 1932); got != "darkraise/darkmem/1932/" {
		t.Errorf("RunPrefix %q", got)
	}
	key := ObjectKey("Darkraise", "DarkMem", 1932, "Dist")
	if key != "darkraise/darkmem/1932/Dist.zip" {
		t.Fatalf("ObjectKey %q", key)
	}
	owner, repo, id, name, ok := ParseObjectKey(key)
	if !ok || owner != "darkraise" || repo != "darkmem" || id != 1932 || name != "Dist" {
		t.Fatalf("ParseObjectKey %q %q %d %q %v", owner, repo, id, name, ok)
	}
	for _, bad := range []string{
		"darkraise/darkmem/1932/dist", "darkraise/darkmem/1932/.zip", "darkraise/darkmem/x/dist.zip",
		"darkraise/darkmem/0/dist.zip", "darkraise/darkmem/-1/dist.zip", "darkraise/darkmem/+5/dist.zip",
		"darkraise/darkmem/05/dist.zip", "darkraise/darkmem/1932/sub/dist.zip", "darkraise/1932/dist.zip",
		"/darkmem/1932/dist.zip", "",
	} {
		if _, _, _, _, ok := ParseObjectKey(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestScopeKey(t *testing.T) {
	for _, tc := range []struct {
		key, prefix, wantErr string
	}{
		{"darkraise/darkmem/1/dist.zip", "darkraise/darkmem/", ""},
		{"darkraise/darkmem/1/dist.zip", "darkraise/", ""},
		{"darkraise/darkmem2/1/dist.zip", "darkraise/darkmem/", "is outside darkraise/darkmem/"},
		{"other/darkmem/1/dist.zip", "darkraise/", "is outside darkraise/"},
		{"Darkraise/darkmem/1/dist.zip", "darkraise/", "is outside"},
		{"darkraise/darkmem/../x/1/dist.zip", "darkraise/darkmem/", `has an empty, "." or ".." segment`},
		{"darkraise/darkmem/./1/dist.zip", "darkraise/darkmem/", "segment"},
		{"darkraise/darkmem//dist.zip", "darkraise/darkmem/", "segment"},
		{"darkraise/darkmem/", "darkraise/darkmem/", "segment"},
		{"darkraise/darkmem/1/", "darkraise/darkmem/", "segment"},
		{"darkraise/x", "darkraise", "must be a non-empty prefix ending in /"},
		{"x", "", "must be a non-empty prefix"},
		{"/x", "/", "must be a non-empty prefix"},
	} {
		err := ScopeKey(tc.key, tc.prefix)
		if tc.wantErr == "" && err != nil {
			t.Errorf("%q in %q: %v", tc.key, tc.prefix, err)
		}
		if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
			t.Errorf("%q in %q: error %v, want %q", tc.key, tc.prefix, err, tc.wantErr)
		}
	}
}

func TestMetaRoundTrip(t *testing.T) {
	exp := time.Date(2026, 10, 22, 8, 0, 0, 0, time.FixedZone("SGT", 8*3600))
	m := Meta{RunID: 1932, Attempt: 2, Workflow: "Build 🚀 release", Job: "build", SHA: "abc123", Ref: "main",
		Files: 12, Digest: "sha256:00ff", Expires: &exp, Uploader: "ghr-artifact/v0.2.0"}
	got := m.Map()
	for k, want := range map[string]string{
		"ghr-run-id": "1932", "ghr-run-attempt": "2", "ghr-job": "build", "ghr-sha": "abc123", "ghr-ref": "main",
		"ghr-files": "12", "ghr-digest": "sha256:00ff", "ghr-expires": "2026-10-22T00:00:00Z", "ghr-uploader": "ghr-artifact/v0.2.0",
	} {
		if got[k] != want {
			t.Errorf("%s = %q, want %q", k, got[k], want)
		}
	}
	for k, v := range got {
		for i := 0; i < len(v); i++ {
			if v[i] < 0x20 || v[i] >= 0x7f {
				t.Errorf("%s = %q is not plain ASCII", k, v)
				break
			}
		}
	}
	back := ParseMeta(got)
	if back.Expires == nil || !back.Expires.Equal(exp) {
		t.Fatalf("expires %v", back.Expires)
	}
	m.Expires, back.Expires = nil, nil
	if !reflect.DeepEqual(back, m) {
		t.Fatalf("round trip\n got %+v\nwant %+v", back, m)
	}

	noExp := Meta{RunID: 5, Attempt: 1, Files: 1}.Map()
	if _, ok := noExp["ghr-expires"]; ok {
		t.Errorf("ghr-expires written without Expires: %v", noExp)
	}
	if _, ok := noExp["ghr-workflow"]; ok {
		t.Errorf("empty workflow written: %v", noExp)
	}
}

func TestParseMetaMalformed(t *testing.T) {
	got := ParseMeta(map[string]string{
		"ghr-run-id": "x", "ghr-run-attempt": "-1", "ghr-files": "99999999999999999999",
		"ghr-expires": "tomorrow", "ghr-workflow": "=?bogus?=",
	})
	if got.RunID != 0 || got.Attempt != 0 || got.Files != 0 || got.Expires != nil {
		t.Fatalf("malformed fields not zero: %+v", got)
	}
	if got.Workflow != "=?bogus?=" {
		t.Fatalf("workflow %q", got.Workflow)
	}
	if got := ParseMeta(nil); !reflect.DeepEqual(got, Meta{}) {
		t.Fatalf("nil map %+v", got)
	}
}

func TestContentDisposition(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"dist", `attachment; filename="dist.zip"`},
		{"my build 1.2", `attachment; filename="my build 1.2.zip"`},
		{"résumé;x=1", `attachment; filename="r_sum_;x=1.zip"; filename*=UTF-8''r%C3%A9sum%C3%A9%3Bx%3D1.zip`},
	} {
		if got := ContentDisposition(tc.name); got != tc.want {
			t.Errorf("%q: %s, want %s", tc.name, got, tc.want)
		}
	}
}
