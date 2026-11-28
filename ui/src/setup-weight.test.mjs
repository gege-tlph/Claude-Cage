import test from "node:test";
import assert from "node:assert/strict";
import { setupPercent, stepOf } from "./setup-weight.mjs";

test("labels map to the install's steps", () => {
  assert.equal(stepOf("正在准备 Debian 13 镜像"), 0);
  assert.equal(stepOf("正在下载 Debian 13 镜像"), 1);
  assert.equal(stepOf("正在下载 Debian 13 系统镜像"), 1);
  assert.equal(stepOf("正在校验安装文件"), 2);
  assert.equal(stepOf("正在校验 Debian 镜像"), 2);
  assert.equal(stepOf("正在复制安装文件"), 4);
  assert.equal(stepOf("正在安装 Chromium 浏览器"), 8);
  assert.equal(stepOf("正在下载网页编辑器 code-server"), 12);
  assert.equal(stepOf("正在启动浏览器和编辑器"), 14);
  assert.equal(stepOf("something else"), -1);
});

test("the download moves the number with its bytes, and it never goes back", () => {
  const start = setupPercent({ label: "正在下载 Debian 13 镜像", received: 0, total: 100 });
  const half = setupPercent({ label: "正在下载 Debian 13 镜像", received: 50, total: 100 }, start);
  assert.ok(half.percent > start.percent);
  assert.ok(half.current > 0);
  // An unknown label keeps the last step; a smaller value does not show.
  const unknown = setupPercent({ label: "正在更新软件源" }, half);
  assert.equal(unknown.step, half.step);
  assert.equal(unknown.percent, half.percent);
  const back = setupPercent({ label: "正在准备 Debian 13 镜像" }, half);
  assert.equal(back.percent, half.percent);
  const end = setupPercent({ label: "正在启动浏览器和编辑器" }, half);
  assert.ok(end.percent >= 90 && end.percent < 100);
});
