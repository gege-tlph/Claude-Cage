import { useLayoutEffect } from "react";

// useSidebarEdge keeps --settings-sidebar-edge on the root at the right edge
// of the settings sidebar (ref). While settings are open the status bar sits
// at the window's bottom (StatusBarHost); it starts at this edge, under the
// submit bar, and the sidebar runs down to the bottom (user, 2026-11-28).
// offsetWidth, not the box on screen: the page's entry animation moves it.
export function useSidebarEdge(ref) {
  useLayoutEffect(() => {
    const element = ref.current, root = document.documentElement;
    if (!element) return undefined;
    const set = () => root.style.setProperty("--settings-sidebar-edge", `${element.offsetLeft + element.offsetWidth}px`);
    set();
    const observer = new ResizeObserver(set);
    observer.observe(element);
    return () => {
      observer.disconnect();
      root.style.removeProperty("--settings-sidebar-edge");
    };
  }, [ref]);
}
