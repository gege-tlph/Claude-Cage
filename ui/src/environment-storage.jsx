import React, { useEffect, useState } from "react";
import { FolderOpen } from "lucide-react";
import { api, native, nativeAvailable } from "./api";
import { SafetyMessage } from "./safety-message";
import { localizeBackendMessage } from "./backend-message.mjs";
import { translate, useTranslation } from "./interface-language.mjs";

export function EnvironmentStorage({ status, onConfirm }) {
  const tr = useTranslation();
  const [location, setLocation] = useState(null);
  const [pending, setPending] = useState(null);
  const [directory, setDirectory] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const job = status.job;
  const recovery = status.storage_recovery?.required ? status.storage_recovery : location?.recovery;
  // Neither missing nor recoverable: only a rebuild leaves it (#846).
  const unavailable = !recovery?.required ? location?.unavailable : null;
  const storageJob = job?.kind === "storage" || job?.kind === "storage-recovery";
  const busy = submitting || Boolean(pending) || Boolean(job?.running);
  useEffect(() => {
    let live = true;
    if (pending && job?.id === pending.previousJob) return;
    if (storageJob && job.running) return;
    api("/environment/storage").then((next) => {
      if (!live) return;
      setLocation(next);
      if (pending) {
        setPending(null);
        if (job?.id === pending.id && job.error) setError(job.error);
        else if (job?.id === pending.id || next.directory && next.directory === pending.path) { setDirectory(""); setError(""); }
        else setError("操作已结束，请核对当前环境目录后重试。");
      }
    }).catch((e) => {
      if (live) { setError(e.message); setPending(null); }
    });
    return () => { live = false; };
  }, [pending, job?.id, job?.running, storageJob, status.storage_recovery?.required]);

  const choose = async () => {
    setError("");
    try {
      const selected = await native("chooseFolder", { purpose: "environmentStorage" });
      if (selected) setDirectory(selected);
    } catch { setError("未能选择文件夹，请直接填写绝对路径。"); }
  };
  const apply = async () => {
    if (!location?.directory || busy || recovery?.required) return;
    const path = directory.trim();
    if (location.has_environment && !(await onConfirm({
      title: "迁移本机环境？",
      body: translate("请先保存文件。Cage lite 会停止环境和终端，将虚拟磁盘复制到 {0}，校验后再切换位置。原数据保留为备份，迁移完成后可手动启动环境。", path),
      confirm: "迁移环境",
    }))) return;
    setSubmitting(true);
    setError("");
    try {
      const accepted = await api("/environment/storage", {
        directory: path, previous: location.directory, migrate: location.has_environment, missing: Boolean(location.missing),
      });
      setPending({ id: accepted.id, previousJob: job?.id, path });
    } catch (e) { setError(e.message); }
    finally { setSubmitting(false); }
  };
  const reset = async () => {
    if (busy || !(await onConfirm({
      title: "重建环境？",
      body: "原存储移到归档文件夹，不会删除。",
      confirm: "重建环境",
      danger: true,
    }))) return;
    setSubmitting(true);
    setError("");
    try {
      const accepted = await api("/environment/storage/reset", {});
      setPending({ id: accepted.id, previousJob: job?.id });
    } catch (e) { setError(e.message); }
    finally { setSubmitting(false); }
  };
  const recover = async (repair) => {
    if (busy) return;
    setSubmitting(true);
    setError("");
    try {
      const accepted = await api("/environment/storage/recover", { repair });
      setPending({ id: accepted.id, previousJob: job?.id });
    } catch (e) { setError(e.message); }
    finally { setSubmitting(false); }
  };
  return <section className="environment-versions environment-storage" aria-label={tr("本机环境存储")}>
    <h4>{tr("本机环境存储")}</h4>
    {recovery?.required ? <>
      <p role="alert" className="error-message"><SafetyMessage message={recovery.error} /></p>
      <div className="storage-actions">
        <button type="button" disabled={busy} onClick={() => recover(false)}>{tr("重试恢复")}</button>
        {recovery.repair_allowed && <button type="button" disabled={busy} onClick={() => recover(true)}>{tr("修复系统助手并恢复")}</button>}
        <button type="button" disabled={busy} onClick={reset}>{tr("重建环境")}</button>
      </div>
    </> : unavailable ? <>
      <p role="alert" className="error-message"><SafetyMessage message={unavailable.error} /></p>
      <div className="storage-actions">
        <button type="button" disabled={busy} onClick={reset}>{tr("重建环境")}</button>
      </div>
    </> : <>
    <div className="setting-summary-row"><span>{tr("当前环境目录")}</span><strong>{location?.directory || (error ? tr("无法读取环境目录") : tr("正在读取…"))}</strong></div>
    {status.environment_identity_directory && status.environment_identity_directory !== status.environment_directory &&
      <p className="field-help">{tr("原工作目录中的文件保留在") + " "}<code>{status.environment_identity_directory}</code></p>}
    {location?.missing && location.error && <p role="alert" className="error-message"><SafetyMessage message={location.error} /></p>}
    {location?.backup && <p className="field-help">{tr("迁移前的备份保存在") + " "}<code>{location.backup}</code></p>}
    {location?.reset && <p className="field-help">{tr("重建前的存储保存在") + " "}<code>{location.reset}</code></p>}
    <label>{tr("新存储位置")}
      <input value={directory} onChange={(e) => { setDirectory(e.target.value); setError(""); }} spellCheck="false"
        disabled={busy || !location?.directory} placeholder={tr("本地磁盘上专用空文件夹的完整路径")} />
    </label>
    <div className="storage-actions">
      {nativeAvailable() && <button type="button" disabled={busy || !location?.directory} onClick={choose}><FolderOpen size={15} />{tr("选择文件夹")}</button>}
      <button type="button" disabled={busy || !location?.directory || !directory.trim() || directory.trim() === location.directory} onClick={apply}>
        {location?.has_environment ? tr("迁移到此位置") : tr("使用此位置")}
      </button>
    </div>
    </>}
    {storageJob && job.running && <p role="status" className="field-help">{localizeBackendMessage(job.message, tr)}</p>}
    {error && error !== recovery?.error && error !== unavailable?.error && <p role="alert" className="error-message"><SafetyMessage message={error} /></p>}
  </section>;
}
