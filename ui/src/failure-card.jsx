import React, { useState } from "react";
import { ArrowDownToLine, Cpu, Globe2, Power, RotateCcw, RotateCw } from "lucide-react";
import { ActionButton, usePhase } from "./action-button";
import { StatusIcon } from "./status-icon";
import { exportDiagnostics } from "./diagnostics.jsx";
import { isRemoteClient } from "./client-context.mjs";
import { localizeBackendMessage } from "./backend-message.mjs";
import { useTranslation } from "./interface-language.mjs";
import "./failure-card.css";

// The levels and their circle: 严重 red !, 警告 orange !, 提示 blue i.
const ICON_STATE = { error: "fail", warn: "warn", info: "info" };

function LogIcon() {
  return <svg viewBox="0 0 16 16" width="15" height="15" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" aria-hidden="true">
    <rect x="2.5" y="2" width="11" height="12" rx="2" /><path d="M5 5.5h6M5 8h6M5 10.5h3.5" />
  </svg>;
}

// The tools a failure can offer, by the backend's name. waits: the button
// spins until the action ends (or the card goes away with a new job).
const TOOLS = {
  start: { label: "启动环境", icon: <Power size={15} />, busy: "启动中", waits: true },
  stop: { label: "停止环境", icon: <Power size={15} />, busy: "停止中", waits: true },
  restart: { label: "重启环境", icon: <RotateCw size={15} />, busy: "重启中", waits: true },
  "continue-install": { label: "继续安装", icon: <Power size={15} />, busy: "启动中", waits: true },
  diagnostics: { label: "导出诊断包", icon: <ArrowDownToLine size={15} />, busy: "导出中", waits: true },
  log: { label: "操作日志", icon: <LogIcon /> },
  "settings:network": { label: "网络与代理", icon: <Globe2 size={15} />, settings: true },
  "settings:project": { label: "环境与恢复", icon: <RotateCcw size={15} />, settings: true },
  "settings:resources": { label: "计算资源", icon: <Cpu size={15} />, settings: true },
};

// openOperationLog asks the status bar to open the 操作日志 window.
export function openOperationLog() {
  window.dispatchEvent(new Event("cage-open-operation-log"));
}

function ToolButton({ name, primary, onTool }) {
  const tr = useTranslation();
  const tool = TOOLS[name];
  const phase = usePhase();
  const run = async () => {
    phase.start();
    try {
      if (name === "diagnostics") {
        // A canceled save dialog is no result.
        if (!(await exportDiagnostics())) return phase.reset();
      } else await onTool(name);
      phase.finish(true);
    } catch {
      phase.finish(false);
    }
  };
  if (!tool) return null;
  const label = name === "diagnostics" && isRemoteClient() ? tr("导出服务器诊断包") : tr(tool.label);
  if (!tool.waits) {
    return <button type="button" className={`failure-tool${primary ? " primary" : ""}`} onClick={() => (name === "log" ? openOperationLog() : onTool(name))}>
      <span className="failure-tool-icon" aria-hidden="true">{tool.icon}</span>{label}
    </button>;
  }
  return <ActionButton className={`failure-tool${primary ? " primary" : ""}`} phase={phase.phase} icon={tool.icon} busyLabel={tr(tool.busy)}
    onClick={run}>{label}</ActionButton>;
}

// localizeFailure translates a failure's text: the backend's templates, then
// the stage of 「卡在{阶段}，…」 and the units of waits (「10 分钟」).
function localizeFailure(text, tr) {
  const stuck = /^卡在(.+?)，(.+)$/.exec(text);
  const out = stuck
    ? tr("卡在{0}，{1}", localizeBackendMessage(stuck[1], tr), localizeBackendMessage(stuck[2], tr))
    : localizeBackendMessage(text, tr);
  return out.replace(/(\d+(?:\.\d+)?) (小时|分钟|分|秒)/g, (_, n, unit) => tr(`{0} ${unit}`, n));
}

// Keeps a value in parentheses on one line: 「（10 分钟）」.
function Unbroken({ text }) {
  const parts = text.split(/(（[^）]{1,24}）|\([^)]{1,24}\))/);
  return parts.map((part, index) => index % 2 ? <span key={index} className="failure-nowrap">{part}</span> : part);
}

// FailureCard shows a job's failure in three lines: what failed and why, the
// suggestion (下一步 for a notice), and tool buttons, the suggested one first.
// Its color follows the level. onTool(name) runs start, stop, restart,
// continue-install and the settings tools, each carrying the whole flow
// through; tools listed in hide are left out. After a detour into settings,
// the button that carries on (start, restart, continue) comes first.
export function FailureCard({ failure, onTool, hide = [] }) {
  const tr = useTranslation();
  const [detoured, setDetoured] = useState(false);
  const level = ICON_STATE[failure.level] ? failure.level : "error";
  let tools = (failure.tools || []).filter((name) => TOOLS[name] && !hide.includes(name));
  if (detoured) tools = [...tools.filter((name) => TOOLS[name].waits && name !== "diagnostics"), ...tools.filter((name) => !TOOLS[name].waits || name === "diagnostics")];
  const runTool = (name) => {
    if (TOOLS[name]?.settings) setDetoured(true);
    return onTool(name);
  };
  const reason = failure.reason ? localizeFailure(failure.reason, tr) : "";
  return <section className="failure-card" data-level={level} role="alert">
    <StatusIcon state={ICON_STATE[level]} size={20} className="failure-card-icon" />
    <p className="failure-card-lead">
      <b>{localizeBackendMessage(failure.lead, tr)}</b>
      {reason && <>{tr("，")}<Unbroken text={reason} /></>}
      {failure.raw && <span className="failure-card-raw">{tr("（最后一条错误：{0}）", failure.raw)}</span>}
    </p>
    {failure.next && <p className="failure-card-next">
      <span>{tr(level === "info" ? "下一步" : "解决建议")}{tr("：")}</span>{localizeFailure(failure.next, tr)}
    </p>}
    {tools.length > 0 && <div className="failure-card-tools">
      {tools.map((name, index) => <ToolButton key={name} name={name} primary={index === 0} onTool={runTool} />)}
    </div>}
  </section>;
}
