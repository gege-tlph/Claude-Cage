import React from "react";
import { useTranslation } from "./interface-language.mjs";
import { localizeBackendMessage } from "./backend-message.mjs";

// The last network check, item by item: first run has no safety panel.
export function InitializationDetails({ status }) {
  const tr = useTranslation();
  const job = status?.job || {};
  const reasons = [...new Set([job.error, status?.safety?.error, status?.network_error].filter(Boolean))];
  const checks = status?.safety?.report?.checks || [];
  const labels = { passed: "已通过", failed: "未通过", unknown: "尚未完成" };
  return <div className="modal-body">
    {reasons.map(reason => <p key={reason} role="alert">{localizeBackendMessage(reason, tr)}</p>)}
    {checks.length > 0 && <><ol className="initialization-checks">{checks.map(check => <li key={check.id}>
      <strong>{localizeBackendMessage(check.title, tr)} · {labels[check.state] ? tr(labels[check.state]) : tr("尚未完成")}</strong>
      <p>{localizeBackendMessage(check.evidence, tr)}</p>{check.state !== "passed" && check.remedy && <p>{localizeBackendMessage(check.remedy, tr)}</p>}
    </li>)}</ol></>}
  </div>;
}
