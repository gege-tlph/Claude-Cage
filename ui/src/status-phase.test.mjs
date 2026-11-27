import { test } from "node:test";
import assert from "node:assert/strict";
import { CLOSE_MS, HOLD_MS, MIN_BUSY_MS, POP_MS, createPhase } from "./status-phase.mjs";

// A clock whose timers run only when advanced.
function fakeClock() {
  let time = 0, next = 1;
  const timers = new Map();
  return {
    now: () => time,
    setTimer: (fn, ms) => { const id = next++; timers.set(id, { at: time + ms, fn }); return id; },
    clearTimer: (id) => timers.delete(id),
    advance(ms) {
      const end = time + ms;
      for (;;) {
        const due = [...timers.entries()].filter(([, t]) => t.at <= end).sort((a, b) => a[1].at - b[1].at)[0];
        if (!due) break;
        timers.delete(due[0]);
        time = due[1].at;
        due[1].fn();
      }
      time = end;
    },
  };
}

function driver(options = {}) {
  const clock = fakeClock(), seen = [];
  const phase = createPhase((p) => seen.push(`${clock.now()}:${p}`), { ...clock, reduced: () => false, ...options });
  return { clock, seen, phase };
}

test("a quick action keeps the spinner for the minimum, then shows done and returns to rest", () => {
  const { clock, seen, phase } = driver();
  let idle = 0;
  phase.start();
  clock.advance(100);
  phase.finish(true, () => idle++);
  clock.advance(10_000);
  const doneAt = MIN_BUSY_MS, idleAt = doneAt + CLOSE_MS + POP_MS + HOLD_MS;
  assert.deepEqual(seen, ["0:busy", `${doneAt}:done`, `${idleAt}:idle`]);
  assert.equal(idle, 1);
});

test("a slow action shows its result as soon as it ends", () => {
  const { clock, seen, phase } = driver();
  phase.start();
  clock.advance(2000);
  phase.finish(false);
  clock.advance(0);
  assert.deepEqual(seen, ["0:busy", "2000:fail"]);
});

test("without motion the result still stays readable", () => {
  const { clock, seen, phase } = driver({ reduced: () => true });
  phase.start();
  clock.advance(MIN_BUSY_MS);
  phase.finish(true);
  clock.advance(10_000);
  assert.deepEqual(seen, ["0:busy", `${MIN_BUSY_MS}:done`, `${MIN_BUSY_MS + HOLD_MS}:idle`]);
});

test("starting again during the result restarts the spinner", () => {
  const { clock, seen, phase } = driver();
  phase.start();
  clock.advance(MIN_BUSY_MS);
  phase.finish(true);
  clock.advance(100);
  phase.start();
  clock.advance(10_000);
  assert.deepEqual(seen, ["0:busy", `${MIN_BUSY_MS}:done`, `${MIN_BUSY_MS + 100}:busy`]);
  assert.equal(phase.phase, "busy");
});

test("a finish without a start is ignored, reset returns to rest at once", () => {
  const { clock, seen, phase } = driver();
  assert.equal(phase.finish(true), false);
  clock.advance(10_000);
  assert.deepEqual(seen, []);
  phase.start();
  phase.reset();
  clock.advance(10_000);
  assert.deepEqual(seen, ["10000:busy", "10000:idle"]);
});
