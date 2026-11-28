import React, { useEffect, useRef, useState } from "react";
import { SafetyMessage } from "./safety-message";
import "./setup-progress.css";
import { useTranslation } from "./interface-language.mjs";
import { localizeBackendMessage } from "./backend-message.mjs";
import { ActionButton } from "./action-button";
import { FailureCard } from "./failure-card";
import { SETUP_STEPS, setupPercent } from "./setup-weight.mjs";

// The four columns stay as they were: a line over each, the current one
// darker (user, demo v8).
const stages = [
  ["image", "系统镜像"],
  ["packages", "系统软件"],
  ["tools", "开发工具"],
  ["services", "环境服务"],
];

function bytes(value) {
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) { value /= 1024; unit++; }
  return `${Number(value.toFixed(unit && value < 100 ? 1 : 0))} ${units[unit]}`;
}

// SetupProgress is the install card (design agreed 2026-11-28, demo v6–v8):
// the step's name, the whole install's percent (setup-weight.mjs, only
// forward), a bar of the part done and the current step's segment with a
// sweep. A failure stops the bar, the segment turns red and the failure shows
// in three lines; 「继续安装」 carries the install on. checking: the install
// is done and the network check runs; finished: everything passed, the
// button turns ✓完成 and then 进入工作台 (onEnter).
export function SetupProgress({ job, onStop, onRetry, onTool, onEnter, disconnected, checking = false, finished = false, entering = false }) {
  const tr = useTranslation();
  const progress = job.setup_progress || {};
  const { phase, label, received, total, bytes_per_second, operation } = progress;
  const native = job.kind === "guest-install";
  const failed = Boolean(job.error);
  const memory = useRef({ id: job.id, step: -1, percent: 0 });
  if (memory.current.id !== job.id) memory.current = { id: job.id, step: -1, percent: 0 };
  const result = finished ? { step: memory.current.step, done: 100, current: 0, percent: 100 }
    : checking ? { step: memory.current.step, done: Math.max(memory.current.percent, 96), current: 100 - Math.max(memory.current.percent, 96), percent: Math.max(memory.current.percent, 96) }
      : setupPercent(progress, memory.current);
  memory.current = { ...memory.current, step: result.step, percent: result.percent };
  const hasBytes = Number.isFinite(received) && received >= 0;
  const knownSize = hasBytes && Number.isFinite(total) && total > 0 && received <= total;
  const meter = knownSize ? `${bytes(received)} / ${bytes(total)}` : hasBytes ? tr("已接收 {0}", bytes(received)) : "";
  const speed = hasBytes && operation !== "verify" && Number.isFinite(bytes_per_second) && bytes_per_second > 0 ? ` · ${bytes(bytes_per_second)}/s` : "";
  const meta = finished ? tr("{0}/{0} 步", SETUP_STEPS) : meter ? meter + speed : result.step >= 0 ? tr("第 {0}/{1} 步", result.step + 1, SETUP_STEPS) : " ";
  const title = finished ? tr("安装与检查") : checking ? tr("检查网络") : label ? localizeBackendMessage(label, tr) : tr("正在准备环境");
  const current = finished ? null : checking ? "services" : phase;
  // 停止安装 → ✓完成 → 进入工作台.
  const [ending, setEnding] = useState("");
  useEffect(() => {
    if (!finished) { setEnding(""); return; }
    setEnding("done");
    const timer = setTimeout(() => setEnding("enter"), 1200);
    return () => clearTimeout(timer);
  }, [finished]);
  return <section className={`setup-progress${failed ? " setup-progress-failed" : ""}`} aria-label={tr("环境安装进度")}>
    <ol className="setup-stages" aria-label={tr("安装阶段")}>
      {stages.map(([key, name]) => <li key={key} aria-current={key === current ? "step" : undefined}>{native && key === "image" ? tr("安装材料") : native && key === "packages" ? tr("系统安装") : tr(name)}</li>)}
    </ol>
    <div className="setup-transfer">
      <div className="setup-transfer-heading">
        <div className="setup-transfer-description">
          <h2 key={title} className="setup-step-name">{title}</h2>
          {/* Always one line, so steps with and without byte counts keep the card's height. */}
          <span className="setup-transfer-bytes">{meta}</span>
        </div>
        <strong className="setup-transfer-number">{result.percent}<small>%</small></strong>
      </div>
      <div role="progressbar" aria-label={tr("安装进度")} aria-valuemin={0} aria-valuemax={100} aria-valuenow={result.percent}
        aria-valuetext={meter ? `${result.percent}%，${meter}` : `${result.percent}%`}
        className={`setup-meter${failed ? " failed" : ""}`}>
        <span className="setup-meter-done" style={{ width: `${result.done}%` }} />
        {result.current > 0 && <span className="setup-meter-current" style={{ left: `${result.done}%`, width: `${result.current}%` }} />}
      </div>
      {failed && (job.failure
        ? <FailureCard compact failure={job.failure} onTool={onTool} hide={["settings:project", "settings:resources"]} labels={{ "settings:network": "网络代理", "settings:dns": "网络代理" }} />
        : <p className="setup-transfer-error" role="alert"><SafetyMessage message={job.error} /></p>)}
    </div>
    <div className="setup-transfer-actions">
      {finished
        ? ending === "enter"
          ? <ActionButton className="primary" phase={entering ? "busy" : "idle"} busyLabel={tr("正在进入…")} disabled={disconnected} onClick={onEnter}>{tr("进入工作台")}</ActionButton>
          : <ActionButton className="primary" phase="done">{tr("停止安装")}</ActionButton>
        : failed
          ? !job.failure?.tools?.length && <button type="button" className="primary" onClick={onRetry} disabled={disconnected}>{tr("继续安装")}</button>
          : <button type="button" onClick={onStop} disabled={disconnected || checking}>{tr("停止安装")}</button>}
    </div>
  </section>;
}
