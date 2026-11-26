import test from "node:test";
import assert from "node:assert/strict";
import { exitRegion, pickRelay } from "./relay-pick.mjs";

const candidates = [
  { id: "hk", name: "🇭🇰 香港 01" },
  { id: "au1", name: "澳大利亚 悉尼 01" },
  { id: "au2", name: "🇦🇺 Sydney 02" },
  { id: "jp", name: "日本 东京" },
  { id: "info", name: "剩余流量：100 GB" },
];
const results = [
  { node_id: "hk", delay: 40 },
  { node_id: "au1", delay: 180 },
  { node_id: "au2", delay: 150 },
  { node_id: "jp", error: "测速超时" },
  { node_id: "info", error: "上游失败" },
];

test("the relay is the fastest working node in the exit's region", () => {
  assert.deepEqual(pickRelay({ region: "AU", candidates, results }), { node: candidates[2], delay: 150, sameRegion: true });
});

test("without a node in the region the fastest working node is used, and says so", () => {
  assert.deepEqual(pickRelay({ region: "US", candidates, results }), { node: candidates[0], delay: 40, sameRegion: false });
  assert.deepEqual(pickRelay({ region: "", candidates, results }), { node: candidates[0], delay: 40, sameRegion: false });
});

test("failed, missing and excluded nodes are never chosen", () => {
  assert.equal(pickRelay({ region: "JP", candidates, results }).node.id, "hk");
  assert.equal(pickRelay({ region: "AU", candidates, results, exclude: ["au2"] }).node.id, "au1");
  assert.equal(pickRelay({ region: "AU", candidates, results: [{ node_id: "jp", error: "x" }] }), null);
  assert.equal(pickRelay({ region: "AU", candidates: [], results }), null);
});

test("the exit's region comes from the panel, else from its name", () => {
  assert.equal(exitRegion({ name: "悉尼住宅", region: "AU" }), "AU");
  assert.equal(exitRegion({ name: "🇯🇵 东京住宅", region: "" }), "JP");
  assert.equal(exitRegion({ name: "office" }), "");
  assert.equal(exitRegion(null), "");
});
