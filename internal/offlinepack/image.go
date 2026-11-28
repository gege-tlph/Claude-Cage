package offlinepack

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LimaCacheDir is the cache Lima's downloader uses: CAGE_LIMA_CACHE_DIR,
// which the cage command sets to the environment storage, else the user's.
func LimaCacheDir() (string, error) {
	if dir := os.Getenv("CAGE_LIMA_CACHE_DIR"); dir != "" {
		return dir, nil
	}
	ucd, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(ucd, "lima"), nil
}

// ErrImageNotPinned: the data's image is not the one the app's template pins.
var ErrImageNotPinned = errors.New("the offline image is not the Debian image this version installs")

// SeedImage puts the data's image in Lima's download cache as the download of
// url, so that Lima finds it there instead of downloading it. It is checked
// against digest, the SHA-512 pinned in the app's template, while copied: the
// cache records only the digest, and Lima trusts that record. progress gets
// the bytes copied.
func SeedImage(cacheDir, dir string, m *Manifest, url, digest string, progress func(done, total int64)) error {
	if m.Image.URL != url || m.Image.Digest != digest {
		return ErrImageNotPinned
	}
	key := sha256.Sum256([]byte(url))
	shad := filepath.Join(cacheDir, "download", "by-url-sha256", hex.EncodeToString(key[:]))
	data := filepath.Join(shad, "data")
	digestFile := filepath.Join(shad, "sha512.digest")
	if recorded, err := os.ReadFile(digestFile); err == nil && strings.TrimSpace(string(recorded)) == digest {
		if info, err := os.Stat(data); err == nil && info.Mode().IsRegular() && info.Size() == m.Image.Size {
			return nil
		}
	}
	if err := os.MkdirAll(shad, 0o700); err != nil {
		return err
	}
	in, err := os.Open(filepath.Join(dir, filepath.FromSlash(m.Image.Path)))
	if err != nil {
		return &CorruptError{Path: m.Image.Path}
	}
	defer in.Close()
	tmp, err := os.CreateTemp(shad, ".data-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	h := sha512.New()
	var done int64
	buf := make([]byte, 1<<20)
	for {
		n, rerr := in.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
			if _, err := tmp.Write(buf[:n]); err != nil {
				tmp.Close()
				return err
			}
			done += int64(n)
			if progress != nil {
				progress(done, m.Image.Size)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			tmp.Close()
			return &CorruptError{Path: m.Image.Path}
		}
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if "sha512:"+hex.EncodeToString(h.Sum(nil)) != digest || done != m.Image.Size {
		return &CorruptError{Path: m.Image.Path}
	}
	// The digest record last: until it is there, Lima does not use the data.
	_ = os.Remove(digestFile)
	if err := os.Rename(tmp.Name(), data); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(shad, "url"), []byte(url), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(digestFile, []byte(digest), 0o644); err != nil {
		return fmt.Errorf("could not record the image digest: %w", err)
	}
	return nil
}
