import React, { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { CLOSE_MS, createPhase, reducedMotion } from "./status-phase.mjs";
import { StatusIcon } from "./status-icon";
import { useTranslation } from "./interface-language.mjs";
import "./status-motion.css";

// usePhase drives a button whose work finishes elsewhere (a background job
// read from the status poll): start() when it begins, finish(ok) when it ends.
export function usePhase() {
  const [phase, setPhase] = useState("idle");
  const driver = useRef(null);
  if (!driver.current) driver.current = createPhase(setPhase);
  useEffect(() => () => driver.current.dispose(), []);
  const start = useCallback(() => driver.current.start(), []);
  const finish = useCallback((ok, onIdle) => driver.current.finish(ok, onIdle), []);
  const reset = useCallback(() => driver.current.reset(), []);
  return { phase, start, finish, reset };
}

// useAction wraps an async action: it fails when it throws or returns false.
// The action's own result is returned; errors are rethrown after the button
// shows 失败.
export function useAction(action) {
  const { phase, start, finish, reset } = usePhase();
  const latest = useRef(action);
  latest.current = action;
  const run = useCallback(async (...args) => {
    start();
    try {
      const result = await latest.current(...args);
      finish(result !== false);
      return result;
    } catch (error) {
      finish(false);
      throw error;
    }
  }, [start, finish]);
  return { phase, run, reset };
}

// ActionButton is a button for an action the user waits for (network or the
// environment). At rest it has no icon; busy shows the spinning arc with
// busyLabel; the result shows 完成 or 失败 on the button only, then the label
// comes back. The width follows the text. Busy buttons keep their colour.
export function ActionButton({
  phase = "idle",
  children,
  busyLabel,
  doneLabel,
  failLabel,
  className = "",
  disabled,
  onClick,
  ...rest
}) {
  const tr = useTranslation();
  const ref = useRef(null);
  // The text changes to 完成 / 失败 as the mark draws, after the arc closed.
  const [textPhase, setTextPhase] = useState(phase);
  const previous = useRef(phase);
  useEffect(() => {
    const was = previous.current;
    previous.current = phase;
    if ((phase === "done" || phase === "fail") && was === "busy" && !reducedMotion()) {
      const timer = setTimeout(() => setTextPhase(phase), CLOSE_MS);
      return () => clearTimeout(timer);
    }
    setTextPhase(phase);
  }, [phase]);
  const text = textPhase === "busy" ? busyLabel || children
    : textPhase === "done" ? doneLabel || tr("完成")
      : textPhase === "fail" ? failLabel || tr("失败")
        : children;
  const withIcon = phase !== "idle";
  const icon = phase === "idle" ? "idle" : phase;
  const width = useRef(0), hadIcon = useRef(withIcon);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const from = width.current, had = hadIcon.current;
    el.classList.add("is-measuring");
    el.style.width = "";
    const to = el.getBoundingClientRect().width;
    width.current = to;
    hadIcon.current = withIcon;
    if (!from || Math.abs(from - to) < 0.5 || reducedMotion()) {
      el.classList.remove("is-measuring");
      return;
    }
    el.classList.toggle("has-icon", had);
    el.style.width = `${from}px`;
    void el.offsetWidth;
    el.classList.remove("is-measuring");
    el.classList.toggle("has-icon", withIcon);
    el.style.width = `${to}px`;
    const timer = setTimeout(() => { el.style.width = ""; }, 240);
    return () => clearTimeout(timer);
  }, [text, withIcon]);
  const working = phase !== "idle";
  return (
    <button
      ref={ref}
      type="button"
      {...rest}
      className={["action-button", className, withIcon && "has-icon"].filter(Boolean).join(" ")}
      aria-busy={working || undefined}
      disabled={disabled || working}
      onClick={working ? undefined : onClick}
    >
      <span className="action-button-slot"><StatusIcon state={icon} /></span>
      <span className="action-button-label" key={text}>{text}</span>
    </button>
  );
}
