import assert from "node:assert/strict";
import test from "node:test";
import { announcement, eventLog, jobNames, logGroups, logTime, normalLogLevel, rawLog, simpleLog, statusMessage, unreadFailure } from "./status-message.mjs";

test("the status bar shows a notification, then the stage, then a failure, then the resting text", () => {
  const job = { id: "a", running: true, stage: "启动浏览器", message: "正在启动浏览器、编辑器与共享文件…" };
  assert.deepEqual(statusMessage({ toast: "项目文件已刷新", job }), { text: "项目文件已刷新", tone: "", raw: "项目文件已刷新" });
  assert.equal(statusMessage({ toast: "保存失败", toastTone: "error", job }).tone, "fail");
  assert.deepEqual(statusMessage({ job }), { text: "启动浏览器", tone: "busy", raw: "" });
  // A backend without stages still shows its message.
  assert.equal(statusMessage({ job: { running: true, message: "正在准备…" } }).text, "正在准备…");
  assert.deepEqual(statusMessage({ job: { id: "a", error: "x", stage: "启动环境失败" } }), { text: "启动环境失败", tone: "fail", raw: "x" });
  assert.equal(statusMessage({ job: { id: "a", error: "x" } }).text, "失败");
  // The level of a failure picks the color: 警告 orange, 提示 plain.
  assert.equal(statusMessage({ job: { id: "a", error: "x", stage: "更新订阅失败", failure: { level: "warn" } } }).tone, "warn");
  assert.equal(statusMessage({ job: { id: "a", error: "x", failure: { level: "info" } } }).tone, "");
  // A canceled job is not a failure.
  assert.equal(statusMessage({ job: { id: "a", error: "x", canceled: true, stage: "已取消" }, running: true, ready: true }).text, "");
  assert.equal(statusMessage({ job: { id: "a" }, running: true, ready: false }).text, "环境运行中 · 浏览器待恢复");
  assert.equal(statusMessage({ job: { id: "a" }, running: true, ready: true, remote: true }).text, "文件保存在云端");
  assert.equal(statusMessage({ job: { id: "a" }, running: false }).text, "");
});

test("the red dot marks a failed job until the log is opened after it", () => {
  const failed = { id: "a", error: "x" };
  assert.equal(unreadFailure(failed, ""), true);
  assert.equal(unreadFailure(failed, "a"), false);
  assert.equal(unreadFailure({ ...failed, running: true }, ""), false);
  assert.equal(unreadFailure({ ...failed, canceled: true }, ""), false);
  assert.equal(unreadFailure({ id: "a" }, ""), false);
  assert.equal(unreadFailure({ ...failed, failure: { level: "warn" } }, ""), true);
  assert.equal(unreadFailure({ ...failed, failure: { level: "info" } }, ""), false);
});

test("a screen reader hears the start, the end and a failure only", () => {
  const start = { id: "a", running: true, stage: "启动环境" };
  assert.equal(announcement(null, start), "");
  assert.equal(announcement({}, start), "启动环境");
  assert.equal(announcement(start, { ...start, stage: "启动浏览器" }), "");
  assert.equal(announcement(start, { id: "a" }), "完成");
  assert.equal(announcement(start, { id: "a", error: "x", stage: "启动环境失败" }), "启动环境失败");
  assert.equal(announcement(start, { id: "a", canceled: true, error: "x" }), "已取消");
  assert.equal(announcement({ id: "a" }, { id: "a" }), "");
  // A job that started and ended between two looks.
  assert.equal(announcement({ id: "a" }, { id: "b" }), "完成");
});

test("the log groups entries by job and prints the raw output under each job", () => {
  const at = (s) => new Date(2026, 10, 28, 9, 5, s).toISOString();
  const entries = [
    { time: at(1), job: "a", kind: "start", message: "启动环境" },
    { time: at(2), job: "a", kind: "start", message: "正在启动浏览器、编辑器与共享文件…" },
    { time: at(3), job: "a", kind: "start", message: "已完成", state: "done" },
    { time: at(4), job: "b", kind: "proxy-route", message: "切换节点" },
  ];
  assert.equal(logTime(at(7)), "09:05:07");
  assert.equal(logTime("bad"), "");
  assert.equal(logTime(new Date(2026, 10, 28, 9, 5, 7, 42).toISOString(), true), "09:05:07.042");
  assert.equal(normalLogLevel("warn"), "warn");
  assert.equal(normalLogLevel(null), "info");
  assert.equal(
    eventLog([
      { time: new Date(2026, 10, 28, 9, 5, 7, 5).toISOString(), level: "warn", source: "proxy", message: "connection refused" },
      { time: new Date(2026, 10, 28, 9, 5, 8, 0).toISOString(), level: "info", source: "task/start", message: "ready" },
    ]),
    "09:05:07.005 WARN  proxy connection refused\n09:05:08.000 INFO  task/start ready",
  );
  assert.deepEqual(logGroups(entries).map((group) => [group.job, group.entries.length]), [["a", 3], ["b", 1]]);
  assert.deepEqual(logGroups(undefined), []);
  const names = jobNames(entries);
  assert.deepEqual(names, { a: "启动环境", b: "切换节点" });
  assert.equal(simpleLog(entries.slice(2), (text) => `<${text}>`), "09:05:03 <已完成>\n\n09:05:04 <切换节点>");
  assert.equal(
    rawLog([{ job: "a", kind: "start", started: at(1), log: "one\ntwo\n" }, { job: "b", kind: "proxy-route", started: at(4), log: "" }, { job: "c", kind: "safety", started: at(9), log: "x" }], names),
    "── 09:05:01 启动环境 ──\none\ntwo\n\n── 09:05:09 safety ──\nx",
  );
});
