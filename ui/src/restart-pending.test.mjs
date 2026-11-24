import assert from "node:assert/strict";
import test from "node:test";
import { newlyPending, restartDialog } from "./restart-pending.mjs";

test("only settings that became pending ask for a restart", () => {
  assert.deepEqual(newlyPending(null, ["memory"]), []);
  assert.deepEqual(newlyPending([], ["balanced_display", "memory"]), ["balanced_display", "memory"]);
  assert.deepEqual(newlyPending(["memory"], ["memory"]), []);
  assert.deepEqual(newlyPending(["memory"], ["memory", "cpus"]), ["cpus"]);
  assert.deepEqual(newlyPending(["memory"], []), []);
  assert.deepEqual(newlyPending(["memory"], undefined), []);
});

test("the restart dialog names every pending setting", () => {
  const dialog = restartDialog(["balanced_display", "memory", "unknown"], (text) => `<${text}>`);
  assert.equal(dialog.title, "重启环境以应用更改");
  assert.deepEqual(dialog.body, ["<优先流畅> · <内存上限> · <unknown>", "终端会话会结束，文件会保留。"]);
  assert.equal(dialog.confirm, "立即重启");
  assert.equal(dialog.cancel, "稍后");
});
