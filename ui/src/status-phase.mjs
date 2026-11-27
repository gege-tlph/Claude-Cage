// Timing of an action that the user waits for: a button's spinner, then 完成
// or 失败, then back to rest. One circle language for every state:
//   idle · selected (待应用) · busy · done · fail
// Pure so it can be tested without React; status-icon.jsx and
// action-button.jsx use it.

// The arc closes to a full turn while it still spins, then the mark draws.
export const CLOSE_MS = 350;
// The mark's draw and the slight pop after it.
export const POP_MS = 320;
// A spinner shorter than this reads as a flicker.
export const MIN_BUSY_MS = 400;
// 完成 / 失败 stays on the button this long after the mark is drawn.
export const HOLD_MS = 1200;

export const PHASES = ["idle", "busy", "done", "fail"];

export function reducedMotion() {
  if (typeof document === "undefined") return false;
  if (document.documentElement.dataset.reducedMotion === "true") return true;
  return typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;
}

// createPhase drives one action. start() shows the spinner; finish(ok) keeps
// it for at least MIN_BUSY_MS, then shows done or fail, then idle.
// onChange(phase) is called on every change; onIdle after a finish returns to
// rest. Timers are injected for tests.
export function createPhase(onChange, {
  now = () => (typeof performance !== "undefined" ? performance.now() : Date.now()),
  setTimer = setTimeout,
  clearTimer = clearTimeout,
  reduced = reducedMotion,
} = {}) {
  let phase = "idle", started = 0, timers = [];
  const set = (next) => {
    if (next === phase) return;
    phase = next;
    onChange(next);
  };
  const clear = () => {
    timers.forEach(clearTimer);
    timers = [];
  };
  const later = (ms, fn) => {
    const id = setTimer(() => {
      timers = timers.filter((t) => t !== id);
      fn();
    }, ms);
    timers.push(id);
  };
  return {
    get phase() { return phase; },
    start() {
      clear();
      started = now();
      set("busy");
    },
    // Ignored, returning false, when nothing was started; else onIdle runs
    // once the result has shown.
    finish(ok, onIdle) {
      if (phase === "idle" && !started) return false;
      clear();
      const remaining = phase === "busy" ? Math.max(0, MIN_BUSY_MS - (now() - started)) : 0;
      started = 0;
      later(remaining, () => {
        set(ok ? "done" : "fail");
        const motion = reduced() ? 0 : CLOSE_MS + POP_MS;
        later(motion + HOLD_MS, () => {
          set("idle");
          onIdle?.();
        });
      });
      return true;
    },
    reset() {
      clear();
      started = 0;
      set("idle");
    },
    dispose() {
      clear();
    },
  };
}
