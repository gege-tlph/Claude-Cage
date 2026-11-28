import React from "react";
import { ArrowRight } from "lucide-react";
import "./route-text.css";

// RouteText draws 「A → B」 with arrow icons in place of the character
// (user, 2026-11-28: arrows are icons, never characters).
export function RouteText({ text }) {
  if (typeof text !== "string" || !text.includes(" → ")) return text || null;
  const parts = text.split(" → ");
  return parts.map((part, index) => <React.Fragment key={index}>
    {index > 0 && <><ArrowRight size={12} className="route-text-arrow" aria-hidden="true" /><span className="route-text-sr">→</span></>}
    {part}
  </React.Fragment>);
}
