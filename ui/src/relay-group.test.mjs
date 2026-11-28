import assert from "node:assert/strict";
import test from "node:test";
import { candidates, groupValid, hopsFromProxy, isGroup, newGroup, routeBody, routeKey, toggleNode, withMode, withSource, withStrategy } from "./relay-group.mjs";

const sources = [
  { id: "air", name: "旅行订阅", nodes: [{ id: "la" }, { id: "tokyo" }, { id: "hk" }] },
  { id: "home", name: "住宅代理", nodes: [{ id: "exit" }] },
];
const nodes = [
  { id: "la", sourceID: "air" }, { id: "tokyo", sourceID: "air" }, { id: "hk", sourceID: "air", blocked_reason: "需迁移" },
  { id: "exit", sourceID: "home" }, { id: "ferry", sourceID: "local", relay: true },
];

test("a route without groups is sent as node_ids, as before", () => {
  assert.deepEqual(routeBody(["la", "exit"]), { node_ids: ["la", "exit"] });
  assert.equal(hopsFromProxy({ route_hops: [{ node_id: "la" }, { node_id: "exit" }] }), null);
});

test("a saved group comes back with the range it was saved with", () => {
  const route = hopsFromProxy({ route_hops: [
    { relay: { strategy: "url-test", source_id: "air" }, candidate_count: 2, active_node_id: "la" },
    { relay: { strategy: "fallback", node_ids: ["la", "tokyo"] } },
    { node_id: "exit" },
  ] });
  assert.deepEqual(route[0], { relay: { strategy: "url-test", source_id: "air", node_ids: [] }, mode: "source" });
  assert.deepEqual(route[1], { relay: { strategy: "fallback", source_id: "", node_ids: ["la", "tokyo"] }, mode: "nodes" });
  assert.equal(route[2], "exit");
});

test("a group is sent as hops with only the range it shows", () => {
  let hop = newGroup(sources, "la");
  assert.equal(hop.relay.source_id, "air");
  hop = withStrategy(hop, "url-test");
  assert.deepEqual(routeBody([hop, "exit"]), { hops: [{ relay: { strategy: "url-test", source_id: "air" } }, { node_id: "exit" }] });
  hop = toggleNode(toggleNode(withMode(hop, "nodes"), "la"), "tokyo");
  assert.deepEqual(routeBody([hop, "exit"]).hops[0], { relay: { strategy: "url-test", node_ids: ["la", "tokyo"] } });
  assert.deepEqual(toggleNode(hop, "la").relay.node_ids, ["tokyo"]);
  // Choosing nodes keeps the source for switching back; the saved route only
  // changes with what is sent.
  assert.equal(routeKey([withMode(withSource(hop, "air"), "source"), "exit"]), routeKey([{ relay: { strategy: "url-test", source_id: "air", node_ids: [] }, mode: "source" }, "exit"]));
});

test("a group needs two usable candidates outside the fixed hops", () => {
  const whole = newGroup(sources, "la");
  assert.deepEqual(candidates(whole, [whole, "exit"], nodes).map((n) => n.id), ["la", "tokyo"]);
  assert.equal(groupValid(whole, [whole, "exit"], nodes), true);
  // A fixed hop of the same chain and a blocked node are not candidates.
  assert.equal(groupValid(whole, ["tokyo", whole, "exit"], nodes), false);
  const picked = toggleNode(withMode(whole, "nodes"), "la");
  assert.equal(groupValid(picked, [picked, "exit"], nodes), false);
  assert.equal(groupValid(toggleNode(picked, "hk"), [picked, "exit"], nodes), false);
  assert.equal(groupValid(toggleNode(picked, "tokyo"), [picked, "exit"], nodes), true);
  assert.equal(isGroup("la"), false);
  assert.equal(isGroup(picked), true);
});
