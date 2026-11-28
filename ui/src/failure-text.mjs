// A failure the backend could only hand over as joined text — a subscription's
// saved error, an update message from the desktop app, an older backend —
// split back into the three lines of the failure card (usererror.Failure):
//
//   {lead}，{reason}（最后一条错误：{raw}）。解决建议：{next}。
//
// Level: 「下一步」 is a notice; a reason that says what is still in use
// (「仍在用…」「已恢复…」) is a warning; anything else is an error.
const JOINED = /^(.+?)，((?:原因|卡在)[^。]*?)(?:（最后一条错误：(.*)）)?。(?:(解决建议|下一步)：(.+?)。?)?$/s;

export function failureFromText(text, tools = []) {
  if (typeof text !== "string") return null;
  const m = JOINED.exec(text.trim());
  if (!m) return null;
  const [, lead, reason, raw, label, next] = m;
  const level = label === "下一步" ? "info" : /仍在用|已恢复/.test(reason) ? "warn" : "error";
  const failure = { level, lead, reason, tools };
  if (raw) failure.raw = raw;
  if (next) failure.next = next;
  return failure;
}

// failureOf is the failure an error carries (X-Cage-Failure, a job's
// failure), or the one its text reads as.
export function failureOf(error, tools) {
  if (!error) return null;
  if (error.failure) return error.failure;
  return failureFromText(typeof error === "string" ? error : error.message, tools);
}
