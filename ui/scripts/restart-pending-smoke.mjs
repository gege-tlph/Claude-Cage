// Settings saved while the environment runs that apply at its next start:
// the workbench asks once, "Later" leaves a status bar item and row tags,
// "Restart now" sends one restart, and all of it goes once applied.
import assert from "node:assert/strict";
import { createWorkbench } from "./workbench-fixture.mjs";

const lang = process.env.SMOKE_LANG || "zh-CN";
const en = lang === "en";
const t = en
  ? { title: "Restart to apply changes", names: "Prefer smoothness · Memory limit", consequence: "Terminal sessions end; files are kept.", later: "Later", now: "Restart now", pending: "Restart pending", settings: "Settings", general: "General" }
  : { title: "重启环境以应用更改", names: "优先流畅 · 内存上限", consequence: "终端会话会结束，文件会保留。", later: "稍后", now: "立即重启", pending: "待重启", settings: "设置", general: "通用" };
const restarts = [];
const { browser, page, state, errors } = await createWorkbench({
  waitForReady: false,
  beforeNavigate: async page => {
    if (en) await page.route("**/api/client/language", route => route.fulfill({ json: { language: lang, active: lang, system: lang } }));
    await page.route("**/api/host-tools", route => route.fulfill({ json: { plugins: [] } }));
    await page.route("**/api/environment/restart", route => {
      restarts.push(route.request().method());
      state.status.job = { id: "restart-1", kind: "restart", running: false, error: "", message: "" };
      state.status.restart_pending = [];
      return route.fulfill({ json: state.status.job });
    });
  },
});
try {
  Object.assign(state.status, { vm_running: true, ready: true, restart_pending: [] });
  Object.assign(state.settings, { balanced_display: false, project_file_cache: true });
  const dialog = page.getByRole("dialog");
  const statusItem = page.locator(".status-bar").getByRole("button", { name: t.pending });
  await page.locator(".status-bar").waitFor();
  // Saving two such settings while running asks once, naming both.
  state.status.restart_pending = ["balanced_display", "memory"];
  await dialog.getByRole("heading", { name: t.title, exact: true }).waitFor();
  await dialog.getByText(t.names, { exact: true }).waitFor();
  await dialog.getByText(t.consequence, { exact: true }).waitFor();
  await dialog.getByRole("button", { name: t.later, exact: true }).click();
  await dialog.waitFor({ state: "detached" });
  assert.equal(restarts.length, 0, "Later restarted the environment");
  await statusItem.waitFor();
  // Still pending at the next status: no second prompt.
  await page.waitForTimeout(2500);
  assert.equal(await dialog.count(), 0, "the same pending settings asked again");

  // The changed setting is marked in settings.
  await page.getByRole("button", { name: t.settings, exact: true }).click();
  await page.getByRole("navigation").getByRole("button", { name: t.general, exact: true }).click();
  const row = page.locator(".settings-row").filter({ hasText: en ? "Prefer smoothness" : "优先流畅" });
  await row.locator(".restart-tag").waitFor();
  await page.keyboard.press("Escape");

  // The status bar item reopens the dialog; Restart now sends one restart.
  await statusItem.click();
  await dialog.getByRole("button", { name: t.now, exact: true }).click();
  await statusItem.waitFor({ state: "detached", timeout: 10000 });
  assert.deepEqual(restarts, ["POST"]);
  assert.deepEqual(errors, []);
  console.log(`[${lang}] restart pending: asked once, Later keeps the status item and row tag, Restart now sends one restart.`);
} finally {
  await browser.close();
}
