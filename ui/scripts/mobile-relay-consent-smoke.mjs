// #740: the official relay is contacted only after the user confirms its
// address, and a paired phone shows which relay it keeps using.
import assert from "node:assert/strict";
import { createWorkbench } from "./workbench-fixture.mjs";

const lang = process.env.SMOKE_LANG || "zh-CN";
const text = lang === "en"
  ? { settings: "Settings", remote: "Remote connection", generate: "Generate pairing QR code", title: "Connect to the Cage official relay?", body: "43.142.88.90 · Direct", connect: "Connect", cancel: "Cancel", relay: "Relay", reconnecting: "Reconnecting" }
  : { settings: "设置", remote: "远程连接", generate: "生成配对二维码", title: "连接 Cage 官方中继？", body: "43.142.88.90 · 直连", connect: "连接", cancel: "取消", relay: "中继", reconnecting: "正在重连" };
let paired = false;
const posts = [];
const { browser, page, errors } = await createWorkbench({
  waitForReady: false,
  beforeNavigate: async page => {
    if (lang !== "zh-CN") await page.route("**/api/client/language", route => route.fulfill({ json: { language: lang, active: lang, system: lang } }));
    await page.route("**/api/mobile-pair", async route => {
      if (route.request().method() === "POST") {
        posts.push(route.request().postDataJSON());
        return route.fulfill({ json: { image: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==", expires: new Date(Date.now() + 120000).toISOString(), manual: "CAGE-test" } });
      }
      const state = { paired, status: "", device: null, official_relay: "43.142.88.90" };
      if (paired) Object.assign(state, { relay: "43.142.88.90", link: "正在重连" });
      return route.fulfill({ json: state });
    });
  },
});
try {
  await page.getByRole("button", { name: text.settings, exact: true }).click();
  await page.getByRole("navigation").getByRole("button", { name: text.remote, exact: true }).click();
  const generate = page.getByRole("button", { name: text.generate, exact: true });
  await generate.waitFor();
  await page.waitForFunction(() => !document.querySelector(".remote-pair-actions button.primary")?.disabled);

  // Cancel: nothing is sent.
  await generate.click();
  await page.getByRole("heading", { name: text.title, exact: true }).waitFor();
  await page.getByText(text.body, { exact: true }).waitFor();
  if (process.env.SMOKE_OUT) { await page.waitForTimeout(800); await page.screenshot({ path: `${process.env.SMOKE_OUT}/relay-confirm-${lang}.png` }); }
  await page.getByRole("button", { name: text.cancel, exact: true }).click();
  await page.getByRole("heading", { name: text.title, exact: true }).waitFor({ state: "hidden" });
  assert.equal(posts.length, 0, "cancelled dialog still contacted the relay");

  // Confirm: the request names the confirmed relay.
  await generate.click();
  await page.getByRole("button", { name: text.connect, exact: true }).click();
  await page.locator(".remote-pair-code img").waitFor();
  assert.deepEqual(posts, [{ relay: "", admin: "", confirm: "43.142.88.90" }]);

  // Paired: the relay and its link state are visible.
  paired = true;
  const relay = page.getByLabel(text.relay, { exact: true });
  await relay.getByText(text.reconnecting, { exact: true }).waitFor();
  assert.equal(await relay.innerText(), `${text.relay}\n43.142.88.90\n${text.reconnecting}`);
  if (process.env.SMOKE_OUT) await page.screenshot({ path: `${process.env.SMOKE_OUT}/relay-paired-${lang}.png` });
  assert.deepEqual(errors, []);
  console.log(`[${lang}] official relay: cancel sends nothing, confirm sends the confirmed host, paired relay and link state shown.`);
} finally {
  await browser.close();
}
