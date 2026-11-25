import assert from "node:assert/strict";
import { createWorkbench } from "./workbench-fixture.mjs";

// A link a program prints in the terminal (OSC 8) comes from the guest. After
// the user confirms, it opens in the environment's browser: the desktop shell
// blocks window.open, and the host browser would leave the isolation.
const fixture = await createWorkbench({ mockWebSockets: false });
const { page } = fixture;
const opened = [];
const dialogs = [];
try {
  await page.route("**/api/browser/new", async (route) => {
    opened.push(JSON.parse(route.request().postData() || "{}"));
    await route.fulfill({ json: { id: "link-tab" } });
  });
  page.on("dialog", (dialog) => { dialogs.push(dialog.message()); void dialog.accept(); });
  await page.routeWebSocket("**/api/**", (socket) => {
    if (!socket.url().includes("/terminals/")) return socket.close();
    socket.send(JSON.stringify({ type: "history", offset: 0, size: 0, reset: false }));
    socket.send(Buffer.from("\x1b]8;;https://example.com/docs\x07OPEN-THIS-LINK\x1b]8;;\x07\r\n"));
  });
  await page.reload();
  await page.getByRole("button", { name: "启动环境", exact: true }).click();
  await page.getByRole("button", { name: "新建终端", exact: true }).first().click();
  await page.getByRole("menuitem", { name: "普通终端", exact: true }).click();
  const link = page.locator(".xterm-rows").getByText("OPEN-THIS-LINK").first();
  await link.waitFor();
  const box = await link.boundingBox();
  await page.mouse.move(box.x + 4, box.y + box.height / 2);
  await page.waitForTimeout(200);
  await page.mouse.click(box.x + 4, box.y + box.height / 2);
  await page.waitForFunction(() => true, null, { timeout: 100 });
  const deadline = Date.now() + 5000;
  while (!opened.length && Date.now() < deadline) await page.waitForTimeout(50);
  assert.equal(dialogs.length, 1, "the link asked for confirmation");
  assert.ok(dialogs[0].includes("https://example.com/docs"));
  assert.deepEqual(opened, [{ url: "https://example.com/docs", incognito: false }]);
  assert.deepEqual(fixture.errors, []);
  console.log("PASS: a confirmed terminal link opens in the environment browser");
} finally {
  await fixture.browser.close();
}
