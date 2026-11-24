import { accountName } from "./privacy-mask.mjs";
import { translate } from "./message-catalog.mjs";

// The controller names instances nobody named in Chinese; they are shown in
// the interface language. Names the user gave stay as they are.
const generatedLabels = new Set(["默认 Desktop", "待登录 Desktop"]);

// masked: the privacy mask (#836) hides a label that is an email.
export function desktopLabel(a, masked = false) {
  const label = accountName(a, masked);
  return generatedLabels.has(label) ? translate(label) : label;
}

export const desktopTitle = (a, masked = false) => {
  const label = desktopLabel(a, masked);
  return a.number ? `#${String(a.number).padStart(2, "0")} ${label}` : label;
};

export function addDesktopBeside(panes, account, defaultEntry) {
  const current = panes.length ? panes : defaultEntry ? [defaultEntry] : [];
  if (current.some((pane) => pane.id === account.id)) return current;
  if (account.id === "default") return [account, ...current.slice(0, 1)];
  return current.length ? [current[0], account] : [account];
}

export function hasDefaultDesktop(catalog) {
  if (!catalog) return true;
  return !catalog.default_removed && !catalog.redirects?.default;
}

export function visibleDesktopCount(catalog) {
  const instances = catalog?.instances ?? catalog?.accounts ?? [];
  const others = instances.filter((item) => item.id !== "default");
  return (hasDefaultDesktop(catalog) ? 1 : 0) + others.length;
}

export function canDeleteDesktop(catalog) {
  return Boolean(catalog?.supported) && visibleDesktopCount(catalog) > 1;
}

export function mergedDefaultInstance(catalog) {
  const target = catalog?.redirects?.default;
  if (!target || !Array.isArray(catalog?.instances)) return null;
  return catalog.instances.find((item) => item.id === target) || null;
}

export function fallbackDesktopInstance(catalog) {
  const merged = mergedDefaultInstance(catalog);
  if (merged) return merged;
  if (!catalog?.default_removed || !Array.isArray(catalog?.instances)) return null;
  const others = catalog.instances.filter((item) => item.id !== "default");
  return others.find((item) => item.running) || others[0] || null;
}

export function reconcileAccountDesktopPanes(oldPanes, catalog, viewingDefault = false) {
  if (!catalog?.instances) return oldPanes;
  if (!oldPanes.length) {
    if (!viewingDefault) return oldPanes;
    const merged = mergedDefaultInstance(catalog);
    return merged && merged.id !== "default" ? [merged] : oldPanes;
  }
  if (oldPanes.length === 1 && catalog.redirects?.[oldPanes[0].id] === "default") return [];
  const seen = new Set();
  const updated = [];
  for (const pane of oldPanes) {
    const id = catalog.redirects?.[pane.id] || pane.id;
    const item = catalog.instances.find((a) => a.id === id) || (pane.id === id && id === "default" ? pane : null);
    if (!item || seen.has(id)) continue;
    seen.add(id);
    updated.push(item);
  }
  return updated;
}

export function desktopClipboardState({ ready, focused, visible, windowVisible, native }) {
  const connected = Boolean(ready && focused && native);
  return {
    connected,
    pollingVisible: Boolean(connected && visible && windowVisible),
  };
}

export function desktopMergeNoticeTarget(previousRedirect, catalog) {
  const nextRedirect = catalog?.redirects?.default || "";
  if (!nextRedirect || nextRedirect === previousRedirect) return null;
  return mergedDefaultInstance(catalog);
}
