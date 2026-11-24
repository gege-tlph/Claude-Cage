// Home page: a storage error under the start button keeps its buttons whole
// (they were squeezed to ~25px and clipped), offers the storage settings, and
// is read again once a job (a new storage location) finishes.
import assert from "node:assert/strict";
import { createWorkbench } from "./workbench-fixture.mjs";

const lang = process.env.SMOKE_LANG || "zh-CN";
const en = lang === "en";
const t = en
  ? { recheck: "Recheck install status", settings: "Environment and recovery", missing: "Environment storage folder no longer exists: " }
  : { recheck: "重新检查安装状态", settings: "环境与恢复", missing: "环境存储目录已不存在：" };
const folder = "C:\\Users\\demo\\Documents\\A very long folder name for the Cage environment storage\\CageData";
let missing = true;
let reads = 0;
const { browser, page, state, errors } = await createWorkbench({
  waitForReady: false,
  clientContext: { mode: "local", connection_id: "local", client_platform: "windows", server_platform: "windows", connection: { state: "disconnected" } },
  beforeNavigate: async page => {
    if (en) await page.route("**/api/client/language", route => route.fulfill({ json: { language: lang, active: lang, system: lang } }));
    await page.route("**/api/host-tools", route => route.fulfill({ json: { plugins: [] } }));
    await page.route("**/api/environment/guest-systems", route => {
      reads++;
      return missing
        ? route.fulfill({ status: 400, contentType: "text/plain; charset=utf-8", body: `环境存储目录已不存在：${folder}` })
        : route.fulfill({ json: { fresh: true } });
    });
  },
});
try {
  state.status.local_vm_storage = true;
  const alert = page.locator(".guest-system-picker-error [role=alert]");
  await alert.waitFor();
  assert.equal(await alert.innerText(), t.missing + folder);
  const recheck = page.getByRole("button", { name: t.recheck, exact: true });
  const card = await page.locator(".guest-system-picker-error").boundingBox();
  for (const button of [recheck, page.getByRole("button", { name: t.settings, exact: true })]) {
    const box = await button.boundingBox();
    assert.ok(box.width > 80, `button squeezed to ${box.width}px`);
    assert.ok(box.x + box.width <= card.x + card.width + 1, "button leaves the card");
  }

  // A finished job (here: storage moved in settings) reads the state again.
  missing = false;
  const before = reads;
  state.status.job = { id: "storage-1", kind: "storage", running: false, error: "", message: "" };
  await alert.waitFor({ state: "detached", timeout: 10000 });
  assert.ok(reads > before, "picker did not read the state again");
  assert.deepEqual(errors, []);
  console.log(`[${lang}] storage error: buttons whole, settings offered, state reread after a job.`);
} finally {
  await browser.close();
}
