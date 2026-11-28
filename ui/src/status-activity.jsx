import React, { useCallback, useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore } from "react";
import { createPortal } from "react-dom";
import { OperationLog } from "./operation-log";
import { SafetyChecklistButton } from "./safety-message";
import { localizeBackendMessage } from "./backend-message.mjs";
import { useTranslation } from "./interface-language.mjs";
import { announcement, statusMessage, unreadFailure } from "./status-message.mjs";
import "./status-bar.css";

// The failed job whose log has been opened, shared by every status bar (first
// run and workbench), so the red dot does not come back on the next view.
let seenFailure = "";
const seenListeners = new Set();
const markSeen = (id) => {
  if (!id || id === seenFailure) return;
  seenFailure = id;
  seenListeners.forEach((listener) => listener());
};
const subscribeSeen = (listener) => {
  seenListeners.add(listener);
  return () => seenListeners.delete(listener);
};
const readSeen = () => seenFailure;

// Whether the 操作日志 window is open, for shortcuts that close an overlay
// before a tab.
export const operationLogOpen = () => typeof document !== "undefined" && Boolean(document.querySelector(".operation-log"));

// The status bar's height, for views that stop above it (settings).
function useStatusBarHeight(anchor) {
  useLayoutEffect(() => {
    const bar = anchor.current?.closest(".status-bar");
    if (!bar) return;
    const root = document.documentElement;
    const set = () => root.style.setProperty("--status-bar-height", `${bar.getBoundingClientRect().height}px`);
    set();
    const observer = typeof ResizeObserver === "function" ? new ResizeObserver(set) : null;
    observer?.observe(bar);
    return () => {
      observer?.disconnect();
      root.style.removeProperty("--status-bar-height");
    };
  }, [anchor]);
}

// StatusBarHost keeps the workbench status bar in its place, or, with
// overlay, at the window's bottom edge above a page that covers the workbench
// (settings), outside the workbench that page makes inert. The bar is moved,
// not mounted again, and its place keeps its height, so the workspace and the
// remote display under it do not resize.
export function StatusBarHost({ overlay = false, children }) {
  const [host] = useState(() => {
    const element = document.createElement("div");
    element.className = "status-bar-host";
    return element;
  });
  const slot = useRef(null);
  useLayoutEffect(() => {
    host.classList.toggle("is-overlay", overlay);
    (overlay ? document.body : slot.current)?.appendChild(host);
  }, [overlay, host]);
  useEffect(() => () => host.remove(), [host]);
  return <>
    <div ref={slot} className={`status-bar-slot${overlay ? " is-empty" : ""}`} aria-hidden={overlay || undefined} />
    {createPortal(children, host)}
  </>;
}

function LogIcon() {
  return <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round">
    <rect x="2.5" y="2" width="11" height="12" rx="2" /><path d="M5 5.5h6M5 8h6M5 10.5h3.5" />
  </svg>;
}

// StatusActivity is the status bar's message and its 操作日志 icon. The
// message is plain text: the running job's short stage under a light sweep,
// a failure in red, a notification, or the resting text. Clicking it or the
// icon opens the log; the icon shows a red dot after a failure until then.
// hideMessage keeps only the icon (first run, while the install card shows
// the progress).
export function StatusActivity({ job, toast, toastTone, running, ready, remote, hideMessage = false, exportAlways = false }) {
  const tr = useTranslation();
  const anchor = useRef(null), iconRef = useRef(null), textRef = useRef(null);
  const [open, setOpen] = useState(false);
  const [cut, setCut] = useState(false);
  const [spoken, setSpoken] = useState("");
  const seen = useSyncExternalStore(subscribeSeen, readSeen);
  useStatusBarHeight(anchor);

  const message = statusMessage({ toast, toastTone, job, running, ready, remote });
  const text = message.text ? localizeBackendMessage(message.text, tr) : "";
  const dot = !open && unreadFailure(job, seen);

  // Opening the log, or having it open when the job fails, reads the failure.
  useEffect(() => {
    if (open && unreadFailure(job, seenFailure)) markSeen(job.id);
  }, [open, job?.id, job?.error, job?.running, job?.canceled]);

  // A screen reader hears the start, the end and a failure, and notifications.
  const previous = useRef(null);
  useEffect(() => {
    // Before the first status read there is nothing to compare with.
    if (!job) return;
    const said = announcement(previous.current, job);
    previous.current = { id: job?.id, running: Boolean(job?.running) };
    if (said) setSpoken(localizeBackendMessage(said, tr));
  }, [job?.id, job?.running, job?.error, job?.stage, job?.canceled]);
  useEffect(() => {
    if (toast) setSpoken(localizeBackendMessage(toast, tr));
  }, [toast]);

  // The full text shows on hover only when it is cut.
  const measure = () => {
    const element = textRef.current;
    setCut(Boolean(element && element.scrollWidth > element.clientWidth + 1));
  };

  const close = useCallback((returnFocus) => {
    setOpen(false);
    if (returnFocus) iconRef.current?.focus();
  }, []);

  return <span ref={anchor} className="status-activity">
    {!hideMessage && text && <button
      type="button"
      className={`status-message${message.tone ? ` is-${message.tone}` : ""}`}
      title={cut ? text : undefined}
      aria-label={`${text} · ${tr("操作日志")}`}
      aria-haspopup="dialog"
      onPointerEnter={measure}
      onClick={() => setOpen(true)}
    >
      <span ref={textRef} key={text} className="status-message-text">{text}</span>
    </button>}
    {!hideMessage && message.raw && <SafetyChecklistButton message={message.raw} />}
    <button
      ref={iconRef}
      type="button"
      className="status-log-button"
      aria-label={tr("操作日志")}
      title={tr("操作日志")}
      aria-haspopup="dialog"
      aria-expanded={open}
      onClick={() => setOpen(!open)}
    >
      <LogIcon />
      {dot && <span className="status-log-dot" aria-hidden="true" />}
    </button>
    <span className="status-announcer" role="status" aria-live="polite">{spoken}</span>
    {open && <OperationLog job={job} exportAlways={exportAlways} anchor={anchor} onClose={close} />}
  </span>;
}
