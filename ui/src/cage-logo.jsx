import React from "react";
import "./cage-logo.css";

// The Cage mark drawn on the pixel grid of the app icon. The tile takes the
// theme's paper colour and the cage its strongest ink, so the mark follows
// light and dark themes; the crab keeps its own colours. The startup splash
// animates the same parts (see startup-splash.css).
export function CageLogo({ className = "", label }) {
  const a11y = label ? { role: "img", "aria-label": label } : { "aria-hidden": "true" };
  return (
    <svg className={`cage-mark ${className}`} viewBox="0 0 100 100" {...a11y}>
      <g className="cage-mark-crab">
        <rect x="20.5" y="29.5" width="59.2" height="37" />
        <rect x="12.5" y="47.5" width="8.5" height="10" />
        <rect x="79.2" y="47.5" width="8.3" height="10" />
        <g className="cage-mark-legs-a">
          <rect x="24.8" y="66" width="4.6" height="9" />
          <rect x="57.3" y="66" width="5.9" height="9" />
        </g>
        <g className="cage-mark-legs-b">
          <rect x="36.5" y="66" width="5.8" height="9" />
          <rect x="70.6" y="66" width="4.6" height="9" />
        </g>
        <g className="cage-mark-eyes">
          <rect x="36.5" y="37.2" width="5.6" height="10.3" />
          <rect x="58" y="37.2" width="5.6" height="10.3" />
        </g>
      </g>
      <rect className="cage-mark-frame" x="7.3" y="12.3" width="85.4" height="73.6" pathLength="100" />
      <rect className="cage-mark-bar cage-mark-bar-1" x="29" y="12" width="5.8" height="74" />
      <rect className="cage-mark-bar cage-mark-bar-2" x="65.2" y="12" width="5.8" height="74" />
    </svg>
  );
}
