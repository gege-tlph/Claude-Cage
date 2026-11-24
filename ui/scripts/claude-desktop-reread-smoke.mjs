// When the first status read fails, "Reload status" reads it again and the
// panel recovers. The click event must not reach refresh() as its argument.
import assert from "node:assert/strict";
import { createWorkbench } from "./workbench-fixture.mjs";

const lang = process.env.SMOKE_LANG || "zh-CN";
const en = lang === "en";
const t = en
  ? { desktop: "Claude Desktop", reread: "Reload status", install: "Download and install" }
  : { desktop: "Claude Desktop", reread: "重新读取状态", install: "下载并安装" };
let reads = 0;
const { browser, page, state, errors } = await createWorkbench({
  beforeNavigate: async (page, fixture) => {
    fixture.status.ready = fixture.status.vm_running = true;
    if (en) await page.route("**/api/client/language", route => route.fulfill({ json: { language: lang, active: lang, system: lang } }));
    await page.route("**/api/claude-desktop**", route => {
      if (route.request().method() !== "GET") return route.fulfill({ json: {} });
      reads += 1;
      if (reads === 1) return route.fulfill({ status: 502, contentType: "text/plain", body: "测试：状态暂不可用" });
      return route.fulfill({ json: { installed: false, running: false, version: "", failed: false } });
    });
  },
});
try {
  await page.getByRole("button", { name: t.desktop, exact: true }).click();
  const reread = page.getByRole("button", { name: t.reread, exact: true });
  await reread.waitFor();
  const before = reads;
  await reread.click();
  await page.getByRole("button", { name: t.install, exact: true }).waitFor();
  assert.ok(reads > before, "the status was not read again");
  assert.equal(await reread.count(), 0);
  assert.deepEqual(errors, [], "the click raised a page error");
  console.log(`claude desktop reread (${lang}): status read again, panel recovered`);
} finally {
  await browser.close();
}
