import test from "node:test";
import assert from "node:assert/strict";
import { updateFailure, updateFailureLine } from "./update-failure.mjs";

test("update failures read as warnings with a retry", () => {
  const restored = updateFailure("更新失败，原因是 0.2.4 启动后 2 分钟内没有完成启动确认，已恢复 0.2.3。解决建议：重试更新。", "failed");
  assert.equal(restored.level, "warn");
  assert.deepEqual(restored.tools, ["retry-update", "diagnostics"]);
  const download = updateFailure("下载更新失败，原因是更新源返回 HTTP 503。解决建议：稍后重试更新。", "failed");
  assert.equal(download.level, "warn");
  const manual = updateFailure("无法自动更新，原因是当前程序未签名或签名无法验证。下一步：从发布页下载安装包，退出 Cage lite 后安装到原位置（配置、项目和环境磁盘会保留）。", "manual");
  assert.equal(manual.level, "info");
  assert.deepEqual(manual.tools, ["releases"]);
  assert.equal(updateFailure("更新未完成：原应用无法恢复。程序备份保留在 D:/backup", "failed"), null);
});

test("the first line stands alone", () => {
  assert.equal(updateFailureLine("下载更新失败，原因是更新源返回 HTTP 503。解决建议：稍后重试更新。"), "下载更新失败，原因是更新源返回 HTTP 503");
  assert.equal(updateFailureLine("更新已暂停，可稍后重试"), "更新已暂停，可稍后重试");
});
