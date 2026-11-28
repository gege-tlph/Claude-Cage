import React, { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Copy, X } from "lucide-react";
import { api, writeClipboard } from "./api";
import { isRemoteClient } from "./client-context.mjs";
import { exportDiagnostics } from "./diagnostics.jsx";
import { ActionButton, usePhase } from "./action-button";
import { localizeBackendMessage } from "./backend-message.mjs";
import { useTranslation } from "./interface-language.mjs";
import { HOLD_MS } from "./status-phase.mjs";
import { eventLog, jobNames, logGroups, logTime, rawLog, simpleLog } from "./status-message.mjs";
import { useLogLevel } from "./log-level";

const viewKey = "cage-operation-log-view";

// CopyButton sits in the log's top-right corner; once copied, its icon turns
// into a green check for a moment.
function CopyButton({ text, onError }) {
  const tr = useTranslation();
  const [copied, setCopied] = useState(false);
  const timer = useRef(0);
  useEffect(() => () => clearTimeout(timer.current), []);
  const copy = async () => {
    try {
      await writeClipboard(text);
      setCopied(true);
      clearTimeout(timer.current);
      timer.current = setTimeout(() => setCopied(false), HOLD_MS);
    } catch (reason) { onError(reason.message); }
  };
  const label = copied ? tr("已复制") : tr("复制");
  return <button type="button" className={`operation-log-copy${copied ? " is-copied" : ""}`} disabled={!text}
    aria-label={label} title={label} onClick={copy}>
    <Copy size={14} className="operation-log-copy-icon" aria-hidden="true" />
    <svg className="operation-log-copy-check" viewBox="0 0 16 16" aria-hidden="true"><path pathLength="1" d="M3.5 8.4 6.6 11.4 12.5 5" /></svg>
  </button>;
}

function savedView() {
  try { return localStorage.getItem(viewKey) === "raw" ? "raw" : "simple"; } catch { return "simple"; }
}

// OperationLog is the 操作日志 window above the status bar: this run's last
// hour of operations, as short timed lines (简易), or as every recorded event
// at the level chosen in settings, with its source (高级). After a failure it
// offers the diagnostics package. It adds no masking of its own: it only
// shows on this screen, on request.
export function OperationLog({ job, exportAlways = false, anchor, onClose }) {
  const tr = useTranslation();
  const [view, setView] = useState(savedView);
  const level = useLogLevel();
  const [log, setLog] = useState(null);
  const [error, setError] = useState("");
  const [exportError, setExportError] = useState("");
  const exportPhase = usePhase();
  const box = useRef(null), body = useRef(null), stick = useRef(true);
  const failed = Boolean(job?.error && !job.running && !job.canceled);

  // Read again whenever the job moves on; while it runs, also on a timer. The
  // advanced view's events keep coming without a job, so it always reads on.
  const revision = `${job?.id}|${job?.running}|${job?.message}|${job?.error}|${job?.log?.length || 0}`;
  const raw = view === "raw";
  useEffect(() => {
    let live = true, busy = false;
    const read = async () => {
      if (busy) return;
      busy = true;
      try {
        const next = await api(raw ? `/jobs/log?level=${level}` : "/jobs/log");
        if (live) { setLog({ ...next, raw }); setError(""); }
      } catch (reason) {
        if (live) setError(reason.message);
      } finally { busy = false; }
    };
    read();
    const timer = job?.running || raw ? setInterval(read, 2000) : null;
    return () => { live = false; clearInterval(timer); };
  }, [revision, job?.running, raw, level]);

  useEffect(() => {
    box.current?.focus({ preventScroll: true });
    const close = (event) => {
      if (event.type === "keydown" && event.key !== "Escape") return;
      // A dialog opened over the log closes first.
      if (document.querySelector(".modal-scrim")) return;
      event.preventDefault();
      onClose(true);
    };
    const outside = (event) => {
      if (box.current?.contains(event.target) || anchor?.current?.contains(event.target)) return;
      onClose(false);
    };
    document.addEventListener("keydown", close);
    document.addEventListener("cage-close-modal", close);
    document.addEventListener("pointerdown", outside, true);
    return () => {
      document.removeEventListener("keydown", close);
      document.removeEventListener("cage-close-modal", close);
      document.removeEventListener("pointerdown", outside, true);
    };
  }, [onClose, anchor]);

  const entries = log?.entries || [];
  // A backend without events shows each job's output instead.
  const events = log?.raw ? log.events : null;
  const rawText = !log?.raw ? "" : events ? eventLog(events) : rawLog(log.outputs, jobNames(entries));
  // Follow new lines while the view is scrolled to the end.
  useLayoutEffect(() => {
    const element = body.current;
    if (element && stick.current) element.scrollTop = element.scrollHeight;
  }, [log, view]);
  const scrolled = () => {
    const element = body.current;
    if (element) stick.current = element.scrollHeight - element.scrollTop - element.clientHeight < 24;
  };

  const choose = (next) => {
    setView(next);
    stick.current = true;
    try { localStorage.setItem(viewKey, next); } catch { /* the choice is only for this window */ }
  };
  const save = async () => {
    setExportError("");
    exportPhase.start();
    try {
      const notice = await exportDiagnostics();
      if (notice) exportPhase.finish(true);
      else exportPhase.reset();
    } catch (reason) {
      exportPhase.finish(false);
      setExportError(reason.message);
    }
  };

  const empty = <p className="operation-log-empty">{tr("暂无日志")}</p>;
  return createPortal(
    <section ref={box} className="operation-log" role="dialog" aria-label={tr("操作日志")} tabIndex={-1}>
      <header className="operation-log-head">
        <h2>{tr("操作日志")}</h2>
        <span className="operation-log-views" role="group" aria-label={tr("操作日志")}>
          <button type="button" aria-pressed={view === "simple"} onClick={() => choose("simple")}>{tr("简易")}</button>
          <button type="button" aria-pressed={view === "raw"} onClick={() => choose("raw")}>{tr("高级")}</button>
        </span>
        <button type="button" className="operation-log-close" aria-label={tr("关闭")} title={tr("关闭")} onClick={() => onClose(true)}><X size={15} /></button>
      </header>
      {error && <p className="operation-log-error" role="alert">{localizeBackendMessage(error, tr)}</p>}
      <div className="operation-log-content">
      {view === "simple"
        ? <div ref={body} className="operation-log-body" onScroll={scrolled}>
          {entries.length ? logGroups(entries).map((group, index) => (
            <ol key={`${group.job}:${index}`} className="operation-log-job">
              {group.entries.map((entry, line) => (
                <li key={line} className={line === 0 ? "operation-log-name" : entry.state ? `operation-log-${entry.state}` : undefined}>
                  <time dateTime={entry.time}>{logTime(entry.time)}</time>
                  <span>{localizeBackendMessage(entry.message, tr)}</span>
                </li>
              ))}
            </ol>
          )) : log && empty}
        </div>
        : <div ref={body} className="operation-log-body operation-log-raw" onScroll={scrolled}>
          {events?.length ? <pre>{events.map((event, index) => (
            <span key={index} className={`operation-log-event is-${event.level}`}>
              <time dateTime={event.time}>{logTime(event.time, true)}</time>
              {" "}<b>{String(event.level).toUpperCase().padEnd(5)}</b>
              {" "}<i>{event.source}</i>
              {" "}{event.message}{"\n"}
            </span>
          ))}</pre> : rawText ? <pre>{rawText}</pre> : log?.raw && empty}
        </div>}
        <CopyButton text={raw ? rawText : simpleLog(entries, (message) => localizeBackendMessage(message, tr))} onError={setError} />
      </div>
      {(failed || exportAlways) && <footer className="operation-log-foot">
        {exportError && <span className="operation-log-error" role="alert">{localizeBackendMessage(exportError, tr)}</span>}
        <ActionButton phase={exportPhase.phase} onClick={save}>
          {isRemoteClient() ? tr("导出服务器诊断包") : tr("导出诊断包")}
        </ActionButton>
      </footer>}
    </section>,
    document.body,
  );
}
