// Package offlinepack reads the installation data of the full package: the
// files that the first-run setup would otherwise download (the Debian image,
// the APT packages with their signed indexes, the tool releases), installed
// next to the app in offline\. Setup still installs everything; only the
// downloads are replaced. Claude Code is not part of it.
package offlinepack

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// Format is the layout this version reads.
const Format = 1

// DataEnv names a data directory to use instead of the one next to the app.
// For tests and for building the full package.
const DataEnv = "CAGE_OFFLINE_DATA"

// ManifestName is the manifest in the data directory.
const ManifestName = "manifest.json"

// GuestRoot is where the data is copied in the guest during setup. The guest
// scripts take its sources.list as the sign of an offline installation.
const GuestRoot = "/var/cache/cage-offline"

// Manifest lists the data. Every file except the image is copied into the
// guest under GuestRoot at its path.
type Manifest struct {
	Format  int    `json:"format"`
	Version string `json:"version"`
	Arch    string `json:"arch"`
	Image   Image  `json:"image"`
	Files   []File `json:"files"`
}

// Image is the Debian cloud image, checked against the digest pinned in the
// app's own template when it is put in Lima's cache.
type Image struct {
	Path   string `json:"path"`
	URL    string `json:"url"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

type File struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

var hexSHA256 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// validPath accepts relative slash paths without "." or ".." elements.
func validPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\:\x00\r\n") || path.Clean(p) != p {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}

func (m *Manifest) Validate() error {
	if m.Format != Format {
		return fmt.Errorf("unsupported offline data format %d", m.Format)
	}
	if m.Version == "" || (m.Arch != "amd64" && m.Arch != "arm64") {
		return errors.New("offline data manifest lacks its version or architecture")
	}
	if !validPath(m.Image.Path) || !strings.HasPrefix(m.Image.URL, "https://") || !strings.HasPrefix(m.Image.Digest, "sha512:") || m.Image.Size <= 0 {
		return errors.New("offline data manifest has an invalid image entry")
	}
	seen := map[string]bool{m.Image.Path: true}
	for _, f := range m.Files {
		if !validPath(f.Path) || !hexSHA256.MatchString(f.SHA256) || f.Size < 0 || seen[f.Path] {
			return fmt.Errorf("offline data manifest has an invalid entry %q", f.Path)
		}
		seen[f.Path] = true
	}
	return nil
}

// Load reads and validates dir's manifest.
func Load(dir string) (*Manifest, error) {
	f, err := os.Open(filepath.Join(dir, ManifestName))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var m Manifest
	dec := json.NewDecoder(io.LimitReader(f, 16<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("offline data manifest: %w", err)
	}
	return &m, m.Validate()
}

// Dir is the data directory for the app at exe: DataEnv when set, else
// offline\ in the installation folder (exe is resources\cage.exe).
func Dir(exe string) string {
	if d := os.Getenv(DataEnv); d != "" {
		return d
	}
	return filepath.Join(filepath.Dir(filepath.Dir(exe)), "offline")
}

// Find returns the data for this app version and architecture, or nil when
// there is none. Data for another version or architecture is not used: it was
// collected with other installation scripts.
func Find(exe, version, arch string) (string, *Manifest, error) {
	dir := Dir(exe)
	m, err := Load(dir)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil, nil
	}
	if err != nil {
		return dir, nil, err
	}
	if m.Version != version || m.Arch != arch {
		return "", nil, nil
	}
	return dir, m, nil
}

// TotalBytes is the size of the files copied into the guest.
func (m *Manifest) TotalBytes() int64 {
	var n int64
	for _, f := range m.Files {
		n += f.Size
	}
	return n
}

// CorruptError names the first file that does not match the manifest.
type CorruptError struct{ Path string }

func (e *CorruptError) Error() string {
	return "offline data file does not match its manifest: " + e.Path
}

// Verify checks every guest file's size and SHA-256. progress gets the bytes
// checked so far.
func Verify(dir string, m *Manifest, progress func(done, total int64)) error {
	total := m.TotalBytes()
	var done int64
	buf := make([]byte, 1<<20)
	for _, f := range m.Files {
		if err := verifyFile(filepath.Join(dir, filepath.FromSlash(f.Path)), f, buf, func(n int64) {
			done += n
			if progress != nil {
				progress(done, total)
			}
		}); err != nil {
			return err
		}
	}
	return nil
}

func verifyFile(p string, f File, buf []byte, read func(int64)) error {
	info, err := os.Lstat(p)
	if err != nil || !info.Mode().IsRegular() || info.Size() != f.Size {
		return &CorruptError{Path: f.Path}
	}
	in, err := os.Open(p)
	if err != nil {
		return &CorruptError{Path: f.Path}
	}
	defer in.Close()
	h := sha256.New()
	for {
		n, err := in.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
			read(int64(n))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return &CorruptError{Path: f.Path}
		}
	}
	if hex.EncodeToString(h.Sum(nil)) != f.SHA256 {
		return &CorruptError{Path: f.Path}
	}
	return nil
}
