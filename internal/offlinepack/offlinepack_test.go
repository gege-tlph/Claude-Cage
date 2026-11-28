package offlinepack

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testURL = "https://cloud.debian.org/images/cloud/trixie/x/debian.qcow2"

// writeData makes a data directory with the given guest files and an image.
func writeData(t *testing.T, files map[string]string, image string) (string, *Manifest) {
	t.Helper()
	dir := t.TempDir()
	sum := sha512.Sum512([]byte(image))
	m := &Manifest{Format: Format, Version: "1.2.3", Arch: "amd64",
		Image: Image{Path: "image/debian.qcow2", URL: testURL, Digest: "sha512:" + hex.EncodeToString(sum[:]), Size: int64(len(image))}}
	write := func(p, content string) {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(m.Image.Path, image)
	for _, p := range []string{"apt/sources.list", "apt/debian/pool/main/a/a.deb", "tools/abc/gh.deb"} {
		content, ok := files[p]
		if !ok {
			continue
		}
		write(p, content)
		h := sha256.Sum256([]byte(content))
		m.Files = append(m.Files, File{Path: p, Size: int64(len(content)), SHA256: hex.EncodeToString(h[:])})
	}
	data, _ := json.Marshal(m)
	write(ManifestName, string(data))
	return dir, m
}

var sample = map[string]string{"apt/sources.list": "deb file:/x trixie main\n", "apt/debian/pool/main/a/a.deb": "deb-bytes", "tools/abc/gh.deb": "gh"}

func TestFindUsesOnlyThisVersionsData(t *testing.T) {
	dir, _ := writeData(t, sample, "image")
	t.Setenv(DataEnv, dir)
	if got, m, err := Find("ignored", "1.2.3", "amd64"); err != nil || m == nil || got != dir {
		t.Fatalf("Find = %q, %v, %v", got, m, err)
	}
	for _, c := range [][2]string{{"1.2.4", "amd64"}, {"1.2.3", "arm64"}} {
		if _, m, err := Find("ignored", c[0], c[1]); err != nil || m != nil {
			t.Fatalf("Find(%v) = %v, %v; data of another build must be ignored", c, m, err)
		}
	}
	t.Setenv(DataEnv, t.TempDir())
	if _, m, err := Find("ignored", "1.2.3", "amd64"); err != nil || m != nil {
		t.Fatalf("no data: %v, %v", m, err)
	}
}

func TestDirIsNextToTheInstallation(t *testing.T) {
	t.Setenv(DataEnv, "")
	exe := filepath.Join("C:", "Programs", "Cage", "resources", "cage.exe")
	if got, want := Dir(exe), filepath.Join("C:", "Programs", "Cage", "offline"); got != want {
		t.Fatalf("Dir = %q, want %q", got, want)
	}
}

func TestValidateRejectsEscapingPaths(t *testing.T) {
	for _, p := range []string{"../x", "/etc/passwd", "a/../../b", `a\b`, "C:/x", "a/./b", ""} {
		m := Manifest{Format: Format, Version: "1", Arch: "amd64", Image: Image{Path: "i", URL: testURL, Digest: "sha512:x", Size: 1},
			Files: []File{{Path: p, SHA256: strings.Repeat("a", 64)}}}
		if m.Validate() == nil {
			t.Errorf("path %q accepted", p)
		}
	}
}

func TestVerifyNamesTheChangedFile(t *testing.T) {
	dir, m := writeData(t, sample, "image")
	var last int64
	if err := Verify(dir, m, func(done, total int64) { last = done }); err != nil || last != m.TotalBytes() {
		t.Fatalf("Verify = %v, progress %d of %d", err, last, m.TotalBytes())
	}
	if err := os.WriteFile(filepath.Join(dir, "tools", "abc", "gh.deb"), []byte("GH"), 0o644); err != nil {
		t.Fatal(err)
	}
	var corrupt *CorruptError
	if err := Verify(dir, m, nil); !errors.As(err, &corrupt) || corrupt.Path != "tools/abc/gh.deb" {
		t.Fatalf("Verify after a change = %v", err)
	}
}

func TestWriteTarHoldsTheGuestFilesOnly(t *testing.T) {
	dir, m := writeData(t, sample, "image")
	var buf bytes.Buffer
	if err := WriteTar(&buf, dir, m, nil); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	r := tar.NewReader(&buf)
	for {
		h, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			data, _ := io.ReadAll(r)
			got[h.Name] = string(data)
		} else if h.Typeflag != tar.TypeDir {
			t.Fatalf("unexpected entry %q", h.Name)
		}
	}
	if len(got) != len(sample) {
		t.Fatalf("tar has %v", got)
	}
	for p, content := range sample {
		if got[p] != content {
			t.Fatalf("%s = %q", p, got[p])
		}
	}
}

func TestWriteTarRefusesAFileThatShrank(t *testing.T) {
	dir, m := writeData(t, sample, "image")
	if err := os.WriteFile(filepath.Join(dir, "apt", "sources.list"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteTar(io.Discard, dir, m, nil); err == nil {
		t.Fatal("a file shorter than its manifest entry was streamed")
	}
}

func TestSeedImagePutsThePinnedImageInLimasCache(t *testing.T) {
	dir, m := writeData(t, sample, "the image")
	cache := t.TempDir()
	if err := SeedImage(cache, dir, m, testURL, "sha512:"+strings.Repeat("0", 128), nil); !errors.Is(err, ErrImageNotPinned) {
		t.Fatalf("another digest: %v", err)
	}
	if err := SeedImage(cache, dir, m, testURL, m.Image.Digest, nil); err != nil {
		t.Fatal(err)
	}
	key := sha256.Sum256([]byte(testURL))
	shad := filepath.Join(cache, "download", "by-url-sha256", hex.EncodeToString(key[:]))
	if data, _ := os.ReadFile(filepath.Join(shad, "data")); string(data) != "the image" {
		t.Fatalf("data = %q", data)
	}
	if d, _ := os.ReadFile(filepath.Join(shad, "sha512.digest")); string(d) != m.Image.Digest {
		t.Fatalf("digest = %q", d)
	}
	if u, _ := os.ReadFile(filepath.Join(shad, "url")); string(u) != testURL {
		t.Fatalf("url = %q", u)
	}
}

func TestSeedImageRefusesAChangedImage(t *testing.T) {
	dir, m := writeData(t, sample, "the image")
	if err := os.WriteFile(filepath.Join(dir, "image", "debian.qcow2"), []byte("the imagE"), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := t.TempDir()
	var corrupt *CorruptError
	if err := SeedImage(cache, dir, m, testURL, m.Image.Digest, nil); !errors.As(err, &corrupt) {
		t.Fatalf("changed image: %v", err)
	}
	key := sha256.Sum256([]byte(testURL))
	if _, err := os.Stat(filepath.Join(cache, "download", "by-url-sha256", hex.EncodeToString(key[:]), "sha512.digest")); err == nil {
		t.Fatal("a changed image was recorded in the cache")
	}
}

func TestBootScriptWaitsForTheFinishedInstall(t *testing.T) {
	got := BootScript("#!/bin/sh\necho prepare\n")
	if !strings.HasPrefix(got, "#!/bin/sh\nif [ ! -e "+FinishedMarker+" ]; then\n") || !strings.HasSuffix(got, "fi\necho prepare\n") {
		t.Fatalf("BootScript = %q", got)
	}
	if !strings.Contains(GuestFinishScript, FinishedMarker) {
		t.Fatal("guest-finish.sh does not write the marker BootScript waits for")
	}
}

func TestGuestScriptsUseOneRoot(t *testing.T) {
	for name, script := range map[string]string{"prepare": GuestPrepareScript, "finish": GuestFinishScript, "fetch": FetchFunction} {
		if !strings.Contains(script, "root="+GuestRoot+"\n") && !strings.Contains(script, "root="+GuestRoot+" ") {
			t.Errorf("%s script does not use %s", name, GuestRoot)
		}
	}
}
