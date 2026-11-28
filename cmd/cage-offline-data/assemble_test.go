package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sylvinus/agent-vm/internal/offlinepack"
)

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func inRelease(label, suite string, indexes map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "-----BEGIN PGP SIGNED MESSAGE-----\nHash: SHA512\n\nOrigin: Debian\nLabel: %s\nSuite: %s\nCodename: %s\nSHA256:\n", label, suite, suite)
	for p, content := range indexes {
		fmt.Fprintf(&b, " %s %d %s\n", sum(content), len(content), p)
	}
	b.WriteString("-----BEGIN PGP SIGNATURE-----\n\nx\n-----END PGP SIGNATURE-----\n")
	return b.String()
}

// collectedTar is what collect.sh writes for one Debian suite, the security
// suite and the three tool releases.
func collectedTar(t *testing.T, deb string, extraDeb string) (string, map[string]string) {
	main := fmt.Sprintf("Package: a\nSource: a-src\nVersion: 1\nFilename: pool/main/a/a_1_amd64.deb\nSHA256: %s\n\nPackage: b\nVersion: 2\nFilename: pool/main/b/b_2_amd64.deb\nSHA256: %s\n", sum(deb), sum("other"))
	security := "Package: apt\nVersion: 3.0.4\nFilename: pool/updates/main/a/apt/apt_3.0.4_amd64.deb\nSHA256: " + sum("apt") + "\n"
	tools := map[string]string{"gh.deb": "gh", "mise.tar.gz": "mise", "code-server.deb": "cs"}
	files := map[string]string{
		"arch": "amd64\n",
		"lists/deb.debian.org_debian_dists_trixie_InRelease":                                    inRelease("Debian", "trixie", map[string]string{"main/binary-amd64/Packages": main}),
		"lists/deb.debian.org_debian_dists_trixie_main_binary-amd64_Packages":                   main,
		"lists/deb.debian.org_debian-security_dists_trixie-security_InRelease":                  inRelease("Debian-Security", "trixie-security", map[string]string{"main/binary-amd64/Packages": security}),
		"lists/deb.debian.org_debian-security_dists_trixie-security_main_binary-amd64_Packages": security,
		"archives/a_1_amd64.deb":                    deb,
		"archives/apt_3.0.4.deb":                    "apt",
		"schemas/code-server-machine-settings.json": "{}",
	}
	if extraDeb != "" {
		files["archives/x.deb"] = extraDeb
	}
	for name, content := range tools {
		files["tools/"+sum(content)+"/"+name] = content
	}
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	for name, content := range files {
		w.WriteHeader(&tar.Header{Name: "./" + name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg})
		w.Write([]byte(content))
	}
	w.Close()
	dir := t.TempDir()
	p := filepath.Join(dir, "collected.tar")
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	env := fmt.Sprintf("CAGE_CODE_SERVER_AMD64_SHA256=%s\nCAGE_MISE_AMD64_SHA256=%s\nCAGE_GH_AMD64_SHA256=%s\n", sum("cs"), sum("mise"), sum("gh"))
	if err := os.WriteFile(filepath.Join(dir, "tool-versions.env"), []byte(env), 0o644); err != nil {
		t.Fatal(err)
	}
	return p, tools
}

func imageEntry(t *testing.T) string {
	dir := t.TempDir()
	h := sha512.Sum512([]byte("image"))
	os.WriteFile(filepath.Join(dir, "data"), []byte("image"), 0o644)
	os.WriteFile(filepath.Join(dir, "url"), []byte("https://cloud.debian.org/images/cloud/trixie/x/debian-13-genericcloud-amd64-x.qcow2"), 0o644)
	os.WriteFile(filepath.Join(dir, "sha512.digest"), []byte("sha512:"+hex.EncodeToString(h[:])), 0o644)
	return dir
}

func TestAssembleBuildsSignedLocalRepositories(t *testing.T) {
	collected, _ := collectedTar(t, "a-deb", "")
	out := filepath.Join(t.TempDir(), "offline")
	if err := assemble(collected, imageEntry(t), "9.9.9", filepath.Join(filepath.Dir(collected), "tool-versions.env"), out, io.Discard); err != nil {
		t.Fatal(err)
	}
	m, err := offlinepack.Load(out)
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != "9.9.9" || m.Arch != "amd64" || m.Image.Path != "image/debian-13-genericcloud-amd64-x.qcow2" {
		t.Fatalf("manifest %+v", m)
	}
	if err := offlinepack.Verify(out, m, nil); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"apt/debian/pool/main/a/a_1_amd64.deb", "apt/debian/dists/trixie/InRelease", "apt/debian/dists/trixie/main/binary-amd64/Packages",
		"apt/debian-security/pool/updates/main/a/apt/apt_3.0.4_amd64.deb", "tools/" + sum("gh") + "/gh.deb", "schemas/code-server-machine-settings.json"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(p))); err != nil {
			t.Errorf("missing %s", p)
		}
	}
	sources, _ := os.ReadFile(filepath.Join(out, "apt", "sources.list"))
	want := "deb [signed-by=/usr/share/keyrings/debian-archive-keyring.gpg check-valid-until=no check-date=no by-hash=no] file:/var/cache/cage-offline/apt/debian trixie main\n"
	if !strings.Contains(string(sources), want) || strings.Contains(string(sources), "trusted=yes") {
		t.Fatalf("sources.list:\n%s", sources)
	}
	security, _ := os.ReadFile(filepath.Join(out, "apt", "security.list"))
	if !strings.Contains(string(security), "apt/debian-security trixie-security main") || strings.Count(string(security), "\n") != 1 {
		t.Fatalf("security.list:\n%s", security)
	}
	listed, _ := os.ReadFile(out + ".packages.tsv")
	if !strings.Contains(string(listed), "a-src\t1\tDebian") {
		t.Fatalf("sources list:\n%s", listed)
	}
}

func TestAssembleRefusesAnUnlistedArchive(t *testing.T) {
	collected, _ := collectedTar(t, "a-deb", "stray")
	out := filepath.Join(t.TempDir(), "offline")
	err := assemble(collected, imageEntry(t), "9.9.9", filepath.Join(filepath.Dir(collected), "tool-versions.env"), out, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "x.deb") {
		t.Fatalf("stray archive: %v", err)
	}
}
