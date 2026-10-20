package artifact

import (
	"archive/zip"
	"compress/flate"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

var ErrNoFiles = errors.New("no files matched")

type globPattern struct {
	negate bool
	segs   []string
	search []string
}

// splitPath splits an absolute path into "/"-separated segments; the first
// is the volume name, empty on Unix.
func splitPath(p string) []string {
	return strings.Split(filepath.ToSlash(p), "/")
}

func joinPath(segs []string) string {
	if len(segs) == 1 {
		return filepath.FromSlash(segs[0] + "/")
	}
	return filepath.FromSlash(strings.Join(segs, "/"))
}

func parseGlob(line, workspace string) (globPattern, bool, error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return globPattern{}, false, nil
	}
	var g globPattern
	if strings.HasPrefix(line, "!") {
		g.negate = true
		line = strings.TrimSpace(line[1:])
	}
	if line == "~" || strings.HasPrefix(line, "~/") || strings.HasPrefix(line, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return globPattern{}, false, fmt.Errorf("cannot expand ~ in %q: %w", line, err)
		}
		line = home + line[1:]
	}
	if !filepath.IsAbs(line) {
		line = filepath.Join(workspace, line)
	}
	g.segs = splitPath(filepath.Clean(line))
	i := 1
	for i < len(g.segs) && !strings.ContainsAny(g.segs[i], "*?[") {
		i++
	}
	g.search = g.segs[:i]
	if g.segs[len(g.segs)-1] != "**" {
		g.segs = append(g.segs, "**")
	}
	return g, true, nil
}

func matchSegs(pat, name []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			for i := 0; i <= len(name); i++ {
				if matchSegs(pat[1:], name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		p, n := pat[0], name[0]
		if runtime.GOOS == "windows" {
			p, n = strings.ToLower(p), strings.ToLower(n)
		}
		if ok, err := path.Match(p, n); err != nil || !ok {
			return false
		}
		pat, name = pat[1:], name[1:]
	}
	return len(name) == 0
}

// matches applies the patterns in order, as @actions/glob does: an include
// selects the path, a later "!" exclude drops it again.
func matches(globs []globPattern, file string) bool {
	segs := splitPath(file)
	in := false
	for _, g := range globs {
		if matchSegs(g.segs, segs) {
			in = !g.negate
		}
	}
	return in
}

func samePath(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] && (runtime.GOOS != "windows" || !strings.EqualFold(a[i], b[i])) {
			return false
		}
	}
	return true
}

// searchPaths keeps the include patterns' search paths, minus any that sit
// below another, in pattern order.
func searchPaths(globs []globPattern) [][]string {
	var out [][]string
	for _, g := range globs {
		if g.negate {
			continue
		}
		covered := false
		for _, o := range globs {
			if !o.negate && len(o.search) < len(g.search) && samePath(o.search, g.search[:len(o.search)]) {
				covered = true
				break
			}
		}
		for _, o := range out {
			if samePath(o, g.search) {
				covered = true
			}
		}
		if !covered {
			out = append(out, g.search)
		}
	}
	return out
}

func hidden(name string) bool { return strings.HasPrefix(name, ".") }

// walk follows symbolic links, skips broken ones and stops at a link back to
// one of its own ancestors.
func walk(p string, chain []os.FileInfo, includeHidden bool, visit func(string)) error {
	if !includeHidden && hidden(filepath.Base(p)) {
		return nil
	}
	info, err := os.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		visit(p)
		return nil
	}
	for _, a := range chain {
		if os.SameFile(a, info) {
			return nil
		}
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := walk(filepath.Join(p, e.Name()), append(chain, info), includeHidden, visit); err != nil {
			return err
		}
	}
	return nil
}

// Collect finds the files to upload the way actions/upload-artifact@v4 does.
func Collect(paths []string, includeHidden bool, workspace string) (root string, files []string, err error) {
	var globs []globPattern
	for _, line := range paths {
		g, ok, err := parseGlob(line, workspace)
		if err != nil {
			return "", nil, err
		}
		if ok {
			globs = append(globs, g)
		}
	}
	search := searchPaths(globs)
	if len(search) == 0 {
		return "", nil, ErrNoFiles
	}
	seen := map[string]bool{}
	var found []string
	for _, s := range search {
		err := walk(joinPath(s), nil, includeHidden, func(f string) {
			if !seen[f] && matches(globs, f) {
				seen[f] = true
				found = append(found, f)
			}
		})
		if err != nil {
			return "", nil, err
		}
	}
	if len(found) == 0 {
		return "", nil, ErrNoFiles
	}
	switch {
	case len(search) > 1:
		common := search[0]
		for _, s := range search[1:] {
			n := 0
			for n < len(common) && n < len(s) && common[n] == s[n] {
				n++
			}
			common = common[:n]
		}
		if len(common) == 0 {
			return "", nil, errors.New("the paths share no common directory")
		}
		root = joinPath(common)
	case len(found) == 1 && samePath(search[0], splitPath(found[0])):
		root = filepath.Dir(found[0])
	default:
		root = joinPath(search[0])
	}
	for _, f := range found {
		rel, err := filepath.Rel(root, f)
		if err != nil {
			return "", nil, err
		}
		rel = filepath.ToSlash(rel)
		if err := validFilePath(rel); err != nil {
			return "", nil, err
		}
		files = append(files, rel)
	}
	sort.Strings(files)
	return root, files, nil
}

func WriteZip(w io.Writer, root string, files []string, level int) error {
	if level < 0 || level > 9 {
		return fmt.Errorf("compression level %d is not between 0 and 9", level)
	}
	zw := zip.NewWriter(w)
	zw.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(out, level)
	})
	for _, rel := range files {
		if err := addFile(zw, root, rel, level); err != nil {
			return err
		}
	}
	return zw.Close()
}

func addFile(zw *zip.Writer, root, rel string, level int) error {
	f, err := os.Open(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	hdr, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	hdr.Name = rel
	hdr.Method = zip.Deflate
	if level == 0 {
		hdr.Method = zip.Store
	}
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}

func entryPath(f *zip.File) (string, error) {
	name := strings.TrimSuffix(f.Name, "/")
	if name == "" || strings.Contains(f.Name, `\`) || !filepath.IsLocal(filepath.FromSlash(name)) {
		return "", fmt.Errorf("the zip entry %q would be written outside the destination", f.Name)
	}
	if mode := f.Mode(); !mode.IsRegular() && !mode.IsDir() {
		return "", fmt.Errorf("the zip entry %q is not a regular file or directory", f.Name)
	}
	return filepath.FromSlash(name), nil
}

// Extract checks every entry before it writes any file.
func Extract(r io.ReaderAt, size int64, dest string) (files int, err error) {
	zr, err := zip.NewReader(r, size)
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		return 0, err
	}
	paths := make([]string, len(zr.File))
	for i, f := range zr.File {
		if paths[i], err = entryPath(f); err != nil {
			return 0, err
		}
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return 0, err
	}
	for i, f := range zr.File {
		target := filepath.Join(dest, paths[i])
		if f.Mode().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return files, err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return files, err
		}
		if err := extractFile(f, target); err != nil {
			return files, err
		}
		files++
	}
	return files, nil
}

func extractFile(f *zip.File, target string) error {
	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return fmt.Errorf("extracting %s: %w", f.Name, err)
	}
	return dst.Close()
}
