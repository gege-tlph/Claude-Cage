// Writes cage-lite-release.json: this release as the releases API lists it
// (tag, assets with size and SHA-256 digest), for in-app updates to read from
// github.com/<repository>/releases/latest/download/ (internal/appupdate,
// catalog.ManifestName). Run on the final release files, after SHA256SUMS.
//
//   node scripts/release-manifest.mjs <release files> <version> <owner/repository>
import { createHash } from "node:crypto";
import { readdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

export const manifestName = "cage-lite-release.json";

export function releaseManifest(directory, version, repository) {
  if (!/^\d+\.\d+\.\d+$/.test(version)) throw new Error(`not a release version: ${version}`);
  if (!/^[\w.-]+\/[\w.-]+$/.test(repository)) throw new Error(`not a repository: ${repository}`);
  const tag = `v${version}`;
  const assets = readdirSync(directory)
    .filter(name => name !== manifestName && statSync(join(directory, name)).isFile())
    .sort()
    .map(name => {
      const bytes = readFileSync(join(directory, name));
      return {
        name,
        browser_download_url: `https://github.com/${repository}/releases/download/${tag}/${encodeURIComponent(name)}`,
        size: bytes.length,
        digest: "sha256:" + createHash("sha256").update(bytes).digest("hex"),
        state: "uploaded",
      };
    });
  if (!assets.some(asset => asset.name === "SHA256SUMS")) throw new Error("SHA256SUMS is missing");
  return { tag_name: tag, draft: false, prerelease: false, assets };
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const [directory, version, repository] = process.argv.slice(2);
  if (!directory || !version || !repository) {
    console.error("usage: release-manifest.mjs <release files> <version> <owner/repository>");
    process.exit(2);
  }
  const manifest = releaseManifest(directory, version, repository);
  writeFileSync(join(directory, manifestName), JSON.stringify(manifest, null, 2) + "\n");
  console.log(`${manifestName}: ${manifest.assets.length} assets for ${manifest.tag_name}`);
}
