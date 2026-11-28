// A plugin with settings shows its settings form only on the 设置 tab; on
// 工具 and 授权与权限 the form is hidden. A draft survives polling and tab
// switches, and untouched fields follow the server value.
import assert from "node:assert/strict";
import { createWorkbench } from "./workbench-fixture.mjs";

let serverValue = "server-1";
let polls = 0;
const view = () => ({
  available: true, enabled: true, directory: "/plugins",
  plugins: [{
    name: "demo", display_name: "Demo 插件", supported: true, loaded: true, kind: "host", enabled: true, source: "local",
    tools: [{ name: "demo_tool", description: "tool" }], permissions: [],
    settings: [{ id: "endpoint", title: "Endpoint", type: "text", value: serverValue },
      { id: "other", title: "Other", type: "text", value: serverValue }],
  }],
  catalog: [], entries: [],
});
const { browser, page, errors } = await createWorkbench({
  beforeNavigate: async (page, state) => {
    state.status.ready = state.status.vm_running = true;
    state.status.local_vm_storage = "C:\\CageData";
    await page.route("**/api/host-tools**", route => {
      const name = new URL(route.request().url()).pathname;
      if (name.endsWith("/host-tools")) { polls++; return route.fulfill({ json: view() }); }
      if (name.endsWith("/market")) return route.fulfill({ json: { items: [], loaded: true } });
      if (name.endsWith("/drafts")) return route.fulfill({ json: { items: [], ready: true } });
      return route.fulfill({ json: {} });
    });
  },
});
try {
  await page.getByRole("button", { name: "插件" }).first().click();
  await page.getByText("Demo 插件").first().click();
  const form = page.locator("form.plugin-config-form");
  await page.getByRole("tab", { name: /工具/ }).click();
  await page.getByText("demo_tool").first().waitFor();
  assert.equal(await form.isVisible(), false, "settings form shows on 工具");
  await page.getByRole("tab", { name: /授权与权限/ }).click();
  assert.equal(await form.isVisible(), false, "settings form shows on 授权与权限");
  await page.getByRole("tab", { name: /设置/ }).click();
  await form.waitFor();
  const input = page.getByLabel("Endpoint");
  await input.fill("my-draft");
  serverValue = "server-2";
  const before = polls;
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  const other = page.getByLabel("Other");
  for (let waited = 0; waited < 15000 && (await other.inputValue()) !== "server-2"; waited += 250) await page.waitForTimeout(250);
  assert.ok(polls > before, "no poll while waiting");
  assert.equal(await input.inputValue(), "my-draft", "poll replaced the draft");
  assert.equal(await page.getByLabel("Other").inputValue(), "server-2", "untouched field kept the old server value");
  await page.getByRole("tab", { name: /工具/ }).click();
  await page.getByRole("tab", { name: /设置/ }).click();
  assert.equal(await page.getByLabel("Endpoint").inputValue(), "my-draft", "tab switch dropped the draft");
  assert.deepEqual(errors, []);
  console.log("plugin config form smoke passed");
} finally {
  await browser.close();
}
