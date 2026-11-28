import assert from "node:assert/strict";
import test from "node:test";
import { candidates, groupProblem, groupValid, hopsFromProxy, isGroup, moveNode, newGroup, routeBody, routeKey, sourceNodes, toggleNode, withMode, withSource, withStrategy } from "./relay-group.mjs";

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
  const withHK = toggleNode(picked, "hk");
  assert.equal(groupValid(withHK, [withHK, "exit"], nodes), false);
  const both = toggleNode(picked, "tokyo");
  assert.equal(groupValid(both, [both, "exit"], nodes), true);
  assert.equal(isGroup("la"), false);
  assert.equal(isGroup(picked), true);
});

test("groups the backend would refuse are not valid, with the reason", () => {
  const more = [...nodes, { id: "sf", sourceID: "air" }];
  const a = newGroup(sources, "la");
  // Two whole-source groups over one source, or chosen nodes shared.
  const b = newGroup(sources, "la");
  assert.equal(groupProblem(a, [a, b, "exit"], more), "overlap");
  assert.equal(groupProblem(b, [a, b, "exit"], more), "overlap");
  const mine = toggleNode(toggleNode(withMode(newGroup(sources), "nodes"), "la"), "sf");
  const theirs = toggleNode(toggleNode(withMode(newGroup(sources), "nodes"), "tokyo"), "sf");
  assert.equal(groupProblem(mine, [mine, theirs, "exit"], more), "overlap");
  const apart = toggleNode(toggleNode(withMode(newGroup(sources), "nodes"), "la"), "tokyo");
  assert.equal(groupProblem(apart, [apart, "exit"], more), "");
  // A chosen node that cannot be used; a source with one node.
  assert.equal(groupProblem(toggleNode(apart, "ferry"), [toggleNode(apart, "ferry"), "exit"], nodes), "unusable");
  const lone = withSource(a, "home");
  assert.equal(groupProblem(lone, [lone, "la"], nodes), "few");
  // Over 300 candidates in a whole source.
  const big = [{ id: "big", name: "大机场", nodes: Array.from({ length: 301 }, (_, i) => ({ id: `n${i}` })) }];
  const huge = newGroup(big);
  assert.equal(groupProblem(huge, [huge, "exit"], sourceNodes(big)), "many");
  assert.equal(sourceNodes(big)[0].sourceID, "big");
});

test("a new group takes a source no other group of the route takes whole", () => {
  const two = [...sources, { id: "more", name: "备用", nodes: [{ id: "m1" }, { id: "m2" }] }];
  const first = newGroup(two, "la");
  assert.equal(first.relay.source_id, "air");
  assert.equal(newGroup(two, "la", [first, "", "exit"]).relay.source_id, "more");
  // Chosen nodes leave the source free.
  assert.equal(newGroup(two, "la", [withMode(first, "nodes"), "", "exit"]).relay.source_id, "air");
});

test("chosen nodes keep the order 故障转移 tries them in", () => {
  const hop = toggleNode(toggleNode(toggleNode(withMode(newGroup(sources), "nodes"), "la"), "tokyo"), "hk");
  assert.deepEqual(moveNode(hop, 2, -1).relay.node_ids, ["la", "hk", "tokyo"]);
  assert.deepEqual(moveNode(hop, 0, 1).relay.node_ids, ["tokyo", "la", "hk"]);
  assert.equal(moveNode(hop, 0, -1), hop);
  assert.equal(moveNode(hop, 2, 1), hop);
  assert.deepEqual(routeBody([moveNode(hop, 2, -1), "exit"]).hops[0].relay.node_ids, ["la", "hk", "tokyo"]);
});
