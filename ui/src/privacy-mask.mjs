import { useSyncExternalStore } from "react";
import { clientStorageKey } from "./client-context.mjs";

// Privacy mask (#804, #836): one switch hides what a screenshot or a shared
// screen should not carry — the exit address and place, and the email and
// organization of Claude accounts. Masked values never reach the page, so
// screen readers and copied text miss them too. Saved per client.
const KEY = "cage-privacy-mask";
const listeners = new Set();
let fallback = false;

function storage() {
  try {
    return globalThis.localStorage;
  } catch {
    return undefined;
  }
}

export function privacyMasked() {
  try {
    const saved = storage()?.getItem(clientStorageKey(KEY));
    return saved === null || saved === undefined ? fallback : JSON.parse(saved) === true;
  } catch {
    return fallback;
  }
}

export function setPrivacyMasked(value) {
  fallback = value === true;
  try {
    storage()?.setItem(clientStorageKey(KEY), JSON.stringify(fallback));
  } catch {
    // Without storage the mask still applies until the page reloads.
  }
  for (const listener of listeners) listener();
}

function subscribe(listener) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function usePrivacyMask() {
  return useSyncExternalStore(subscribe, privacyMasked, () => false);
}

// Shown in place of a hidden account name, in every language.
export const MASKED_NAME = "••••••";

// The name shown for a Claude account. While masked, an email, given as the
// label or the address, never reaches the page; a name the user gave the
// account stays. Lists keep their #NN numbers, so accounts stay apart.
export function accountName(account, masked) {
  const label = String(account?.label || "").trim();
  const email = String(account?.email || "").trim();
  if (!masked) return label || email;
  if (label && label !== email && !label.includes("@")) return label;
  return MASKED_NAME;
}
