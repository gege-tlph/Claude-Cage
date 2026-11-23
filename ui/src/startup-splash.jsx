import React, { useEffect, useState, useSyncExternalStore } from "react";
import "./startup-splash.css";

// One splash covers the whole startup. Each loading stage (connecting to the
// backend, opening the environment, loading the workbench code, reading the
// workspace) registers itself with <StartupStage status=… /> instead of
// drawing its own text, so nothing jumps between stages. The splash leaves
// once no stage is registered; it never waits for a "done" signal, so a path
// without stages (an error page, the environment picker) cannot stay covered.

const stages = new Map();
const listeners = new Set();
let sequence = 0;
let started = false;
let snapshot = { active: false, status: "" };

function publish() {
  const status = [...stages.values()].at(-1) ?? "";
  snapshot = { active: stages.size > 0, status };
  for (const listener of listeners) listener();
}

function subscribe(listener) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function StartupStage({ status }) {
  useEffect(() => {
    const id = ++sequence;
    stages.set(id, status);
    started = true;
    publish();
    return () => {
      stages.delete(id);
      publish();
    };
  }, [status]);
  return null;
}

// Before any stage registers (the interface language is still loading) the
// splash waits at most this long; between stages it bridges short gaps.
const FIRST_STAGE_WAIT = 4000;
const STAGE_GAP = 180;
const LEAVE_DURATION = 420;

export function StartupSplash() {
  const { active, status } = useSyncExternalStore(subscribe, () => snapshot);
  const [phase, setPhase] = useState("shown");
  useEffect(() => {
    if (active) {
      setPhase("shown");
      return undefined;
    }
    const timer = setTimeout(() => setPhase((current) => (current === "gone" ? current : "leaving")), started ? STAGE_GAP : FIRST_STAGE_WAIT);
    return () => clearTimeout(timer);
  }, [active]);
  useEffect(() => {
    if (phase !== "leaving") return undefined;
    const timer = setTimeout(() => setPhase("gone"), LEAVE_DURATION);
    return () => clearTimeout(timer);
  }, [phase]);
  if (phase === "gone") return null;
  return (
    <div className={`startup-splash${phase === "leaving" ? " leaving" : ""}`} role="status" aria-live="polite">
      <div className="startup-splash-stage">
        <div className="startup-splash-glow" aria-hidden="true" />
        <CageMark />
      </div>
      <div className="startup-splash-status">
        <span key={status}>{status}</span>
      </div>
      <div className="startup-splash-dots" aria-hidden="true"><i /><i /><i /></div>
    </div>
  );
}

// The Cage logo redrawn on its pixel grid so each part can move: the crab
// bobs and blinks behind the bars, which drop in when the splash appears.
function CageMark() {
  return (
    <svg className="cage-mark" viewBox="0 0 100 100" aria-hidden="true">
      <g className="cage-mark-crab">
        <rect x="20.5" y="29.5" width="59.2" height="37" />
        <rect x="12.5" y="47.5" width="8.5" height="10" />
        <rect x="79.2" y="47.5" width="8.3" height="10" />
        <g className="cage-mark-legs-a">
          <rect x="24.8" y="66" width="4.6" height="9" />
          <rect x="57.3" y="66" width="5.9" height="9" />
        </g>
        <g className="cage-mark-legs-b">
          <rect x="36.5" y="66" width="5.8" height="9" />
          <rect x="70.6" y="66" width="4.6" height="9" />
        </g>
        <g className="cage-mark-eyes">
          <rect x="36.5" y="37.2" width="5.6" height="10.3" />
          <rect x="58" y="37.2" width="5.6" height="10.3" />
        </g>
      </g>
      <rect className="cage-mark-frame" x="7.3" y="12.3" width="85.4" height="73.6" pathLength="100" />
      <rect className="cage-mark-bar cage-mark-bar-1" x="29" y="12" width="5.8" height="74" />
      <rect className="cage-mark-bar cage-mark-bar-2" x="65.2" y="12" width="5.8" height="74" />
    </svg>
  );
}
