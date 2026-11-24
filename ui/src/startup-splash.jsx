import React, { useEffect, useState, useSyncExternalStore } from "react";
import { CageLogo } from "./cage-logo.jsx";
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
        <CageLogo />
      </div>
      <div className="startup-splash-status">
        <span key={status}>{status}</span>
      </div>
      <div className="startup-splash-dots" aria-hidden="true"><i /><i /><i /></div>
    </div>
  );
}
