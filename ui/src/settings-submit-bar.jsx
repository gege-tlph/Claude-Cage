import React from "react";
import { ActionButton } from "./action-button";
import { useTranslation } from "./interface-language.mjs";
import "./settings-submit-bar.css";

// The one place a settings page commits (design agreed 2026-11-28, demo
// v5–v11): a bar above the global status bar that rises only while there is
// something to apply. 「待应用：…」, 撤销修改 and the button whose name says
// what it will do (应用并重启 / 保存 / 应用 / 保存并启动环境). The progress of
// applying shows in the status bar; 完成 and 失败 show on the button only.
//
// leaving: the user asked to leave with changes that cannot apply at once;
// the bar turns into 「有未应用的修改」 with 放弃 and 继续编辑.
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
  leaving = false,
  onDiscard,
  onKeepEditing,
}) {
  const tr = useTranslation();
  const shown = open || leaving || phase !== "idle";
  const busy = phase === "busy";
  return <div className={`settings-submit${shown ? " open" : ""}`} aria-hidden={!shown}>
    <div className="settings-submit-inner">
      <div className="settings-submit-bar" role="region" aria-label={tr("待应用的修改")}>
        {leaving ? <>
          <span className="settings-submit-text"><b>{tr("有未应用的修改")}</b></span>
          <span className="settings-submit-actions">
            <button type="button" onClick={onDiscard}>{tr("放弃")}</button>
            <button type="button" className="primary" autoFocus onClick={onKeepEditing}>{tr("继续编辑")}</button>
          </span>
        </> : <>
          <span className="settings-submit-text" title={summary}>{summary && <>{tr("待应用：")}<b>{summary}</b></>}</span>
          <span className="settings-submit-actions">
            <button type="button" className="text-button" disabled={busy || !open} onClick={onUndo}>{tr("撤销修改")}</button>
            <ActionButton className="primary" phase={phase} busyLabel={busyLabel} disabled={disabled || !open}
              title={disabled && disabledReason ? disabledReason : undefined} onClick={onApply}>
              {label}
            </ActionButton>
          </span>
        </>}
      </div>
    </div>
  </div>;
}
