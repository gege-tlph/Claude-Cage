package catalog

import (
	"encoding/json"
	"testing"
)

// The assets of a Cage lite release, as GitHub's releases API lists them.
// An installed 0.2.0 must find 0.2.1 there; drafts and prereleases stay
// invisible to it.
func TestSelectCageLiteWindowsRelease(t *testing.T) {
	base := "https://github.com/" + Repository + "/releases/download/"
	asset := func(tag, name string, size int64) map[string]any {
		return map[string]any{"name": name, "browser_download_url": base + tag + "/" + name, "size": size, "state": "uploaded", "digest": "sha256:" + "ab"}
	}
	raw, err := json.Marshal([]map[string]any{
		{"tag_name": "v0.2.2", "draft": true, "prerelease": false, "assets": []any{asset("v0.2.2", "Cage-0.2.2-windows-amd64-setup.exe", 400<<20)}},
		{"tag_name": "v0.2.2-rc.1", "draft": false, "prerelease": true, "assets": []any{asset("v0.2.2-rc.1", "Cage-0.2.2-rc.1-windows-amd64-setup.exe", 400<<20)}},
		{"tag_name": "v0.2.1", "draft": false, "prerelease": false, "assets": []any{
			asset("v0.2.1", "Cage-0.2.1-windows-amd64-setup.exe", 424<<20),
			asset("v0.2.1", "SHA256SUMS", 900),
			asset("v0.2.1", "cage-lite-signing.cer", 1086),
			asset("v0.2.1", "mihomo-v1.19.33-source.tar.gz", 1<<20),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var releases []Release
	if err := json.Unmarshal(raw, &releases); err != nil {
		t.Fatal(err)
	}
	got, err := Select(releases, "0.2.0", "windows", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Version != "0.2.1" || got.Archive.Name != "Cage-0.2.1-windows-amd64-setup.exe" || got.Checksums.Name != "SHA256SUMS" {
		t.Fatalf("installed 0.2.0 does not select the 0.2.1 installer: %+v", got)
	}
	if again, err := Select(releases, "0.2.1", "windows", "amd64"); err != nil || again != nil {
		t.Fatalf("0.2.1 is offered to itself or to a draft/prerelease: %+v %v", again, err)
	}
}

// The manifest scripts/release-manifest.mjs writes (this one is its output for
// two small files) is one release as the API lists it: an installed 0.2.2
// selects it and checks the archive against its SHA256SUMS.
func TestSelectFromReleaseManifest(t *testing.T) {
	const manifest = `{
  "tag_name": "v0.2.3",
  "draft": false,
  "prerelease": false,
  "assets": [
    {
      "name": "Cage-0.2.3-windows-amd64-setup.exe",
      "browser_download_url": "https://github.com/gege-tlph/Claude-Cage/releases/download/v0.2.3/Cage-0.2.3-windows-amd64-setup.exe",
      "size": 15,
      "digest": "sha256:e34210a6de4f653edf588301431c3d69a633638cbf587345cc50a7fed9f38f4c",
      "state": "uploaded"
    },
    {
      "name": "SHA256SUMS",
      "browser_download_url": "https://github.com/gege-tlph/Claude-Cage/releases/download/v0.2.3/SHA256SUMS",
      "size": 189,
      "digest": "sha256:6bfb9f7e0c0e76c719bd5308ca9e23b1d440751ff485430da29ee3bc86ae2807",
      "state": "uploaded"
    },
    {
      "name": "cage-lite-signing.cer",
      "browser_download_url": "https://github.com/gege-tlph/Claude-Cage/releases/download/v0.2.3/cage-lite-signing.cer",
      "size": 4,
      "digest": "sha256:06298432e8066b29e2223bcc23aa9504b56ae508fabf3435508869b9c3190e22",
      "state": "uploaded"
    }
  ]
}`
	const sums = "e34210a6de4f653edf588301431c3d69a633638cbf587345cc50a7fed9f38f4c *Cage-0.2.3-windows-amd64-setup.exe\n" +
		"06298432e8066b29e2223bcc23aa9504b56ae508fabf3435508869b9c3190e22 *cage-lite-signing.cer\n"
	var release Release
	if err := json.Unmarshal([]byte(manifest), &release); err != nil {
		t.Fatal(err)
	}
	c, err := Select([]Release{release}, "0.2.2", "windows", "amd64")
	if err != nil || c == nil || c.Version != "0.2.3" || c.Archive.Name != "Cage-0.2.3-windows-amd64-setup.exe" || c.Checksums.Name != "SHA256SUMS" {
		t.Fatalf("%+v %v", c, err)
	}
	if digest, err := c.SHA256([]byte(sums)); err != nil || digest != "e34210a6de4f653edf588301431c3d69a633638cbf587345cc50a7fed9f38f4c" {
		t.Fatalf("%s %v", digest, err)
	}
	if c, err := Select([]Release{release}, "0.2.3", "windows", "amd64"); err != nil || c != nil {
		t.Fatalf("the installed version was offered: %+v %v", c, err)
	}
}
