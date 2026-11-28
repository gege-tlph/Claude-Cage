import React, { useSyncExternalStore } from "react";
import { useTranslation } from "./interface-language.mjs";
import { LOG_LEVELS, normalLogLevel } from "./status-message.mjs";

// The level of the 操作日志 window's advanced view: its events at this level
// or above. Everything is still recorded at every level for the diagnostics
// package; this only chooses what the window shows, on this computer.
const key = "cage-log-level";
const changed = "cage-log-level";

export function readLogLevel() {
  try { return normalLogLevel(localStorage.getItem(key)); } catch { return "info"; }
}

function subscribe(listener) {
  const storage = (event) => { if (event.key === key) listener(); };
  window.addEventListener("storage", storage);
  window.addEventListener(changed, listener);
  return () => {
    window.removeEventListener("storage", storage);
    window.removeEventListener(changed, listener);
  };
}

export function useLogLevel() {
  return useSyncExternalStore(subscribe, readLogLevel, () => "info");
}

function setLogLevel(level) {
  try { localStorage.setItem(key, normalLogLevel(level)); } catch { /* kept for this window only */ }
  window.dispatchEvent(new Event(changed));
}

const labels = { debug: "调试", info: "信息", warn: "警告", error: "错误" };

// LogLevelSettings is the settings row; it applies at once.
export function LogLevelSettings() {
  const tr = useTranslation();
  const level = useLogLevel();
  return <section className="settings-group" aria-label={tr("操作日志")}>
    <h3>{tr("操作日志")}</h3>
    <div className="settings-card">
      <div className="settings-row">
        <div className="settings-row-copy"><strong>{tr("日志等级")}</strong></div>
        <div className="settings-segmented" role="group" aria-label={tr("日志等级")}>
          {LOG_LEVELS.map((value) => (
            <button key={value} type="button" aria-pressed={level === value} onClick={() => setLogLevel(value)}>{tr(labels[value])}</button>
          ))}
        </div>
      </div>
    </div>
  </section>;
}
