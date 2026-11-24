// #883: beside another pane, the default Claude Desktop keeps its guest at
// least DESKTOP_MIN_WIDTH logical pixels wide and noVNC scales the frame into
// the half-width pane; shown alone, the guest follows the pane as before.
// SMOKE_LANG=en runs the same checks in English.
import assert from "node:assert/strict";
import { createWorkbench } from "./workbench-fixture.mjs";
import { startDisplay } from "./rfb-wire-fixture.mjs";

const lang = process.env.SMOKE_LANG || "zh-CN";
const en = lang === "en";
const t = en
  ? { beside: "Open side by side", exit: "Exit split view", running: "· running" }
  : { beside: "并排打开", exit: "退出分屏", running: "· 运行中" };
const minWidth = 800, density = 2;
const instanceA = "b".repeat(24);
const accounts = [{ id: "a".repeat(24), account_id: "a".repeat(24), number: 1, label: "工作账号", running: true, dedicated: false }];
const instances = [
  { id: "default", number: 0, label: "默认 Desktop", running: true, dedicated: false },
  { id: instanceA, account_id: accounts[0].id, number: 1, label: "工作账号", running: true, dedicated: false },
];
const display = await startDisplay(process.env.CAGE_UI_URL || "http://127.0.0.1:5198", { frameInterval: 500 });
const resizes = [];
const { browser, page, errors } = await createWorkbench({
  mockWebSockets: false, url: display.url, viewport: { width: 1280, height: 800 },
  beforeNavigate: async (page, fixture) => {
    Object.assign(fixture.status, { ready: true, vm_running: true, account_desktops_supported: true, desktop_catalog_revision: 1 });
    if (en) await page.route("**/api/client/language", route => route.fulfill({ json: { language: lang, active: lang, system: lang } }));
    await page.route("**/api/claude-desktop**", route => {
      const path = new URL(route.request().url()).pathname;
      if (path.endsWith("/vnc-credentials")) return route.fulfill({ json: { password: "fixture", display_scale: density } });
      if (path.endsWith("/resize")) {
        const size = route.request().postDataJSON();
        resizes.push(size);
        display.resize(size);
        return route.fulfill({ json: { ok: true } });
      }
      return route.fulfill({ json: { installed: true, running: true, version: "1.0.0", failed: false } });
    });
    await page.route("**/api/desktop-instances**", route => {
      const path = new URL(route.request().url()).pathname.replace("/api", "");
      if (path === "/desktop-instances") return route.fulfill({ json: { supported: true, accounts, instances, redirects: {} } });
      if (path.endsWith("/vnc-credentials")) return route.fulfill({ json: { password: "fixture", display_scale: density } });
      return route.fulfill({ json: { id: "job-instance", running: false } });
    });
  },
});
page.setDefaultTimeout(10000);
// The guest size the default pane asks for once it settles, and that pane's box.
async function settled(expect) {
  const canvas = page.locator("#desktop-pane-default .remote-canvas");
  let box, size;
  for (let tries = 0; tries < 60; tries++) {
    box = await canvas.boundingBox();
    size = resizes.at(-1);
    if (box && size && size.width === expect(box).width && size.height === expect(box).height) return { box, size };
    await page.waitForTimeout(150);
  }
  assert.fail(`guest ${JSON.stringify(size)} for pane ${JSON.stringify(box)}; expected ${JSON.stringify(box && expect(box))}`);
}
const follows = box => ({ width: Math.floor(box.width * density), height: Math.floor(box.height * density) });
const floored = box => box.width >= minWidth ? follows(box) : {
  width: Math.floor(minWidth * density), height: Math.floor(box.height * minWidth / box.width * density),
};
try {
  await page.getByRole("button", { name: "Claude Desktop", exact: true }).click();
  await page.locator(".account-desktop-row").first().getByRole("button").first().click();
  const alone = await settled(follows);
  assert.ok(alone.box.width >= minWidth, `the default pane alone is narrower than ${minWidth}: ${alone.box.width}`);

  const instanceRow = page.locator(".account-desktop-row").filter({ has: page.getByRole("button", { name: `#01 工作账号 ${t.running}`, exact: true }) });
  await instanceRow.getByRole("button", { name: t.beside, exact: true }).click();
  await page.locator(`#desktop-pane-${instanceA}`).waitFor();
  const half = await settled(floored);
  assert.ok(half.box.width < minWidth, `the half-width pane is not narrower than ${minWidth}: ${half.box.width}`);
  assert.equal(half.size.width, minWidth * density, "the guest beside another pane is narrower than its minimum");
  // The whole frame is scaled into the pane: nothing is cut off on the right.
  const shown = await page.locator("#desktop-pane-default .remote-canvas canvas").boundingBox();
  assert.ok(Math.abs(shown.width - half.box.width) < 2, `the frame is ${shown.width} wide in a ${half.box.width} pane`);

  await page.locator("#desktop-pane-default").getByRole("button", { name: t.exit, exact: true }).click();
  await page.locator("#desktop-pane-default").waitFor({ state: "detached" });
  await page.locator(".account-desktop-row").first().getByRole("button").first().click();
  await settled(follows);

  assert.deepEqual(errors, []);
  console.log(`desktop split width (${lang}): alone ${alone.size.width}px follows the pane, beside ${half.size.width}px for a ${half.box.width}px pane, alone again follows`);
} finally {
  await browser.close();
  await display.close?.();
}
