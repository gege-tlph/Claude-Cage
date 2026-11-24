// #846: storage that can be neither used nor recovered offers a rebuild that
// asks first, sends nothing on cancel, and shows where the old storage went.
import assert from "node:assert/strict";
import { createWorkbench } from "./workbench-fixture.mjs";

const lang = process.env.SMOKE_LANG || "zh-CN";
const en = lang === "en";
const t = en
  ? { settings: "Settings", page: "Environment and recovery", panel: "Local environment storage", rebuild: "Rebuild environment", title: "Rebuild the environment?", cancel: "Cancel", kept: "Storage before the rebuild is in" }
  : { settings: "设置", page: "环境与恢复", panel: "本机环境存储", rebuild: "重建环境", title: "重建环境？", cancel: "取消", kept: "重建前的存储保存在" };
const unplugged = "环境存储不可用，请重新连接原磁盘：E:\\CageData";
const archive = "C:\\Users\\demo\\.claude-cage\\state-reset-20261124-170000";
let location = { unavailable: { error: unplugged } };
const resets = [];
const { browser, page, state, errors } = await createWorkbench({
  waitForReady: false,
  clientContext: { mode: "local", connection_id: "local", client_platform: "windows", server_platform: "windows", connection: { state: "disconnected" } },
  beforeNavigate: async page => {
    if (en) await page.route("**/api/client/language", route => route.fulfill({ json: { language: lang, active: lang, system: lang } }));
    await page.route("**/api/host-tools", route => route.fulfill({ json: { plugins: [] } }));
    await page.route("**/api/environment/storage", route => route.fulfill({ json: location }));
    await page.route("**/api/environment/storage/reset", route => {
      resets.push(route.request().postDataJSON());
      location = { directory: "C:\\Users\\demo\\.claude-cage\\state", has_environment: false, reset: archive };
      state.status.job = { kind: "storage-recovery", id: "reset-1", running: false, error: "", message: "环境存储已重建" };
      return route.fulfill({ json: state.status.job });
    });
  },
});
try {
  state.status.local_vm_storage = true;
  await page.getByRole("button", { name: t.settings, exact: true }).click();
  await page.getByRole("button", { name: t.page, exact: true }).click();
  const panel = page.getByRole("region", { name: t.panel });
  await panel.getByRole("alert").filter({ hasText: "E:\\CageData" }).waitFor();
  const rebuild = panel.getByRole("button", { name: t.rebuild, exact: true });

  await rebuild.click();
  await page.getByRole("heading", { name: t.title, exact: true }).waitFor();
  await page.getByRole("dialog").getByRole("button", { name: t.cancel, exact: true }).click();
  assert.equal(resets.length, 0, "cancelled rebuild was sent");

  await rebuild.click();
  await page.getByRole("dialog").getByRole("button", { name: t.rebuild, exact: true }).click();
  await panel.getByText(archive, { exact: true }).waitFor();
  assert.equal(resets.length, 1);
  assert.match(await panel.innerText(), new RegExp(t.kept));
  assert.equal(await rebuild.count(), 0, "rebuild still offered for usable storage");
  assert.deepEqual(errors, []);
  console.log(`[${lang}] unusable storage: rebuild asks first, cancel sends nothing, archive shown after rebuild.`);
} finally {
  await browser.close();
}
