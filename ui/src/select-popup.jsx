import { useLayoutEffect, useState } from "react";

// Both desktop and mobile mount the list outside scroll/overflow containers.
export function useSelectPopup(visible, control, list, count) {
  const [position, setPosition] = useState(null);
  useLayoutEffect(() => {
    if (!visible) return;
    const place = () => {
      const rect = control.current.getBoundingClientRect();
      const style = getComputedStyle(control.current);
      const gap = 4, margin = 8;
      const below = window.innerHeight - rect.bottom - gap - margin;
      const above = rect.top - gap - margin;
      const upward = below < Math.min(list.current.scrollHeight + 8, 224) && above > below;
      const maxHeight = Math.max(40, Math.min(280, upward ? above : below));
      // As wide as the control, or as its longest option when that is wider
      // (up to 320px), so a short control's options do not wrap. offsetWidth:
      // the opening animation scales the list on screen. +1 for rounding.
      const previous = list.current.style.width;
      list.current.style.width = "max-content";
      const natural = list.current.offsetWidth + 1;
      list.current.style.width = previous;
      const width = Math.min(Math.max(rect.width, Math.min(natural, 320)), window.innerWidth - margin * 2);
      const height = Math.min(list.current.scrollHeight + 8, maxHeight);
      const next = { position: "fixed", zIndex: 10000, boxSizing: "border-box", margin: 0, left: Math.max(margin, Math.min(rect.left, window.innerWidth - width - margin)), top: upward ? Math.max(margin, rect.top - gap - height) : rect.bottom + gap, width, maxHeight, fontFamily: style.fontFamily, fontSize: style.fontSize, lineHeight: style.lineHeight };
      for (const key of ["--ink", "--paper", "--line", "--selected", "--focus", "--muted", "--control-cursor"]) next[key] = style.getPropertyValue(key);
      setPosition(next);
    };
    const scroll = event => { if (!list.current?.contains(event.target)) place(); };
    place();
    const observer = new ResizeObserver(place);
    observer.observe(control.current);
    window.addEventListener("resize", place);
    document.addEventListener("scroll", scroll, true);
    return () => { observer.disconnect(); window.removeEventListener("resize", place); document.removeEventListener("scroll", scroll, true); };
  }, [visible, control, list, count]);
  return position || { visibility: "hidden" };
}
