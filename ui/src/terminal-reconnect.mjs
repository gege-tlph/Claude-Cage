// A terminal stream can lose its WebSocket: a tunnel drops it after a period
// of silence, or the host slept and the connection died without a close. The
// server sends a heartbeat every 20 seconds and answers pings. The view
// notices a stream that went silent, reconnects on its own, and resumes after
// the last byte it received instead of replaying the whole history.

export const terminalPingMessage = JSON.stringify({ type: "ping" });
// How often the view checks the stream.
export const terminalWatchdogMs = 5_000;
// No frame of any kind for this long: ask the server whether the path works.
export const terminalSilenceMs = 45_000;
// A ping sent to confirm a silent stream must be answered within this time.
export const terminalProbeMs = 5_000;
// A reconnect that completes within this time is not shown at all.
export const terminalNoticeDelayMs = 1_500;
// A connection attempt that has not opened by then is abandoned and retried.
export const terminalConnectTimeoutMs = 20_000;
// After this many quick failures in a row the view stops and shows the button.
export const terminalReconnectLimit = 6;
// A stream that stayed open this long was dropped by the network, not refused.
const stableMs = 10_000;

// failures counts consecutive attempts that closed soon after connecting (or
// never connected). The returned delay is 0 when the view should stop.
export function terminalReconnectPlan(failures, openedFor) {
  const next = openedFor >= stableMs ? 1 : failures + 1;
  if (next > terminalReconnectLimit) return { failures: next, delay: 0 };
  return { failures: next, delay: Math.min(500 * 2 ** (next - 1), 15_000) };
}

// terminalStreamPath resumes after the last byte the view received, or from
// the start when it has received nothing yet.
export function terminalStreamPath(id, offset) {
  const after = Number.isSafeInteger(offset) && offset >= 0 ? offset : 0;
  return `/terminals/${encodeURIComponent(id)}/stream?after=${after}`;
}

// terminalHistoryPlan reads a history marker. The bytes that follow it are
// replayed output; reset means the view must clear its screen first because
// they do not continue what it shows. A marker without an offset is the
// original framing: the next binary frame alone is replayed (offset null).
export function terminalHistoryPlan(offset, marker) {
  if (!Number.isSafeInteger(marker?.offset) || marker.offset < 0) return { reset: false, offset: null, size: null };
  const size = Number.isSafeInteger(marker.size) && marker.size > 0 ? marker.size : 0;
  return { reset: marker.reset === true || offset !== marker.offset, offset: marker.offset, size };
}

// terminalLiveness tells the watchdog what to do with an open stream: "probe"
// sends a ping that must be answered, "dead" drops the stream and "idle"
// waits. A silent stream is probed first, because a page that was frozen can
// run its timers before it delivers the messages that arrived meanwhile.
export function terminalLiveness({ now, lastReceived, probe = 0 }) {
  if (probe && lastReceived < probe) return now - probe >= terminalProbeMs ? "dead" : "idle";
  if (now - lastReceived >= terminalSilenceMs) return "probe";
  return "idle";
}
