import React, { useEffect, useState } from "react";
import { LoaderCircle } from "lucide-react";
import { api } from "./api";
import { SafetyMessage } from "./safety-message";
import { useTranslation } from "./interface-language.mjs";

const endpointHost = (endpoint) => {
  try {
    return new URL(endpoint).host;
  } catch {
    return endpoint;
  }
};

const sameList = (a = [], b = []) => JSON.stringify(a.filter(Boolean)) === JSON.stringify(b.filter(Boolean));

// Encrypted DNS on the first-run proxy step. APT in the first installation
// resolves through these endpoints, so when it cannot, they can be tested
// through the applied node and changed here before continuing (#815).
export function InitializationDNS({ status, disabled, open }) {
  const tr = useTranslation();
  const [saved, setSaved] = useState(null);
  const [draft, setDraft] = useState(null);
  const [result, setResult] = useState(null);
  const [checking, setChecking] = useState(false);
  const [jobId, setJobId] = useState("");
  const [error, setError] = useState("");
  const load = () => api("/settings").then((value) => {
    setSaved(value);
    setDraft(value);
  }).catch((e) => setError(e.message));
  useEffect(() => { load(); }, []);
  useEffect(() => {
    if (!jobId || status.job?.id !== jobId || status.job.running) return;
    setJobId("");
    if (status.job.error) setError(status.job.error);
    else load();
  }, [jobId, status.job]);

  if (!draft) return error ? <p role="alert" className="error-message"><SafetyMessage message={error} /></p> : null;
  const busy = disabled || checking || Boolean(jobId) || Boolean(status.job?.running);
  const dirty = draft.dns_url !== saved.dns_url || !sameList(draft.dns_fallback_urls, saved.dns_fallback_urls);
  const update = (key, value) => {
    setDraft((previous) => ({ ...previous, [key]: value }));
    setResult(null);
    setError("");
  };
  const check = async () => {
    setChecking(true);
    setError("");
    try {
      setResult(await api("/settings/dns-check", draft));
    } catch (e) {
      setResult(null);
      setError(e.message);
    } finally {
      setChecking(false);
    }
  };
  const save = async () => {
    setError("");
    try {
      const accepted = await api("/settings", draft);
      setJobId(accepted.id);
    } catch (e) {
      setError(e.message);
    }
  };
  const outcome = (check) => check.result === "ok"
    ? tr("可用 · {0}", check.address)
    : check.result === "no_address"
      ? tr("没有地址")
      : check.result === "response"
        ? tr("返回错误 · {0}", check.detail)
        : tr("连接失败");

  return <details className="initialization-dns" open={open || undefined}>
    <summary>{tr("加密 DNS")}<small>{endpointHost(saved.dns_url)}</small></summary>
    <label>{tr("加密 DNS")}
      <input value={draft.dns_url} onChange={(e) => update("dns_url", e.target.value)} disabled={busy} spellCheck="false" />
    </label>
    <label>{tr("备用加密 DNS（每行一个，最多 4 个）")}
      <textarea rows={2} value={(draft.dns_fallback_urls ?? []).join("\n")} disabled={busy} spellCheck="false" placeholder="https://…/dns-query"
        onChange={(e) => update("dns_fallback_urls", e.target.value.split(/\r?\n/))} />
    </label>
    <div className="initialization-dns-actions">
      <button type="button" disabled={busy} onClick={check} title={tr("经所选节点解析 {0}", "deb.debian.org")}>
        {checking && <LoaderCircle size={15} className="spin" />}{tr("测试加密 DNS")}
      </button>
      <button type="button" className="primary" disabled={busy || !dirty} onClick={save}>
        {jobId && <LoaderCircle size={15} className="spin" />}{tr("保存")}
      </button>
    </div>
    {result && <ul className="dns-check-results" aria-label={tr("解析 {0}", result.host)}>
      {result.endpoints.map((check) => <li key={check.endpoint} data-result={check.result} title={check.detail || undefined}>
        <span>{endpointHost(check.endpoint)}</span>
        <strong>{outcome(check)}</strong>
      </li>)}
    </ul>}
    {error && <p role="alert" className="error-message"><SafetyMessage message={error} /></p>}
  </details>;
}
