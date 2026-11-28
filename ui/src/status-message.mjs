// What the status bar says, and the operation log (操作日志) in text form.
// Pure so it can be tested without React; status-activity.jsx and
// operation-log.jsx use it.

// statusMessage picks the status bar's message: a notification first, then
// the running job's short stage, then a failed job's short result, then the
// resting text. tone is "busy" (the light sweep), "fail" (red), "warn"
// (orange, a failure whose level is 警告) or "". A failure's sentence goes to
// the log, not here. raw is the backend text the
// message stands for, for the safety checklist link.
export function statusMessage({ toast = "", toastTone = "", job, running = false, ready = false, remote = false } = {}) {
  if (toast) return { text: toast, tone: toastTone === "error" ? "fail" : "", raw: toast };
  if (job?.running) {
    const text = job.stage || job.message || "";
    return { text, tone: text ? "busy" : "", raw: "" };
  }
  if (job?.error && !job.canceled) {
    const level = job.failure?.level;
    return { text: job.stage || "失败", tone: level === "warn" ? "warn" : level === "info" ? "" : "fail", raw: job.error };
  }
  if (running) return { text: ready ? (remote ? "文件保存在云端" : "") : "环境运行中 · 浏览器待恢复", tone: "", raw: "" };
  return { text: "", tone: "", raw: "" };
}

// unreadFailure is the failed job the log icon marks with a red dot until
// the log has been opened after it ended.
export function unreadFailure(job, seen) {
  return Boolean(job?.id && !job.running && job.error && !job.canceled && job.failure?.level !== "info" && job.id !== seen);
}

// announcement is what a screen reader hears when the job changes: only the
// start, the end and a failure, never each stage. previous is null on the
// first look, when nothing is announced.
export function announcement(previous, job) {
  if (!previous || !job?.id) return "";
  const same = previous.id === job.id;
  if (job.running) return same && previous.running ? "" : job.stage || job.message || "";
  if (same && !previous.running) return "";
  if (job.canceled) return "已取消";
  return job.error ? job.stage || "失败" : "完成";
}

const pad = (value, size = 2) => String(value).padStart(size, "0");

// The log's clock: 24-hour local time with seconds, and milliseconds for
// the advanced view.
export function logTime(value, millis = false) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const time = `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
  return millis ? `${time}.${pad(date.getMilliseconds(), 3)}` : time;
}

// The advanced view's levels, lowest first; it shows events at the chosen
// level or above.
export const LOG_LEVELS = ["debug", "info", "warn", "error"];
export const normalLogLevel = (level) => (LOG_LEVELS.includes(level) ? level : "info");

// eventLine is one event of the advanced view as text: time, level, source,
// message.
export function eventLine(event) {
  return `${logTime(event.time, true)} ${String(event.level || "").toUpperCase().padEnd(5)} ${event.source || "-"} ${event.message || ""}`;
}

export function eventLog(events = []) {
  return (events || []).map(eventLine).join("\n");
}

// logGroups groups the simple view's entries by job, in order. A job's first
// entry is its name; the rest are its progress messages and its result.
export function logGroups(entries = []) {
  const groups = [];
  for (const entry of entries || []) {
    const last = groups.at(-1);
    if (last && last.job === entry.job) last.entries.push(entry);
    else groups.push({ job: entry.job, kind: entry.kind, entries: [entry] });
  }
  return groups;
}

// simpleLog is the simple view as one text, for copying: a timed line per
// entry, a blank line between jobs. localize translates each message.
export function simpleLog(entries = [], localize = (message) => message) {
  return logGroups(entries)
    .map((group) => group.entries.map((entry) => `${logTime(entry.time)} ${localize(entry.message)}`).join("\n"))
    .join("\n\n");
}

// rawLog is the advanced view as one text: each job's raw output under a
// line with its start time and name. names maps a job id to its name.
export function rawLog(outputs = [], names = {}) {
  return (outputs || [])
    .filter((output) => output.log)
    .map((output) => `── ${logTime(output.started)} ${names[output.job] || output.kind} ──\n${output.log.replace(/\s+$/, "")}`)
    .join("\n\n");
}

// jobNames maps each job id to its name: the message of its first entry.
export function jobNames(entries = []) {
  const names = {};
  for (const entry of entries || []) if (!(entry.job in names)) names[entry.job] = entry.message;
  return names;
}
