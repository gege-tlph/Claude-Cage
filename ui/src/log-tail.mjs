// The advanced view of 操作日志 can hold 3000 events. It draws the newest
// LOG_TAIL first and older ones LOG_TAIL_STEP at a time as the view nears its
// top (operation-log-tail.js).
export const LOG_TAIL = 200;
export const LOG_TAIL_STEP = 400;

// eventKey names one event across reads, which return new objects.
export function eventKey(event) {
  return event ? `${event.time}\u0000${event.source}\u0000${event.message}` : null;
}

// tailStart is the index of the first drawn event: the event named by first
// while it is still there, else the start of the newest LOG_TAIL.
export function tailStart(events = [], first = null) {
  const list = events || [];
  if (first !== null) {
    const index = list.findIndex((event) => eventKey(event) === first);
    if (index >= 0) return index;
  }
  return Math.max(0, list.length - LOG_TAIL);
}
