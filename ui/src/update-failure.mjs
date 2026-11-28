import { failureFromText } from "./failure-text.mjs";

// An update failure the desktop app hands over as joined text (AppUpdater,
// UpdateInstaller; notes/failure-text-audit-2026-11-28.md U1–U5), as the
// failure card draws it. The level follows how far the update got: a
// download failure or a restored version is a warning, a manual install a
// notice; text that does not split (an incomplete recovery) stays as text.
export function updateFailure(message, phase) {
  const failure = failureFromText(message);
  if (!failure) return null;
  if (failure.level === "error") failure.level = "warn";
  failure.tools = phase === "manual" ? ["releases"] : ["retry-update", "diagnostics"];
  return failure;
}

// The failure's first line, for a tooltip or a short notice.
export function updateFailureLine(message) {
  const failure = failureFromText(message);
  return failure ? `${failure.lead}，${failure.reason}` : message;
}
