import test from "node:test";
import assert from "node:assert/strict";
import { addRelay, canMakeRelay, choose, makeRelay, move, nextEmpty, remove, toChain, toSingle } from "./proxy-route.mjs";

test("switching to a chain does not pin the old node as the exit", () => {
  const fresh = toChain(["hk"]);
  assert.deepEqual(fresh, { route: ["", "hk"], active: 0 });
  assert.equal(canMakeRelay(fresh.route), true);
  // 设为前置: the old node goes in front, the exit is now the slot to choose.
  const relay = makeRelay(fresh.route);
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
