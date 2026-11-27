import React, { useEffect, useRef, useState } from "react";
import { CLOSE_MS, reducedMotion } from "./status-phase.mjs";
import "./status-motion.css";

// StatusIcon is the one circle every state uses: idle (empty ring), selected
// (blue dot, 待应用), busy (spinning arc), done (green ring and check), fail
// (red ring and !). From busy, done and fail first close the arc while it
// spins, then draw the mark with a slight pop.
export function StatusIcon({ state = "idle", size = 16, className = "", label }) {
  const finished = state === "done" || state === "fail";
  const [closing, setClosing] = useState(false);
  const [closed, setClosed] = useState(finished);
  const [pop, setPop] = useState(false);
  const previous = useRef(state);
  useEffect(() => {
    const was = previous.current;
    previous.current = state;
    if (was === state) return;
    setPop(false);
    if (!finished) {
      setClosing(false);
      setClosed(false);
      return;
    }
    const fromBusy = was === "busy" && !reducedMotion();
    setClosing(fromBusy);
    setClosed(false);
    let frame = 0;
    const timer = setTimeout(() => {
      setClosing(false);
      setClosed(true);
      frame = requestAnimationFrame(() => setPop(true));
    }, fromBusy ? CLOSE_MS : 0);
    return () => {
      clearTimeout(timer);
      cancelAnimationFrame(frame);
    };
  }, [state, finished]);
  const classes = ["status-icon", className, closing && "is-closing", closed && "is-closed", pop && "is-pop"].filter(Boolean).join(" ");
  return (
    <span
      className={classes}
      data-state={state}
      style={{ width: size, height: size }}
      {...(label ? { role: "img", "aria-label": label } : { "aria-hidden": "true" })}
    >
      <svg viewBox="0 0 20 20" focusable="false">
        <g className="status-icon-spin"><circle className="status-icon-ring" cx="10" cy="10" r="8.25" /></g>
        <circle className="status-icon-dot" cx="10" cy="10" r="8" />
        <path className="status-icon-mark status-icon-check" pathLength="1" d="M6.1 10.3 8.8 12.9 14 7.4" />
        <path className="status-icon-mark status-icon-bang" pathLength="1" d="M10 5.6v5.1" />
        <circle className="status-icon-bang-dot" cx="10" cy="14.2" r="1.15" />
      </svg>
    </span>
  );
}
