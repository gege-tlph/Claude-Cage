import test from "node:test";
import assert from "node:assert/strict";
import { adjacentTerminal, reconcileSplits, resizeTerminalSplit, splitTerminal, terminalGeometry } from "./terminal-splits.mjs";

const layout = () => splitTerminal(splitTerminal([], "a", "b", "right", "columns"), "b", "c", "bottom", "rows");

test("nested splits resize independently on both axes", () => {
  let groups = layout();
  groups = resizeTerminalSplit(groups, "columns", 0.4);
  groups = resizeTerminalSplit(groups, "rows", 0.6);
  const { panes, dividers } = terminalGeometry(groups[0], 1008, 508);
  assert.deepEqual([...panes], [
    ["a", { left: 0, top: 0, width: 400, height: 508 }],
    ["b", { left: 408, top: 0, width: 600, height: 300 }],
    ["c", { left: 408, top: 308, width: 600, height: 200 }],
  ]);
  assert.deepEqual(dividers.map(divider => divider.axis), ["x", "y"]);
});

test("closing a pane promotes the nearest surviving neighbor and collapses its parent", () => {
  const groups = layout();
  const available = new Set(["a", "c"]);
  assert.equal(adjacentTerminal(groups[0], "b", available), "c");
  const [tree] = reconcileSplits(groups, available);
  assert.equal(tree.id, "columns");
  assert.equal(tree.second, "c");
  assert.deepEqual(reconcileSplits([tree], new Set(["a"])), []);
});

test("moving a pane across groups collapses the old split and never duplicates sessions", () => {
  const groups = splitTerminal(layout(), "d", "e", "top", "separate");
  const moved = splitTerminal(groups, "a", "e", "left", "move");
  assert.equal(moved.length, 1);
  assert.deepEqual([...terminalGeometry(moved[0], 1200, 600).panes.keys()], ["e", "a", "b", "c"]);
});

test("deep splits keep enough space for every pane in a small viewport", () => {
  const groups = splitTerminal(layout(), "a", "d", "right", "deeper");
  const { panes, minimum, dividers } = terminalGeometry(groups[0], 280, 160);
  assert.ok(minimum.width > 280 && minimum.height > 160);
  for (const { width, height } of panes.values()) assert.ok(width >= 220 && height >= 128);
  for (const { first, min, max } of dividers) assert.ok(min <= first && first <= max);
});
