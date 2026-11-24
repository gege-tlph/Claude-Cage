import { useTranslation } from "./interface-language.mjs";
import React, { useLayoutEffect, useRef, useState } from "react";
import { terminalGeometry } from "./terminal-splits.mjs";

function SplitDivider({ divider, onResize }) {
  const tr = useTranslation();
  const drag = useRef(null);
  const { axis, bounds, first, min, max, available, id } = divider;
  const across = axis === "x";
  const coordinate = event => across ? event.clientX : event.clientY;
  const resize = length => onResize(id, Math.max(min, Math.min(max, length)) / available);
  const percent = length => Math.round(100 * length / available);
  return <div className={`terminal-divider terminal-divider-${axis}`} style={bounds}
    role="separator" tabIndex={0} aria-orientation={across ? "vertical" : "horizontal"}
    aria-label={across ? tr("调整终端窗格宽度") : tr("调整终端窗格高度")}
    aria-valuenow={percent(first)} aria-valuemin={percent(min)} aria-valuemax={percent(max)}
    onPointerDown={event => {
      if (event.button !== 0 || !event.isPrimary) return;
      event.preventDefault();
      event.currentTarget.focus();
      event.currentTarget.setPointerCapture(event.pointerId);
      drag.current = { pointer: event.pointerId, origin: coordinate(event), first };
    }}
    onPointerMove={event => {
      const state = drag.current;
      if (state?.pointer === event.pointerId) resize(state.first + coordinate(event) - state.origin);
    }}
    onLostPointerCapture={() => { drag.current = null; }}
    onDoubleClick={() => resize(available / 2)}
    onKeyDown={event => {
      if (event.ctrlKey || event.metaKey || event.altKey) return;
      const negative = across ? "ArrowLeft" : "ArrowUp", positive = across ? "ArrowRight" : "ArrowDown";
      const step = event.shiftKey ? 48 : 16;
      const length = { [negative]: first - step, [positive]: first + step, Home: min, End: max }[event.key];
      if (typeof length !== "number") return;
      event.preventDefault();
      resize(length);
    }} />;
}

export function TerminalSplitView({ tree, active, visible, children, onResize }) {
  const viewport = useRef(null);
  const [size, setSize] = useState({ width: 0, height: 0 });
  useLayoutEffect(() => {
    if (!tree) return;
    const node = viewport.current;
    const measure = () => setSize(previous =>
      previous.width === node.clientWidth && previous.height === node.clientHeight ? previous
        : { width: node.clientWidth, height: node.clientHeight });
    const observer = new ResizeObserver(measure);
    observer.observe(node);
    measure();
    return () => observer.disconnect();
  }, [Boolean(tree)]);

  useLayoutEffect(() => {
    if (!tree || !visible) return;
    viewport.current.querySelector(`[data-terminal-pane="${CSS.escape(active)}"]`)
      ?.scrollIntoView({ block: "nearest", inline: "nearest" });
  }, [Boolean(tree), active, visible]);

  const geometry = tree ? terminalGeometry(tree, size.width, size.height) : null;
  const minimum = geometry?.minimum;
  return <div ref={viewport} className="terminal-split-viewport" data-split={Boolean(tree)} hidden={!visible}>
    <div className="terminal-split-canvas" style={minimum && { minWidth: minimum.width, minHeight: minimum.height }}>
      {children(geometry?.panes)}
      {geometry?.dividers.map(divider => <SplitDivider key={divider.id} divider={divider} onResize={onResize} />)}
    </div>
  </div>;
}
