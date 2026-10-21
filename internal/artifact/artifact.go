// Package artifact holds the rules ghr and ghr-artifact share for artifacts
// kept on an S3 store: the store and key formats, names, object keys and the
// metadata written on each object.
package artifact

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/endpoint"
	"github.com/darkraise/ghr/internal/s3"
)

const (
	VariableName     = "GHR_ARTIFACT_STORE"
	SecretName       = "GHR_ARTIFACT_KEY"
	MaxRetentionDays = 90
)

// Store is the JSON value of the GHR_ARTIFACT_STORE repository variable.
type Store struct {
	V        int    `json:"v"`
	Endpoint string `json:"endpoint"`
	Bucket   string `json:"bucket"`
	Region   string `json:"region"`
	KeyID    string `json:"key_id"`
	// KeySetAt is when the job key in GHR_ARTIFACT_KEY was set, so a key
	// replaced under the same ID shows in the variable.
	KeySetAt time.Time `json:"key_set_at,omitzero"`
}

func ParseStore(s string) (Store, error) {
	if strings.TrimSpace(s) == "" {
		return Store{}, errors.New("the store is empty")
	}
	var st Store
	if err := json.Unmarshal([]byte(s), &st); err != nil {
		return Store{}, fmt.Errorf("the store is not valid JSON: %w", err)
	}
	if st.V != 1 {
		return Store{}, fmt.Errorf("store version %d is not supported; use a newer ghr-artifact", st.V)
	}
	if u, err := url.Parse(st.Endpoint); err != nil || u.Scheme != "https" || u.Host == "" {
		return Store{}, fmt.Errorf("the store endpoint %q is not an https:// URL", st.Endpoint)
	}
	switch {
	case st.Bucket == "":
		return Store{}, errors.New("the store names no bucket")
	case st.Region == "":
		return Store{}, errors.New("the store names no region")
	case st.KeyID == "":
		return Store{}, errors.New("the store names no key_id")
	}
	return st, nil
}

func (s Store) JSON() string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

const SettingsVersion = 1

// Settings is ghr's artifact store settings file, which the daemon writes
// into an eligible runner's work folder and ghr-artifact reads.
type Settings struct {
	V        int    `json:"v"`
	Endpoint string `json:"endpoint"`
	Bucket   string `json:"bucket"`
	Region   string `json:"region"`
	KeyID    string `json:"key_id"`
	Secret   string `json:"secret"`
}

func (s Settings) JSON() []byte {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return b
}

// ParseSettings never puts any part of b in its error: b holds a secret.
func ParseSettings(b []byte) (Settings, error) {
	var s Settings
	// json.Unmarshal's errors can quote the input, so its text is dropped.
	if err := json.Unmarshal(b, &s); err != nil {
		return Settings{}, errors.New("the settings are not valid JSON")
	}
	if s.V != SettingsVersion {
		return Settings{}, fmt.Errorf("settings version %d is not supported; use a newer ghr-artifact", s.V)
	}
	if err := endpoint.Check(s.Endpoint); err != nil {
		return Settings{}, errors.New("the endpoint is refused: " + err.Error())
	}
	switch {
	case s.Bucket == "":
		return Settings{}, errors.New("the settings name no bucket")
	case s.Region == "":
		return Settings{}, errors.New("the settings name no region")
	case s.KeyID == "":
		return Settings{}, errors.New("the settings name no key_id")
	case s.Secret == "":
		return Settings{}, errors.New("the settings name no secret")
	}
	return s, nil
}

func FormatKey(id, secret string) string { return id + ":" + secret }

// ParseKey never puts any part of s in its error: s holds a secret.
func ParseKey(s string) (s3.Credentials, error) {
	id, secret, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok || id == "" || secret == "" {
		return s3.Credentials{}, errors.New("the key is not in the form <access key id>:<secret>")
	}
	return s3.Credentials{AccessKeyID: id, SecretAccessKey: secret}, nil
}

type invalidChar struct{ char, label string }

// The two lists and their wording follow actions/toolkit's
// path-and-artifact-name-validation.ts, which actions/upload-artifact@v4 applies.
var invalidPathChars = []invalidChar{
	{`"`, `Double quote "`},
	{":", "Colon :"},
	{"<", "Less than <"},
	{">", "Greater than >"},
	{"|", "Vertical bar |"},
	{"*", "Asterisk *"},
	{"?", "Question mark ?"},
	{"\r", `Carriage return \r`},
	{"\n", `Line feed \n`},
}

var invalidNameChars = append(append([]invalidChar{}, invalidPathChars...),
	invalidChar{`\`, `Backslash \`},
	invalidChar{"/", "Forward slash /"},
)

func labels(chars []invalidChar) string {
	out := make([]string, len(chars))
	for i, c := range chars {
		out[i] = c.label
	}
	return strings.Join(out, ", ")
}

func ValidName(name string) error {
	if name == "" {
		return errors.New("the artifact name is empty")
	}
	for _, c := range invalidNameChars {
		if strings.Contains(name, c.char) {
			return fmt.Errorf("the artifact name is not valid: %q. Contains the following character: %s. "+
				"Invalid characters include: %s. These characters are not allowed in the artifact name due to "+
				"limitations with certain file systems such as NTFS. To maintain file system agnostic behavior, "+
				"these characters are intentionally not allowed to prevent potential problems with downloads on "+
				"different file systems", name, c.label, labels(invalidNameChars))
		}
	}
	return nil
}

// validFilePath checks one file's path inside an artifact, "/"-separated.
func validFilePath(p string) error {
	if p == "" {
		return errors.New("the path of a file in the artifact is empty")
	}
	for _, c := range invalidPathChars {
		if strings.Contains(p, c.char) {
			return fmt.Errorf("the path for one of the files in the artifact is not valid: %q. Contains the following "+
				"character: %s. Invalid characters include: %s. These characters are not allowed in files that are "+
				"uploaded due to limitations with certain file systems such as NTFS. To maintain file system agnostic "+
				"behavior, these characters are intentionally not allowed to prevent potential problems with downloads "+
				"on different file systems", p, c.label, labels(invalidPathChars))
		}
	}
	return nil
}

func RepoPrefix(owner, repo string) string {
	return strings.ToLower(owner) + "/" + strings.ToLower(repo) + "/"
}

func RunPrefix(owner, repo string, runID int64) string {
	return RepoPrefix(owner, repo) + strconv.FormatInt(runID, 10) + "/"
}

func ObjectKey(owner, repo string, runID int64, name string) string {
	return RunPrefix(owner, repo, runID) + name + ".zip"
}

func ParseObjectKey(key string) (owner, repo string, runID int64, name string, ok bool) {
	parts := strings.Split(key, "/")
	if len(parts) != 4 || parts[0] == "" || parts[1] == "" {
		return "", "", 0, "", false
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != parts[2] {
		return "", "", 0, "", false
	}
	name, found := strings.CutSuffix(parts[3], ".zip")
	if !found || name == "" {
		return "", "", 0, "", false
	}
	return parts[0], parts[1], id, name, true
}

func ScopeKey(key, prefix string) error {
	if !strings.HasSuffix(prefix, "/") || strings.Trim(prefix, "/") == "" {
		return fmt.Errorf("the scope %q must be a non-empty prefix ending in /", prefix)
	}
	if !strings.HasPrefix(key, prefix) {
		return fmt.Errorf("object key %q is outside %s", key, prefix)
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf(`object key %q has an empty, "." or ".." segment`, key)
		}
	}
	return nil
}

// Meta is the user metadata ghr-artifact writes on each object.
type Meta struct {
	RunID    int64
	Attempt  int
	Workflow string
	Job      string
	SHA      string
	Ref      string
	Files    int
	Digest   string
	Expires  *time.Time
	Uploader string
}

// Map encodes text values as RFC 2047 words when they are not plain ASCII:
// S3 user metadata travels in HTTP headers and is signed as ASCII.
func (m Meta) Map() map[string]string {
	out := map[string]string{
		"ghr-run-id":      strconv.FormatInt(m.RunID, 10),
		"ghr-run-attempt": strconv.Itoa(m.Attempt),
		"ghr-files":       strconv.Itoa(m.Files),
	}
	for k, v := range map[string]string{
		"ghr-workflow": m.Workflow,
		"ghr-job":      m.Job,
		"ghr-sha":      m.SHA,
		"ghr-ref":      m.Ref,
		"ghr-digest":   m.Digest,
		"ghr-uploader": m.Uploader,
	} {
		if v != "" {
			out[k] = mime.QEncoding.Encode("utf-8", v)
		}
	}
	if m.Expires != nil {
		out["ghr-expires"] = m.Expires.UTC().Format(time.RFC3339)
	}
	return out
}

func ParseMeta(m map[string]string) Meta {
	dec := new(mime.WordDecoder)
	text := func(k string) string {
		v := m[k]
		if d, err := dec.DecodeHeader(v); err == nil {
			return d
		}
		return v
	}
	count := func(k string) int64 {
		n, err := strconv.ParseInt(m[k], 10, 64)
		if err != nil || n < 0 {
			return 0
		}
		return n
	}
	out := Meta{
		RunID:    count("ghr-run-id"),
		Attempt:  int(count("ghr-run-attempt")),
		Workflow: text("ghr-workflow"),
		Job:      text("ghr-job"),
		SHA:      text("ghr-sha"),
		Ref:      text("ghr-ref"),
		Files:    int(count("ghr-files")),
		Digest:   text("ghr-digest"),
		Uploader: text("ghr-uploader"),
	}
	if t, err := time.Parse(time.RFC3339, m["ghr-expires"]); err == nil {
		t = t.UTC()
		out.Expires = &t
	}
	return out
}

// ContentDisposition adds an RFC 6266 filename* for names that are not plain
// ASCII, with "_" in their place in the plain filename.
func ContentDisposition(name string) string {
	file := name + ".zip"
	plain := true
	for i := 0; i < len(file); i++ {
		if c := file[i]; c < 0x20 || c >= 0x7f {
			plain = false
			break
		}
	}
	if plain {
		return `attachment; filename="` + file + `"`
	}
	fallback := strings.Map(func(r rune) rune {
		if r < 0x20 || r >= 0x7f {
			return '_'
		}
		return r
	}, file)
	var enc strings.Builder
	for i := 0; i < len(file); i++ {
		c := file[i]
		if 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || strings.IndexByte("!#$&+-.^_`|~", c) >= 0 {
			enc.WriteByte(c)
		} else {
			fmt.Fprintf(&enc, "%%%02X", c)
		}
	}
	return `attachment; filename="` + fallback + `"; filename*=UTF-8''` + enc.String()
}
