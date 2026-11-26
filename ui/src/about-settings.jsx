import React, { useEffect, useRef, useState } from "react";
import { ExternalLink, LoaderCircle, RefreshCw } from "lucide-react";
import { api, native, nativeAvailable } from "./api";
import "./about-settings.css";
import { activeInterfaceLanguage, useTranslation } from "./interface-language.mjs";
import { localizeBackendMessage } from "./backend-message.mjs";

const repository = "https://github.com/gege-tlph/Claude-Cage";
const releaseMessages = {
  current: "已是最新版本",
  available: "发现新发行版",
  ahead: "当前版本高于最新发行版",
  unknown: "当前版本无法比较，请核对发行说明",
  missing: "暂无可比较的公开发行版",
};
const activePhases = new Set(["checking", "downloading", "downloaded", "verifying", "installing"]);

// The App's own updater, the one behind the sidebar's update button, read as
// one release result: About shows the same state and does not ask GitHub
// again on its own.
function updaterResult(state) {
  if (state.phase === "failed" || state.phase === "waiting")
    return { status: "failed", error: state.message || "更新未完成", retryAt: state.phase === "failed" ? state.retryAt : "" };
  if (state.phase === "current") return { status: "current", version: state.currentVersion };
  if (state.latestVersion && ["available", "ready", "manual", ...activePhases].includes(state.phase))
    return { status: "available", version: state.latestVersion };
  return null;
}

function ReleasesLink({ onError }) {
  const tr = useTranslation();
  return <a href={`${repository}/releases`} target="_blank" rel="noopener noreferrer" onClick={async event => {
    if (!nativeAvailable()) return;
    event.preventDefault();
    try { await native("appReference", { kind: "releases" }); }
    catch (error) { onError(error.message); }
  }}>{tr("查看发行说明与下载")}<ExternalLink size={13} aria-hidden="true" /></a>;
}

// What the sidebar's button does next, as text.
function UpdaterAction({ updates }) {
  const tr = useTranslation();
  const { state, run } = updates;
  if (state.phase === "downloading") {
    const percent = state.totalBytes > 0 ? Math.max(0, Math.min(100, Math.floor((state.receivedBytes || 0) / state.totalBytes * 100))) : null;
    return <p className="about-result-detail" role="status">{percent === null ? tr("正在下载更新") : tr("正在下载更新 {0}%", percent)}</p>;
  }
  if (state.phase === "verifying" || state.phase === "downloaded") return <p className="about-result-detail" role="status">{tr("正在验证更新")}</p>;
  if (state.phase === "installing") return <p className="about-result-detail" role="status">{tr("正在安装更新，完成后自动重启")}</p>;
  if (state.phase === "available" || state.phase === "ready")
    return <button className="secondary-button" onClick={() => void run("install")}>{state.phase === "ready" ? tr("安装已下载的更新") : tr("下载更新")}</button>;
  return null;
}

function ReleaseResult({ item, updates, onError }) {
  const tr = useTranslation();
  return <section className="about-result" aria-label={tr("发行版更新")}>
    <h4>{tr("发行版更新")}</h4>
    {!item ? <p className="footnote">{tr("尚未检查")}</p> : <>
      <p className={item.error ? "error-message" : "about-result-status"}>
        {item.error ? localizeBackendMessage(item.error, tr) : tr(releaseMessages[item.status])}
      </p>
      {item.retryAt && <p className="footnote">{tr("可于") + " "}{new Date(item.retryAt).toLocaleString(activeInterfaceLanguage())}{" " + tr("后重试")}</p>}
      {item.version && <p className="about-result-detail">{item.version} · {item.prerelease ? tr("预发行版") : tr("正式版")}</p>}
      {updates && <UpdaterAction updates={updates} />}
    </>}
    <ReleasesLink onError={onError} />
  </section>;
}

// updates: the App's updater when this page runs in the App; a browser or
// remote client checks through the controller instead.
export function AboutSettings({ updates }) {
  const tr = useTranslation();
  const own = updates?.available ? updates : null;
  const [build, setBuild] = useState(null);
  const [result, setResult] = useState(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [clock, setClock] = useState(Date.now);
  const [checkedAt, setCheckedAt] = useState("");
  const item = own ? updaterResult(own.state) : result?.release;
  const checking = own ? own.state.phase === "checking" : busy;
  const retryAt = Date.parse(item?.retryAt || "") || 0;
  const coolingDown = own ? own.coolingDown : retryAt > clock;
  useEffect(() => {
    if (own || !coolingDown) return;
    const timer = setTimeout(() => setClock(Date.now()), Math.min(60000, retryAt - Date.now()));
    return () => clearTimeout(timer);
  }, [clock, coolingDown, retryAt, own]);
  const request = useRef(null);
  useEffect(() => {
    const controller = new AbortController();
    api("/client/about", undefined, undefined, {}, { signal: controller.signal })
      .then(setBuild).catch(error => { if (!controller.signal.aborted) setError(error.message); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => { controller.abort(); request.current?.abort(); };
  }, []);
  const check = async () => {
    setError("");
    if (own) {
      if (activePhases.has(own.state.phase) || own.coolingDown) return;
      await own.run("check");
      setCheckedAt(new Date().toISOString());
      return;
    }
    if (request.current || retryAt > Date.now()) return;
    const controller = new AbortController();
    request.current = controller;
    setBusy(true); setResult(null);
    try {
      const next = await api("/client/about", {}, undefined, {}, { signal: controller.signal });
      setBuild(next.build); setResult(next); setCheckedAt(next.checkedAt || ""); setClock(Date.now());
    } catch (error) { if (!controller.signal.aborted) setError(error.message); }
    finally {
      request.current = null;
      if (!controller.signal.aborted) setBusy(false);
    }
  };
  return <section className="about-settings" aria-label={tr("关于 Cage lite")}>
    <div className="section-title"><h3>{tr("关于")}</h3><p>{tr("Cage lite 的版本与更新信息")}</p></div>
    <dl className="about-build">
      <div><dt>{tr("软件版本")}</dt><dd>{loading ? tr("正在读取…") : build?.version || tr("未知")}</dd></div>
      <div><dt>{tr("构建提交")}</dt><dd>{build?.commit ? <code title={build.commit}>{build.commit.slice(0, 12)}</code> : loading ? tr("正在读取…") : tr("未记录")}</dd></div>
    </dl>
    {build?.modified && <p className="footnote">{tr("此构建包含未提交的修改")}</p>}
    <div className="about-check-actions">
      <button className="secondary-button" onClick={check} disabled={checking || coolingDown || (own && activePhases.has(own.state.phase))}>
        {checking ? <LoaderCircle size={15} className="spin" /> : <RefreshCw size={15} />}
        {checking ? tr("正在检查…") : coolingDown ? tr("等待重试") : tr("检测更新")}
      </button>
      {checkedAt && <span className="footnote">{tr("检查时间：")}{new Date(checkedAt).toLocaleString(activeInterfaceLanguage())}</span>}
    </div>
    {error && <p className="error-message" role="alert">{localizeBackendMessage(error, tr)}</p>}
    <div className="about-results" aria-live="polite" aria-busy={checking}>
      <ReleaseResult item={item} updates={own} onError={setError} />
    </div>
  </section>;
}
