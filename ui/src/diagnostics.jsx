import React, { useEffect, useState } from "react";
import { ArrowDownToLine, LoaderCircle } from "lucide-react";
import { api, native, nativeAvailable } from "./api";
import { reportDiagnostic } from "./diagnostics.mjs";
import { isRemoteClient, runtimeURL } from "./client-context.mjs";
import { useTranslation, translate } from "./interface-language.mjs";

// exportDiagnostics saves or downloads the diagnostics package. It returns
// the notice to show, or "" when the save dialog was canceled.
export async function exportDiagnostics() {
  if (nativeAvailable()) {
    const result = await native("exportDiagnostics");
    return result?.canceled ? "" : "诊断包已保存。分享前请检查其中内容。";
  }
  // Fetch before downloading so authentication or disk failures are shown.
  const response = await fetch(runtimeURL("/diagnostics/export"));
  if (!response.ok) throw new Error((await response.text()).trim() || "诊断包导出失败");
  if (!response.headers.get("content-type")?.startsWith("application/zip")) throw new Error("当前后台不支持诊断导出，请重新打开 Cage lite");
  const url = URL.createObjectURL(await response.blob());
  const link = document.createElement("a"); link.href = url; link.download = "cage-diagnostics.zip"; link.click();
  setTimeout(() => URL.revokeObjectURL(url), 30_000);
  return "诊断包已下载。分享前请检查其中内容。";
}

export function DiagnosticSettings() {
  const tr = useTranslation();
  const remote = isRemoteClient();
  const [info, setInfo] = useState(null);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  useEffect(() => {
    let live = true;
    api("/diagnostics").then(value => { if (live) setInfo(value); })
      .catch(error => { if (live) setError(error.message); });
    return () => { live = false; };
  }, []);
  const save = async () => {
    setBusy(true); setError(""); setNotice("");
    try {
      const notice = await exportDiagnostics();
      if (notice) setNotice(notice);
    } catch (error) { setError(error.message); }
    finally { setBusy(false); }
  };
  return <section className="settings-group" aria-label={tr("诊断包")}>
    <h3>{tr("诊断包")}</h3>
    {info && <p className="footnote">{remote ? tr("服务器") : tr("本机")}{tr("日志") + " "}{(info.bytes / 1024 / 1024).toFixed(1)} MiB · {info.files}{" " + tr("个文件")}</p>}
    <button className="secondary-button" disabled={busy} onClick={save}>
      {busy ? <LoaderCircle size={14} className="spin" /> : <ArrowDownToLine size={14} />}
      {busy ? tr("正在导出…") : remote ? tr("导出服务器诊断包") : tr("导出诊断包")}
    </button>
    {notice && <p role="status">{tr(notice)}</p>}
    {error && <p role="alert" className="error-message">{tr(error)}</p>}
  </section>;
}

export class DiagnosticBoundary extends React.Component {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  componentDidCatch(error, info) { reportDiagnostic("react", new Error(`${error.message}\n${error.stack || ""}\n${info.componentStack || ""}`)); }
  render() {
    if (!this.state.failed) return this.props.children;
    return <main style={{ padding: 32 }}>
      <h2>{translate("工作台显示遇到异常")}</h2>
      <p>{translate("后台任务仍可继续运行。可以导出诊断包，再重新加载界面。")}</p>
      <DiagnosticSettings />
      <button className="primary-button" onClick={() => location.reload()}>{translate("重新加载界面")}</button>
    </main>;
  }
}
