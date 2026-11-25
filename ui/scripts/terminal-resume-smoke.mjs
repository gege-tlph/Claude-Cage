import assert from "node:assert/strict";
import { createWorkbench } from "./workbench-fixture.mjs";

// A dropped terminal stream reconnects on its own and asks for the output
// after the last byte it received. History that continues the screen is
// appended; history that does not is drawn on a cleared screen.
const fixture = await createWorkbench({ mockWebSockets: false });
const { page, state } = fixture;
const urls = [];
const sockets = [];
const rows = () => page.locator(".xterm-rows").innerText();
const waitRows = (text) => page.waitForFunction((value) =>
  document.querySelector(".xterm-rows")?.textContent.includes(value), text, { timeout: 10000 });
const waitConnections = async (count) => {
  const deadline = Date.now() + 10000;
  while (sockets.length < count && Date.now() < deadline) await page.waitForTimeout(20);
  assert.equal(sockets.length, count, "terminal did not reconnect on its own");
};

try {
  await page.route("**/api/terminals", (route) => {
    if (route.request().method() !== "POST") return route.fallback();
    const tab = { id: "resume-test", title: "终端 1", kind: "shell", workspace: state.settings.workspace };
    state.terminals = [tab];
    return route.fulfill({ json: tab });
  });
  const first = "first-part ";
  const live = "live-part ";
  await page.routeWebSocket("**/api/**", (ws) => {
    if (!ws.url().includes("/terminals/")) return ws.close();
    urls.push(new URL(ws.url()).searchParams.get("after"));
    sockets.push(ws);
    if (sockets.length === 1) {
      ws.send(JSON.stringify({ type: "history", offset: 0, size: first.length, reset: false }));
      ws.send(Buffer.from(first));
      ws.send(Buffer.from(live));
    }
  });
  await page.reload();
  await page.getByRole("button", { name: "启动环境", exact: true }).click();
  await page.getByRole("button", { name: "新建终端", exact: true }).first().click();
  await page.getByRole("menuitem", { name: "普通终端", exact: true }).click();
  await waitRows("live-part");
  assert.equal(urls[0], "0");

  // Resume: only the missed output arrives, after what is on screen.
  sockets[0].close();
  await waitConnections(2);
  assert.equal(urls[1], String(first.length + live.length));
  sockets[1].send(JSON.stringify({ type: "history", offset: first.length + live.length, size: 7, reset: false }));
  sockets[1].send(Buffer.from("missed "));
  sockets[1].send(Buffer.from("after-resume"));
  await waitRows("after-resume");
  assert.match(await rows(), /first-part live-part missed after-resume/);
  assert.equal(await page.getByRole("button", { name: "重新连接", exact: true }).count(), 0);

  // The output after that offset is gone: the screen starts over.
  sockets[1].close();
  await waitConnections(3);
  sockets[2].send(JSON.stringify({ type: "history", offset: 500, size: 10, reset: true }));
  sockets[2].send(Buffer.from("kept-tail "));
  await waitRows("kept-tail");
  const screen = await rows();
  assert.ok(!screen.includes("first-part"), "a reset replay kept the old screen");

  // A session that ended does not reconnect.
  sockets[2].send(JSON.stringify({ type: "exit" }));
  sockets[2].close();
  await page.waitForTimeout(1500);
  assert.equal(sockets.length, 3, "an ended session reconnected");
  assert.deepEqual(fixture.errors, []);
  console.log("PASS: automatic reconnect resumes after the received offset; reset replays clear the screen");
} finally {
  await fixture.browser.close();
}
