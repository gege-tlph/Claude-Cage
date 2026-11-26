import test from "node:test";
import assert from "node:assert/strict";
import { addRelay, choose, move, nextEmpty, remove, toChain, toSingle } from "./proxy-route.mjs";

test("switching to a chain does not pin the old node as the exit", () => {
  const fresh = toChain(["hk"]);
  assert.deepEqual(fresh, { route: ["", "hk"], active: 0 });
  // Moving the old exit up makes it the relay; the exit is the slot to choose.
  const relay = move(fresh.route, 1, -1);
  assert.deepEqual(relay, { route: ["hk", ""], active: 1 });
  assert.deepEqual(choose(relay.route, relay.active, "us"), { route: ["hk", "us"], active: -1 });
});

test("a node fills the slot being chosen, trading places if already used", () => {
  assert.deepEqual(choose(["", "hk"], 0, "jp"), { route: ["jp", "hk"], active: -1 });
  assert.deepEqual(choose(["jp", "hk"], 1, "jp"), { route: ["hk", "jp"], active: -1 });
  assert.deepEqual(choose(["hk"], -1, "jp"), { route: ["jp"], active: -1 });
  assert.deepEqual(choose(["a", "b"], -1, "c"), { route: ["a", "b"], active: -1 });
});

test("hops move, a relay can be added or removed, the exit stays", () => {
  assert.deepEqual(move(["a", "b"], 0, 1), { route: ["b", "a"], active: 1 });
  assert.deepEqual(move(["a", "b"], 0, -1), { route: ["a", "b"], active: 0 });
  assert.deepEqual(addRelay(["a", "b"]), { route: ["a", "", "b"], active: 1 });
  assert.deepEqual(remove(["a", "", "b"], 1), { route: ["a", "b"], active: -1 });
  assert.deepEqual(remove(["a", "b"], 1), { route: ["a", "b"], active: -1 });
  assert.deepEqual(remove(["a", "b"], 0), { route: ["b"], active: -1 });
  assert.deepEqual(toSingle(["a", "b"]), { route: ["b"], active: -1 });
  assert.deepEqual(toSingle(["a", ""]), { route: ["a"], active: -1 });
  assert.equal(nextEmpty(["a", "", ""]), 1);
});

// --- Upstream #1063 hop and relay group helpers.
import {
  RELAY_STRATEGY_FALLBACK,
  RELAY_STRATEGY_URL_TEST,
  bulkToggleNodesInRelayHop,
  canSwapTwoHopRoute,
  hopsEqual,
  isMultiNodeHop,
  moveNodeInRelayHop,
  nodeHop,
  nodesClaimedByOtherHops,
  relayHop,
  resolveHopPickerIndex,
  sanitizeRelayHopAfterExitChange,
  savedHopsFromProxy,
  serializeHopsForAPI,
  singleHopFromRoute,
  sourceUsableRelayCount,
  swapTwoHopRoute,
  toggleNodeInRelayHop,
  validateRouteHops,
} from "./proxy-route.mjs";

const sources = [
  {
    id: "airport",
    name: "日常机场",
    nodes: [
      { id: "hk1", name: "香港 01" },
      { id: "hk2", name: "香港 02" },
      { id: "jp1", name: "日本 01" },
    ],
  },
  {
    id: "home",
    name: "住宅出口",
    nodes: [{ id: "exit1", name: "美国住宅" }],
  },
];
const usableIDs = new Set(["hk1", "hk2", "jp1", "exit1"]);

test("unified relay hop treats 1 node as fixed and >=2 or source as auto-switch group", () => {
  const fixedHops = savedHopsFromProxy({ route: ["hk1", "exit1"], selected: "exit1" });
  assert.deepEqual(fixedHops, [relayHop({ node_ids: ["hk1"] }), nodeHop("exit1")]);
  assert.equal(isMultiNodeHop(fixedHops[0]), false);
  // 1-node transit serializes to plain node_ids (backward-compatible).
  assert.deepEqual(serializeHopsForAPI(fixedHops), { node_ids: ["hk1", "exit1"] });
  assert.equal(validateRouteHops(fixedHops, sources, usableIDs).valid, true);

  // Adding a 2nd node seamlessly promotes to an auto-switch group.
  const multiHops = toggleNodeInRelayHop(fixedHops, 0, "hk2", sources, usableIDs);
  assert.equal(isMultiNodeHop(multiHops[0]), true);
  assert.deepEqual(serializeHopsForAPI(multiHops), {
    hops: [
      { relay: { strategy: RELAY_STRATEGY_FALLBACK, node_ids: ["hk1", "hk2"] } },
      { node_id: "exit1" },
    ],
  });

  // Removing hk2 seamlessly demotes back to single fixed node.
  const backToSingle = toggleNodeInRelayHop(multiHops, 0, "hk2", sources, usableIDs);
  assert.equal(isMultiNodeHop(backToSingle[0]), false);
  assert.deepEqual(serializeHopsForAPI(backToSingle), { node_ids: ["hk1", "exit1"] });
  assert.equal(hopsEqual(fixedHops, backToSingle), true);
});

test("whole-source relay hop materializes cleanly when user unchecks one node", () => {
  const sourceRoute = [relayHop({ strategy: RELAY_STRATEGY_URL_TEST, source_id: "airport" }), nodeHop("exit1")];
  assert.equal(isMultiNodeHop(sourceRoute[0]), true);
  assert.deepEqual(serializeHopsForAPI(sourceRoute), {
    hops: [
      { relay: { strategy: RELAY_STRATEGY_URL_TEST, source_id: "airport" } },
      { node_id: "exit1" },
    ],
  });

  // Unchecking jp1 converts to custom pool [hk1, hk2] keeping strategy.
  const narrowed = toggleNodeInRelayHop(sourceRoute, 0, "jp1", sources, usableIDs);
  assert.equal(narrowed[0].source_id, "");
  assert.deepEqual(narrowed[0].node_ids, ["hk1", "hk2"]);
  assert.equal(narrowed[0].strategy, RELAY_STRATEGY_URL_TEST);
});

test("priority reorder, bulk toggle, swap, and exit sanitize work cleanly", () => {
  let hops = [relayHop({ node_ids: [] }), nodeHop("exit1")];
  hops = bulkToggleNodesInRelayHop(hops, 0, ["hk1", "hk2", "jp1"], true, sources, usableIDs);
  assert.deepEqual(hops[0].node_ids, ["hk1", "hk2", "jp1"]);

  hops = moveNodeInRelayHop(hops, 0, "jp1", -1);
  assert.deepEqual(hops[0].node_ids, ["hk1", "jp1", "hk2"]);
  hops = moveNodeInRelayHop(hops, 0, "hk2", -Infinity);
  assert.deepEqual(hops[0].node_ids, ["hk2", "hk1", "jp1"]);

  assert.equal(canSwapTwoHopRoute(hops), false);
  const singleTransit = [relayHop({ node_ids: ["hk1"] }), nodeHop("exit1")];
  assert.equal(canSwapTwoHopRoute(singleTransit), true);
  assert.deepEqual(swapTwoHopRoute(singleTransit), [relayHop({ node_ids: ["exit1"] }), nodeHop("hk1")]);

  const sanitized = sanitizeRelayHopAfterExitChange([
    relayHop({ node_ids: ["hk1", "jp1", "hk2"] }),
    nodeHop("jp1"),
  ], "jp1");
  assert.deepEqual(sanitized[0].node_ids, ["hk1", "hk2"]);
  assert.deepEqual(singleHopFromRoute(hops), [nodeHop("exit1")]);
});

test("same-source exit is allowed and picker focuses missing hop first", () => {
  const sourceRoute = [relayHop({ source_id: "airport" }), nodeHop("jp1")];
  const val = validateRouteHops(sourceRoute, sources, usableIDs);
  assert.equal(val.valid, true);
  assert.deepEqual(val.candidateCounts, [2, 1]);

  // Source-based relay does not block selecting an exit from that source.
  const exitClaimed = nodesClaimedByOtherHops(sourceRoute, 1, sources, usableIDs);
  assert.equal(exitClaimed.size, 0);

  // Empty transit focuses transit (0); filled transit + empty exit focuses exit (1).
  assert.equal(resolveHopPickerIndex([relayHop(), nodeHop("exit1")], null, sources, usableIDs), 0);
  assert.equal(resolveHopPickerIndex([relayHop({ node_ids: ["hk1"] }), nodeHop("")], null, sources, usableIDs), 1);

  // sourceUsableRelayCount does not subtract the node currently selected in targetIndex itself.
  const singleFromAirport = [relayHop({ node_ids: ["hk1"] }), nodeHop("exit1")];
  assert.equal(sourceUsableRelayCount(sources[0], singleFromAirport, 0, usableIDs, sources), 3);
});
