import React, { useEffect, useRef, useState } from "react";
import { ActionButton } from "./action-button";
import { useTranslation } from "./interface-language.mjs";
import { RouteText } from "./route-text";
import "./settings-submit-bar.css";

// The one place a settings page commits (design agreed 2026-11-28, demo
// v5–v11): a bar above the global status bar that rises only while there is
// something to apply. 「待应用：…」, 撤销修改 and the button whose name says
// what it will do (应用并重启 / 保存 / 应用 / 保存并启动环境). The progress of
// applying shows in the status bar; 完成 and 失败 show on the button only.
// attention counts the ← clicks refused while changes wait (settings.jsx
// close): each one flashes the bar (user, 2026-11-28: no dialog).
export function SettingsSubmitBar({
  open,
  summary,
  label,
  busyLabel,
  phase = "idle",
  disabled = false,
  disabledReason = "",
  onApply,
  onUndo,
  attention = 0,
}) {
  const tr = useTranslation();
  const bar = useRef(null);
  // A screen reader hears why ← stayed: the flash is only seen. Emptied
  // first, so a repeated click is heard again.
  const [said, setSaid] = useState("");
  useEffect(() => {
    const element = bar.current;
    if (!attention || !element) return;
    element.classList.remove("is-attention");
    void element.offsetWidth; // restart the flash on a repeated click
    element.classList.add("is-attention");
    setSaid("");
    const speak = setTimeout(() => setSaid(tr("有待应用的修改")), 60);
    // A timer, not animationend: with reduced motion there is no animation.
    const timer = setTimeout(() => element.classList.remove("is-attention"), 950);
    return () => { clearTimeout(timer); clearTimeout(speak); };
  }, [attention]);
  const shown = open || phase !== "idle";
  const busy = phase === "busy";
  return <div className={`settings-submit${shown ? " open" : ""}`} aria-hidden={!shown}>
    <div className="settings-submit-inner">
      <div ref={bar} className="settings-submit-bar" role="region" aria-label={tr("待应用的修改")}>
        <span className="settings-submit-announcer" role="status" aria-live="polite">{said}</span>
        <span className="settings-submit-text" title={summary}>{summary && <>{tr("待应用：")}<b><RouteText text={summary} /></b></>}</span>
        <span className="settings-submit-actions">
          <button type="button" className="text-button" disabled={busy || !open} onClick={onUndo}>{tr("撤销修改")}</button>
          <ActionButton className="primary" phase={phase} busyLabel={busyLabel} disabled={disabled || !open}
            title={disabled && disabledReason ? disabledReason : undefined} onClick={onApply}>
            {label}
          </ActionButton>
        </span>
      </div>
    </div>
  </div>;
}
