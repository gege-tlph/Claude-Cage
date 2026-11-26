import React, { createContext, useContext, useLayoutEffect, useRef, useState } from "react";
import { isSafetyCheckMessage } from "./safety-state.mjs";
import { useTranslation } from "./interface-language.mjs";
import { localizeBackendMessage } from "./backend-message.mjs";

export const SafetyChecklistContext = createContext(null);

// The detail of a failure under a heading that already names it: drop the
// lead the heading repeats ("联网检查未通过：…" under "环境已启动，联网检查未通过").
export function withoutRepeatedLead(text, heading) {
  const lead = /^([^：:\n]{2,40})[：:]\s*/.exec(text || "");
  return lead && heading && heading.toLowerCase().includes(lead[1].trim().toLowerCase()) ? text.slice(lead[0].length) : text;
}

// A long detail shows its first lines; the rest opens on request.
export function FoldedText({ text, className, lines = 3 }) {
  const tr = useTranslation();
  const body = useRef(null);
  const [open, setOpen] = useState(false);
  const [long, setLong] = useState(false);
  useLayoutEffect(() => {
    const element = body.current;
    if (element && !open) setLong(element.scrollHeight > element.clientHeight + 1);
  }, [text, open]);
  return <>
    <p ref={body} className={className} style={open ? undefined : { display: "-webkit-box", WebkitBoxOrient: "vertical", WebkitLineClamp: lines, overflow: "hidden" }}>{text}</p>
    {(long || open) && <button type="button" className="text-button folded-text-toggle" aria-expanded={open} onClick={() => setOpen(!open)}>{open ? tr("收起") : tr("展开")}</button>}
  </>;
}

// The checklist as one action among others, for a card with an action row.
export function SafetyChecklistButton({ message }) {
  const tr = useTranslation();
  const openChecklist = useContext(SafetyChecklistContext);
  if (!openChecklist || !isSafetyCheckMessage(message)) return null;
  return <button type="button" className="text-button" onClick={openChecklist}>{tr("查看检查清单")}</button>;
}

export function SafetyMessage({ message }) {
  const tr = useTranslation();
  const openChecklist = useContext(SafetyChecklistContext);
  const text = localizeBackendMessage(message, tr);
  if (!openChecklist || !isSafetyCheckMessage(message)) return text;
  return <>
    <span className="safety-message-text">{text}</span>
    <button type="button" className="text-button safety-checklist-link" onClick={openChecklist}>
      {tr("查看检查清单")}
    </button>
  </>;
}
