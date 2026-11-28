import test from "node:test";
import assert from "node:assert/strict";
import { failureFromText, failureOf } from "./failure-text.mjs";

test("joined failure text splits back into three lines", () => {
  assert.deepEqual(failureFromText("启动环境失败，原因是虚拟机启动后，SSH 连接超时（10 分钟）。解决建议：重试启动。"), {
    level: "error", lead: "启动环境失败", reason: "原因是虚拟机启动后，SSH 连接超时（10 分钟）", next: "重试启动", tools: [],
  });
  assert.deepEqual(failureFromText("启动环境失败，卡在启动浏览器，原因未识别（最后一条错误：browser exited）。解决建议：重试启动。", ["start"]), {
    level: "error", lead: "启动环境失败", reason: "卡在启动浏览器，原因未识别", raw: "browser exited", next: "重试启动", tools: ["start"],
  });
});

test("the level follows the words", () => {
  assert.equal(failureFromText("更新订阅失败，原因是订阅服务器返回 HTTP 403，仍在用上次的 24 个节点。解决建议：到服务商处重新复制订阅地址。").level, "warn");
  assert.equal(failureFromText("更新失败，原因是 0.2.4 启动后 2 分钟内没有完成启动确认，已恢复 0.2.3。解决建议：重试更新。").level, "warn");
  assert.equal(failureFromText("启动环境前需要代理节点，原因是还没有选择节点。下一步：在网络与代理中导入订阅并选择节点。").level, "info");
});

test("other text is not a failure", () => {
  assert.equal(failureFromText("订阅下载失败，请检查下载路径和服务地址"), null);
  assert.equal(failureFromText(""), null);
  assert.equal(failureFromText(undefined), null);
});

test("an error's own failure wins over its text", () => {
  const failure = { level: "warn", lead: "导入订阅失败", reason: "原因是 2 个节点都没能导入" };
  assert.equal(failureOf(Object.assign(new Error("x"), { failure })), failure);
  assert.equal(failureOf(new Error("启动环境失败，原因是 KVM 不可用。解决建议：重试启动。")).reason, "原因是 KVM 不可用");
  assert.equal(failureOf(null), null);
});
