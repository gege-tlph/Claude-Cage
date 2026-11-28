import assert from "node:assert/strict";
import test from "node:test";
import { LOG_TAIL, eventKey, tailStart } from "./log-tail.mjs";

const events = (count) => Array.from({ length: count }, (_, i) => ({ time: `t${i}`, source: "proxy", message: `line ${i}` }));

test("a short log is drawn whole", () => {
  assert.equal(tailStart(events(20)), 0);
  assert.equal(tailStart([]), 0);
  assert.equal(tailStart(null), 0);
});

test("a long log starts with its newest lines", () => {
  assert.equal(tailStart(events(3000)), 3000 - LOG_TAIL);
});

test("a scrolled-up view keeps its first line while new lines arrive", () => {
  const before = events(3000);
  const first = eventKey(before[1200]);
  const after = [...before.slice(5), ...events(3005).slice(3000)];
  assert.equal(tailStart(after, first), 1195);
});

test("a first line that aged out keeps the view at the oldest remaining line", () => {
  const timed = Array.from({ length: 500 }, (_, i) => ({ time: new Date(Date.UTC(2026, 10, 28, 0, 0, i)).toISOString(), source: "proxy", message: `line ${i}` }));
  assert.equal(tailStart(timed.slice(2), eventKey(timed[0])), 0);
  assert.equal(tailStart(timed.slice(2), eventKey(timed[100])), 98);
});

test("a first line that has left the log falls back to the newest lines", () => {
  assert.equal(tailStart(events(3000), eventKey({ time: "gone", source: "x", message: "y" })), 3000 - LOG_TAIL);
});
