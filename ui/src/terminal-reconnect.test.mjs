import assert from "node:assert/strict";
import test from "node:test";
import {
  terminalHistoryPlan, terminalLiveness, terminalProbeMs, terminalReconnectLimit,
  terminalReconnectPlan, terminalSilenceMs, terminalStreamPath,
} from "./terminal-reconnect.mjs";

test("quick failures back off, then stop", () => {
  let failures = 0;
  const delays = [];
  for (;;) {
    const plan = terminalReconnectPlan(failures, 100);
    failures = plan.failures;
    if (!plan.delay) break;
    delays.push(plan.delay);
  }
  assert.deepEqual(delays, [500, 1000, 2000, 4000, 8000, 15000]);
  assert.equal(failures, terminalReconnectLimit + 1);
});

test("a stream that stayed open starts the back-off over", () => {
  assert.deepEqual(terminalReconnectPlan(5, 60_000), { failures: 1, delay: 500 });
});

test("the stream path resumes after the received offset", () => {
  assert.equal(terminalStreamPath("a b", 12), "/terminals/a%20b/stream?after=12");
  assert.equal(terminalStreamPath("t", null), "/terminals/t/stream?after=0");
  assert.equal(terminalStreamPath("t", -1), "/terminals/t/stream?after=0");
});

test("history continues the screen only at the expected offset", () => {
  assert.deepEqual(terminalHistoryPlan(40, { offset: 40, size: 5, reset: false }), { reset: false, offset: 40, size: 5 });
  assert.deepEqual(terminalHistoryPlan(40, { offset: 10, size: 5, reset: false }), { reset: true, offset: 10, size: 5 });
  assert.deepEqual(terminalHistoryPlan(40, { offset: 40, size: 5, reset: true }), { reset: true, offset: 40, size: 5 });
  assert.deepEqual(terminalHistoryPlan(null, { offset: 0, size: 0 }), { reset: true, offset: 0, size: 0 });
  assert.deepEqual(terminalHistoryPlan(40, { type: "history" }), { reset: false, offset: null, size: null });
});

test("a silent stream is probed before it is dropped", () => {
  const start = 1_000_000;
  assert.equal(terminalLiveness({ now: start + 1000, lastReceived: start }), "idle");
  assert.equal(terminalLiveness({ now: start + terminalSilenceMs, lastReceived: start }), "probe");
  const probe = start + terminalSilenceMs;
  assert.equal(terminalLiveness({ now: probe + 1000, lastReceived: start, probe }), "idle");
  assert.equal(terminalLiveness({ now: probe + terminalProbeMs, lastReceived: start, probe }), "dead");
  assert.equal(terminalLiveness({ now: probe + terminalProbeMs, lastReceived: probe + 10, probe }), "idle");
});
