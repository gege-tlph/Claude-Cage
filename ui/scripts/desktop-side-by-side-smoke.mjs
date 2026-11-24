// #883: the default Desktop and an independent instance side by side in one
// workbench, from either entry: the instance row's split icon while the
// default Desktop is shown, and the default row's split icon while an
// instance is shown. A running Desktop is only connected, never restarted.
// SMOKE_LANG=en runs the same checks in English.
import assert from "node:assert/strict";
import { createWorkbench } from "./workbench-fixture.mjs";

const lang = process.env.SMOKE_LANG || "zh-CN";
const en = lang === "en";
const t = en
  ? { beside: "Open side by side", exit: "Exit split view", collapse: "Collapse sidebar", expand: "Expand sidebar", running: "· running" }
  : { beside: "并排打开", exit: "退出分屏", collapse: "收起侧栏", expand: "展开侧栏", running: "· 运行中" };
const instanceA = "b".repeat(24);
const accounts = [{ id: "a".repeat(24), account_id: "a".repeat(24), number: 1, label: "工作账号", running: true, dedicated: false }];
// The backend names an unrenamed default Desktop this way in every language.
const instances = [
  { id: "default", number: 0, label: "默认 Desktop", running: true, dedicated: false },
  { id: instanceA, account_id: accounts[0].id, number: 1, label: "工作账号", running: true, dedicated: false },
];
const starts = [];
const sockets = [];
const { browser, page, state, errors } = await createWorkbench({
  beforeNavigate: async (page, fixture) => {
    Object.assign(fixture.status, { ready: true, vm_running: true, account_desktops_supported: true, desktop_catalog_revision: 1 });
    if (en) await page.route("**/api/client/language", route => route.fulfill({ json: { language: lang, active: lang, system: lang } }));
    // Registered after the fixture's socket mock, so it sees the screen connections first.
    await page.routeWebSocket(url => url.pathname.endsWith("/vnc"), socket => { sockets.push(new URL(socket.url()).pathname); socket.close(); });
    await page.route("**/api/claude-desktop**", route => {
      const path = new URL(route.request().url()).pathname;
      if (path.endsWith("/vnc-credentials")) return route.fulfill({ json: { password: "fixture1" } });
      if (route.request().method() !== "GET") {
        starts.push(path);
        return route.fulfill({ json: { id: "job-default", running: false } });
      }
      return route.fulfill({ json: { installed: true, running: true, version: "1.0.0", failed: false } });
    });
    await page.route("**/api/desktop-instances**", route => {
      const path = new URL(route.request().url()).pathname.replace("/api", "");
      if (path === "/desktop-instances") return route.fulfill({ json: { supported: true, accounts, instances, redirects: {} } });
      if (path.endsWith("/vnc-credentials")) return route.fulfill({ json: { password: "fixture1" } });
      if (route.request().method() === "POST") starts.push(path);
      return route.fulfill({ json: { id: "job-instance", running: false } });
    });
  },
});
try {
  const rows = page.locator(".account-desktop-row");
  const defaultRow = rows.first();
  const instanceRow = rows.filter({ has: page.getByRole("button", { name: `#01 工作账号 ${t.running}`, exact: true }) });
  const defaultPane = page.locator("#desktop-pane-default");
  const instancePane = page.locator(`#desktop-pane-${instanceA}`);
  const assertSideBySide = async (entry) => {
    await defaultPane.waitFor();
    await instancePane.waitFor();
    const [left, right] = [await defaultPane.boundingBox(), await instancePane.boundingBox()];
    assert.ok(left.x + left.width <= right.x + 1, `${entry}: the default Desktop is not on the left`);
    assert.ok(Math.abs(left.width - right.width) <= 2, `${entry}: the two panes differ in width (${left.width} / ${right.width})`);
    assert.deepEqual(starts, [], `${entry}: a running Desktop was started again`);
  };

  await page.getByRole("button", { name: "Claude Desktop", exact: true }).click();
  await defaultRow.getByRole("button").first().click();
  await defaultPane.waitFor();
  assert.equal(await instancePane.count(), 0);

  // Entry 1: the instance row's split icon while the default Desktop is shown.
  await instanceRow.getByRole("button", { name: t.beside, exact: true }).click();
  await assertSideBySide("instance entry");
  // Each pane opens its own screen connection.
  for (const path of ["/api/claude-desktop/vnc", `/api/desktop-instances/${instanceA}/vnc`]) {
    for (let tries = 0; !sockets.includes(path) && tries < 50; tries++) await page.waitForTimeout(100);
    assert.ok(sockets.includes(path), `no screen connection to ${path}: ${sockets}`);
  }
  // The split icon is gone from a row whose Desktop is already shown.
  assert.equal(await instanceRow.getByRole("button", { name: t.beside, exact: true }).count(), 0);
  assert.equal(await defaultRow.getByRole("button", { name: t.beside, exact: true }).count(), 0);

  // A collapsed sidebar leaves one reveal button, not one per pane.
  await page.getByRole("button", { name: t.collapse, exact: true }).click();
  assert.equal(await page.getByRole("button", { name: t.expand, exact: true }).count(), 1);
  await page.getByRole("button", { name: t.expand, exact: true }).click();

  // Exit split view folds the default pane only; the instance keeps running.
  await defaultPane.getByRole("button", { name: t.exit, exact: true }).click();
  await defaultPane.waitFor({ state: "detached" });
  await instancePane.waitFor();

  // Entry 2: the default row's split icon while an instance is shown.
  await defaultRow.getByRole("button", { name: t.beside, exact: true }).click();
  await assertSideBySide("default entry");

  assert.deepEqual(errors, []);
  console.log(`desktop side by side (${lang}): both entries open default left and instance right, exit folds one pane, no restart`);
} finally {
  await browser.close();
}
