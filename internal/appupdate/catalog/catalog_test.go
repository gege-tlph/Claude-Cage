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
