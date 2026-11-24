// The empty download panel names its state once: "暂无下载" / "No downloads".
// SMOKE_LANG=en runs the same check in English.
import assert from "node:assert/strict";
import { createWorkbench } from "./workbench-fixture.mjs";

const lang = process.env.SMOKE_LANG || "zh-CN";
const en = lang === "en";
const t = en
  ? { browser: "Open browser", trigger: "Browser downloads", panel: "Download file", empty: "No downloads" }
  : { browser: "打开浏览器", trigger: "浏览器下载", panel: "下载文件", empty: "暂无下载" };
const { browser, page, state, errors } = await createWorkbench({
  beforeNavigate: async (page, fixture) => {
    fixture.status.ready = fixture.status.vm_running = true;
    if (en) await page.route("**/api/client/language", route => route.fulfill({ json: { language: lang, active: lang, system: lang } }));
    await page.route("**/api/browser/tabs", route => route.fulfill({ json: { tabs: state.tabs, active: state.active, downloads: [] } }));
    await page.route("**/api/browser/files/saved", route => route.fulfill({ json: [] }));
  },
});
try {
  await page.getByRole("button", { name: t.browser, exact: true }).click();
  await page.getByRole("button", { name: t.trigger, exact: true }).click();
  const panel = page.getByRole("dialog", { name: t.panel, exact: true });
  await panel.waitFor();
  const empty = panel.locator(".browser-download-empty");
  await empty.waitFor();
  assert.equal(await panel.getByText(t.empty, { exact: true }).count(), 1, "the empty state repeats its name");
  assert.equal((await empty.innerText()).trim(), t.empty);
  assert.deepEqual(errors, []);
  console.log(`browser downloads empty (${lang}): "${t.empty}" shown once`);
} finally {
  await browser.close();
}
