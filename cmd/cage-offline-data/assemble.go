package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4/v4"
	"github.com/ulikunitz/xz"

	"github.com/sylvinus/agent-vm/internal/offlinepack"
)

// The NodeSource key agent-vm.setup.sh installs with install_repository_key.
const nodeSourceKeyURL = "https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key"

const debianKeyring = "/usr/share/keyrings/debian-archive-keyring.gpg"

// release is one captured InRelease: its list-file prefix (the source URI as
// APT names list files) and suite, its fields and its index checksums.
type release struct {
	prefix, suite string
	file          string
	fields        map[string]string
	sums          map[string]indexSum
	indexes       []index
	repo          string
}

type indexSum struct {
	sha256 string
	size   int64
}

type index struct {
	file, path string // captured list file, path under dists/<suite>/
}

type debEntry struct {
	rel      *release
	filename string
	source   string
	version  string
}

// unescapeListName turns a piece of an APT list file name back into its
// path: '/' became '_', and '_' and other characters were %-quoted.
func unescapeListName(s string) (string, error) {
	return url.PathUnescape(strings.ReplaceAll(s, "_", "/"))
}

// parseRelease reads the fields of a clearsigned InRelease.
func parseRelease(data []byte) (map[string]string, map[string]indexSum, error) {
	text := string(data)
	if body, ok := strings.CutPrefix(text, "-----BEGIN PGP SIGNED MESSAGE-----\n"); ok {
		if _, rest, ok := strings.Cut(body, "\n\n"); ok {
			text, _, _ = strings.Cut(rest, "\n-----BEGIN PGP SIGNATURE-----")
		}
	}
	fields := map[string]string{}
	sums := map[string]indexSum{}
	key := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, " ") {
			if key == "SHA256" {
				f := strings.Fields(line)
				if len(f) == 3 {
					size, err := strconv.ParseInt(f[1], 10, 64)
					if err != nil {
						return nil, nil, err
					}
					sums[f[2]] = indexSum{sha256: f[0], size: size}
				}
			}
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = k
		fields[k] = strings.TrimSpace(v)
	}
	if fields["Suite"] == "" && fields["Codename"] == "" {
		return nil, nil, errors.New("InRelease without a suite")
	}
	return fields, sums, nil
}

func decompress(name string, data []byte) ([]byte, error) {
	var r io.Reader
	var err error
	switch {
	case strings.HasSuffix(name, ".gz"):
		r, err = gzip.NewReader(bytes.NewReader(data))
	case strings.HasSuffix(name, ".xz"):
		r, err = xz.NewReader(bytes.NewReader(data))
	case strings.HasSuffix(name, ".lz4"):
		r = lz4.NewReader(bytes.NewReader(data))
	case strings.HasSuffix(name, ".zst"):
		var d *zstd.Decoder
		d, err = zstd.NewReader(bytes.NewReader(data))
		if err == nil {
			defer d.Close()
			r = d
		}
	default:
		return data, nil
	}
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

// parsePackages maps each archive's SHA-256 to where the index places it.
func parsePackages(data []byte, rel *release, into map[string]debEntry) {
	for _, stanza := range strings.Split(string(data), "\n\n") {
		var sum, filename, pkg, source, ver string
		for _, line := range strings.Split(stanza, "\n") {
			k, v, ok := strings.Cut(line, ": ")
			if !ok {
				continue
			}
			switch k {
			case "SHA256":
				sum = v
			case "Filename":
				filename = v
			case "Package":
				pkg = v
			case "Source":
				source = v
			case "Version":
				ver = v
			}
		}
		if source == "" {
			source = pkg
		}
		if sum != "" && filename != "" {
			if _, seen := into[sum]; !seen {
				into[sum] = debEntry{rel: rel, filename: filename, source: source, version: ver}
			}
		}
	}
}

var slugChars = regexp.MustCompile(`[^a-z0-9]+`)

func repoName(rel *release, used map[string]bool) string {
	name := strings.Trim(slugChars.ReplaceAllString(strings.ToLower(rel.fields["Label"]), "-"), "-")
	if name == "" {
		name = "repo"
	}
	for i := 2; used[name]; i++ {
		name = fmt.Sprintf("%s-%d", strings.TrimRight(name, "-0123456789"), i)
	}
	used[name] = true
	return name
}

func fileSHA256(p string) (string, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}

func untar(file, dir string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	r := tar.NewReader(bufio.NewReader(f))
	for {
		h, err := r.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := path.Clean(strings.TrimPrefix(h.Name, "./"))
		if name == "." || strings.HasPrefix(name, "../") || strings.HasPrefix(name, "/") {
			continue
		}
		target := filepath.Join(dir, filepath.FromSlash(name))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.Create(target)
			if err != nil {
				return err
			}
			_, err = io.Copy(out, r)
			if cerr := out.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		}
	}
}

func readToolSums(file, arch string) (map[string]string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	want := map[string]string{}
	suffix := "_" + strings.ToUpper(arch) + "_SHA256="
	for _, line := range strings.Split(string(data), "\n") {
		for _, tool := range []string{"CAGE_CODE_SERVER", "CAGE_MISE", "CAGE_GH"} {
			if v, ok := strings.CutPrefix(strings.TrimSpace(line), tool+suffix); ok {
				want[tool] = v
			}
		}
	}
	if len(want) != 3 {
		return nil, fmt.Errorf("%s lacks the %s tool checksums", file, arch)
	}
	return want, nil
}

func copyFile(from, to string) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	if err := os.Link(from, to); err == nil {
		return nil
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

func assemble(collected, imageEntry, version, toolVersions, out string, log io.Writer) error {
	if entries, err := os.ReadDir(out); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s is not empty", out)
	}
	work := out + ".collected"
	if err := os.RemoveAll(work); err != nil {
		return err
	}
	if err := untar(collected, work); err != nil {
		return fmt.Errorf("reading %s: %w", collected, err)
	}
	defer os.RemoveAll(work)
	archData, err := os.ReadFile(filepath.Join(work, "arch"))
	if err != nil {
		return err
	}
	arch := strings.TrimSpace(string(archData))

	// The signed indexes.
	lists, err := os.ReadDir(filepath.Join(work, "lists"))
	if err != nil {
		return err
	}
	var releases []*release
	for _, e := range lists {
		name := e.Name()
		if !strings.HasSuffix(name, "_InRelease") {
			continue
		}
		i := strings.LastIndex(name, "_dists_")
		if i < 0 {
			return fmt.Errorf("unexpected list file %s", name)
		}
		suite, err := unescapeListName(strings.TrimSuffix(name[i+len("_dists_"):], "_InRelease"))
		if err != nil {
			return err
		}
		data, err := os.ReadFile(filepath.Join(work, "lists", name))
		if err != nil {
			return err
		}
		fields, sums, err := parseRelease(data)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		releases = append(releases, &release{prefix: name[:i], suite: suite, file: name, fields: fields, sums: sums})
	}
	debs := map[string]debEntry{}
	for _, e := range lists {
		name := e.Name()
		if !strings.Contains(name, "_Packages") {
			continue
		}
		var rel *release
		for _, r := range releases {
			head := r.prefix + "_dists_" + strings.ReplaceAll(url.PathEscape(r.suite), "/", "_") + "_"
			if strings.HasPrefix(name, head) && (rel == nil || len(head) > len(r.prefix+"_dists_"+rel.suite+"_")) {
				rel = r
			}
		}
		if rel == nil {
			return fmt.Errorf("%s has no captured InRelease", name)
		}
		head := rel.prefix + "_dists_" + strings.ReplaceAll(url.PathEscape(rel.suite), "/", "_") + "_"
		indexPath, err := unescapeListName(strings.TrimPrefix(name, head))
		if err != nil {
			return err
		}
		sum, size, err := fileSHA256(filepath.Join(work, "lists", name))
		if err != nil {
			return err
		}
		if want, ok := rel.sums[indexPath]; !ok || want.sha256 != sum || want.size != size {
			return fmt.Errorf("%s does not match its InRelease (%s)", name, indexPath)
		}
		data, err := os.ReadFile(filepath.Join(work, "lists", name))
		if err != nil {
			return err
		}
		plain, err := decompress(name, data)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		parsePackages(plain, rel, debs)
		rel.indexes = append(rel.indexes, index{file: name, path: indexPath})
	}

	// One local repository per source: the suites of one archive share its pool.
	used := map[string]bool{}
	byPrefix := map[string]string{}
	sort.Slice(releases, func(i, j int) bool { return releases[i].file < releases[j].file })
	for _, r := range releases {
		if len(r.indexes) == 0 {
			continue
		}
		if name, ok := byPrefix[r.prefix]; ok {
			r.repo = name
			continue
		}
		r.repo = repoName(r, used)
		byPrefix[r.prefix] = r.repo
	}

	// The archives, at the paths the indexes give them.
	archives, err := os.ReadDir(filepath.Join(work, "archives"))
	if err != nil {
		return err
	}
	var missing []string
	var packages []string
	var aptBytes int64
	for _, e := range archives {
		p := filepath.Join(work, "archives", e.Name())
		sum, size, err := fileSHA256(p)
		if err != nil {
			return err
		}
		entry, ok := debs[sum]
		if !ok || entry.rel.repo == "" {
			missing = append(missing, e.Name())
			continue
		}
		if err := copyFile(p, filepath.Join(out, "apt", entry.rel.repo, filepath.FromSlash(entry.filename))); err != nil {
			return err
		}
		aptBytes += size
		packages = append(packages, entry.source+"\t"+entry.version+"\t"+entry.rel.fields["Origin"])
	}
	if len(missing) > 0 {
		return fmt.Errorf("no captured index lists %d archives: %s", len(missing), strings.Join(missing, ", "))
	}

	// Indexes and sources.
	keyFile := filepath.Join(work, "keys", "nodesource.asc")
	var sources, security strings.Builder
	for _, r := range releases {
		if r.repo == "" {
			continue
		}
		if err := copyFile(filepath.Join(work, "lists", r.file), filepath.Join(out, "apt", r.repo, "dists", filepath.FromSlash(r.suite), "InRelease")); err != nil {
			return err
		}
		components := map[string]bool{}
		for _, ix := range r.indexes {
			if err := copyFile(filepath.Join(work, "lists", ix.file), filepath.Join(out, "apt", r.repo, "dists", filepath.FromSlash(r.suite), filepath.FromSlash(ix.path))); err != nil {
				return err
			}
			components[strings.Split(ix.path, "/")[0]] = true
		}
		keyring := debianKeyring
		if r.fields["Origin"] != "Debian" {
			if !strings.Contains(strings.ToLower(r.prefix), "nodesource") {
				return fmt.Errorf("no keyring known for %s (%s)", r.file, r.fields["Origin"])
			}
			if _, err := os.Stat(keyFile); err != nil {
				return errors.New("the NodeSource key was not collected")
			}
			keyring = offlinepack.GuestRoot + "/apt/keys/nodesource.asc"
		}
		var names []string
		for c := range components {
			names = append(names, c)
		}
		sort.Strings(names)
		line := fmt.Sprintf("deb [signed-by=%s check-valid-until=no check-date=no by-hash=no] file:%s/apt/%s %s %s\n", keyring, offlinepack.GuestRoot, r.repo, r.suite, strings.Join(names, " "))
		sources.WriteString(line)
		if r.fields["Label"] == "Debian-Security" {
			security.WriteString(line)
		}
	}
	if security.Len() == 0 {
		return errors.New("the Debian security repository was not captured")
	}
	if err := os.WriteFile(filepath.Join(out, "apt", "sources.list"), []byte(sources.String()), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, "apt", "security.list"), []byte(security.String()), 0o644); err != nil {
		return err
	}
	if _, err := os.Stat(keyFile); err == nil {
		if err := copyFile(keyFile, filepath.Join(out, "apt", "keys", "nodesource.asc")); err != nil {
			return err
		}
		urlSum := sha256.Sum256([]byte(nodeSourceKeyURL))
		if err := copyFile(keyFile, filepath.Join(out, "files", hex.EncodeToString(urlSum[:]))); err != nil {
			return err
		}
	}

	// The tool releases, checked against their pinned checksums.
	want, err := readToolSums(toolVersions, arch)
	if err != nil {
		return err
	}
	found := map[string]bool{}
	toolDirs, _ := os.ReadDir(filepath.Join(work, "tools"))
	for _, d := range toolDirs {
		files, err := os.ReadDir(filepath.Join(work, "tools", d.Name()))
		if err != nil {
			return err
		}
		for _, f := range files {
			p := filepath.Join(work, "tools", d.Name(), f.Name())
			sum, _, err := fileSHA256(p)
			if err != nil {
				return err
			}
			if sum != d.Name() {
				return fmt.Errorf("tool release %s/%s does not match its checksum", d.Name(), f.Name())
			}
			found[sum] = true
			if err := copyFile(p, filepath.Join(out, "tools", d.Name(), f.Name())); err != nil {
				return err
			}
		}
	}
	for tool, sum := range want {
		if !found[sum] {
			return fmt.Errorf("the %s release (%s) was not collected", tool, sum)
		}
	}
	if schemas := filepath.Join(work, "schemas", "code-server-machine-settings.json"); fileExists(schemas) {
		if err := copyFile(schemas, filepath.Join(out, "schemas", "code-server-machine-settings.json")); err != nil {
			return err
		}
	}

	// The image, checked against the digest Lima recorded for its download.
	imageURL, err := os.ReadFile(filepath.Join(imageEntry, "url"))
	if err != nil {
		return err
	}
	digest, err := os.ReadFile(filepath.Join(imageEntry, "sha512.digest"))
	if err != nil {
		return err
	}
	image := offlinepack.Image{URL: strings.TrimSpace(string(imageURL)), Digest: strings.TrimSpace(string(digest))}
	image.Path = "image/" + path.Base(image.URL)
	if err := copyFile(filepath.Join(imageEntry, "data"), filepath.Join(out, filepath.FromSlash(image.Path))); err != nil {
		return err
	}
	h := sha512.New()
	f, err := os.Open(filepath.Join(out, filepath.FromSlash(image.Path)))
	if err != nil {
		return err
	}
	image.Size, err = io.Copy(h, f)
	f.Close()
	if err != nil {
		return err
	}
	if "sha512:"+hex.EncodeToString(h.Sum(nil)) != image.Digest {
		return errors.New("the cached Debian image does not match its recorded digest")
	}

	// The manifest.
	m := offlinepack.Manifest{Format: offlinepack.Format, Version: version, Arch: arch, Image: image}
	err = filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(out, p)
		rel = filepath.ToSlash(rel)
		if rel == offlinepack.ManifestName || rel == image.Path {
			return nil
		}
		sum, size, err := fileSHA256(p)
		if err != nil {
			return err
		}
		m.Files = append(m.Files, offlinepack.File{Path: rel, Size: size, SHA256: sum})
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(m.Files, func(i, j int) bool { return m.Files[i].Path < m.Files[j].Path })
	if err := m.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, offlinepack.ManifestName), append(data, '\n'), 0o644); err != nil {
		return err
	}
	// The binary packages' sources, for the release's source notice.
	sort.Strings(packages)
	packages = compact(packages)
	if err := os.WriteFile(out+".packages.tsv", []byte("source\tversion\torigin\n"+strings.Join(packages, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(log, "%d archives (%d MB), %d files, image %d MB, guest files %d MB\n", len(archives), aptBytes>>20, len(m.Files), image.Size>>20, m.TotalBytes()>>20)
	return nil
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular()
}

func compact(s []string) []string {
	var out []string
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}
