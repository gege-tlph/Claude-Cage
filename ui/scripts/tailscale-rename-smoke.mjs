// A device renamed on the network (#605) shows both names once; confirming
// sends acknowledge-rename and the notice goes.
import assert from "node:assert/strict";
import { createWorkbench } from "./workbench-fixture.mjs";

const lang = process.env.SMOKE_LANG || "zh-CN";
const en = lang === "en";
const t = en
  ? { open: "Open Tailscale", renamed: "Device renamed", ok: "OK" }
  : { open: "打开 Tailscale", renamed: "设备已改名", ok: "确定" };
const actions = [];
let tail = {
  installed: true, version: "1.104.5", enabled: true, backend: "Running", tailnet: "fixture-net", account: "owner@example.invalid",
  name: "workbench.test.ts.net", hostname: "workbench", os: "linux", ips: ["100.88.2.3"], peers: [], keys: [], forwards: [],
  user: "cage", ssh_port: 22, renamed_from: "cage-workbench",
};
const { browser, page, errors } = await createWorkbench({
  beforeNavigate: async (page, fixture) => {
    fixture.status.ready = fixture.status.vm_running = true;
    if (en) await page.route("**/api/client/language", route => route.fulfill({ json: { language: lang, active: lang, system: lang } }));
    await page.route("**/api/tailscale**", route => {
      const action = new URL(route.request().url()).pathname.split("/").at(-1);
      actions.push(action);
      if (action === "acknowledge-rename") tail = { ...tail, renamed_from: "" };
      return route.fulfill({ json: tail });
    });
  },
});
try {
  await page.getByRole("button", { name: t.open, exact: true }).click();
  const panel = page.getByRole("main", { name: "Tailscale", exact: true });
  const notice = panel.getByRole("status").filter({ hasText: t.renamed });
  await notice.waitFor();
  assert.equal(await notice.locator("code").first().textContent(), "cage-workbench");
  assert.equal(await notice.locator("code").nth(1).textContent(), "workbench");
  // Name and state only: no sentence in the notice.
  assert.equal((await notice.locator("p").textContent()).trim(), "cage-workbench → workbench");
  await notice.getByRole("button", { name: t.ok, exact: true }).click();
  await notice.waitFor({ state: "detached" });
  assert.ok(actions.includes("acknowledge-rename"), "the rename was not acknowledged");
  assert.deepEqual(errors, []);
  console.log(`tailscale rename notice (${lang}): shown, acknowledged, gone`);
} finally {
  await browser.close();
}
