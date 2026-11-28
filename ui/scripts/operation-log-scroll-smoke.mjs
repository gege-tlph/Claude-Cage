// The advanced 操作日志 view draws the newest events and adds older ones as
// it nears its top. Each time lines are added above, the first line in view
// stays where it was: the view neither jumps down nor snaps back to the end.
import assert from "node:assert/strict";
import { createWorkbench } from "./workbench-fixture.mjs";

const base = Date.now() - 3000 * 1000;
const events = Array.from({ length: 1200 }, (_, i) => ({
  time: new Date(base + i * 1000).toISOString(), level: "info", source: "src", message: `event-${String(i).padStart(5, "0")}`,
}));
const { browser, page, errors } = await createWorkbench({
  beforeNavigate: async (page, state) => {
    Object.assign(state.status, { ready: true, vm_running: true, job: { id: "j1", kind: "start", running: false } });
    state.jobLog = { entries: [], outputs: [] };
    state.jobLogEvents = events;
  },
});
try {
  await page.evaluate(() => localStorage.setItem("cage-operation-log-view", "raw"));
  await page.locator(".status-bar").waitFor();
  await page.getByRole("button", { name: "操作日志", exact: true }).click();
  const body = page.locator(".operation-log-raw");
  await body.locator(".operation-log-event").first().waitFor();
  // The view opens at its end.
  await page.waitForFunction(() => { const el = document.querySelector(".operation-log-raw"); return el && el.scrollTop > 0 && el.scrollTop + el.clientHeight >= el.scrollHeight - 2; });
  await page.waitForTimeout(300);
  // probe names the first line in view, after moving the view to scrollTop
  // when given. The scroll event fires later, so the line is the one the
  // reader stopped at, before older lines are added.
  const probe = (scrollTop) => body.evaluate((el, scrollTop) => {
    if (scrollTop !== undefined) el.scrollTop = scrollTop;
    const top = el.getBoundingClientRect().top;
    const lines = [...el.querySelectorAll(".operation-log-event")];
    const first = lines.find((line) => line.getBoundingClientRect().bottom > top + 1);
    return { drawn: lines.length, first: Number(first?.textContent.match(/event-(\d+)/)?.[1]) };
  }, scrollTop);
  const start = await probe();
  assert.equal(start.drawn, 200, "the view did not start with the newest 200 events");
  let previous = start;
  for (let step = 0; step < 2; step++) {
    // Stop 100px from the top, as a slow scroll would.
    const stopped = await probe(100);
    await page.waitForFunction((count) => document.querySelectorAll(".operation-log-raw .operation-log-event").length > count, stopped.drawn);
    await page.waitForTimeout(300);
    const after = await probe();
    assert.ok(after.drawn > previous.drawn, `step ${step}: no older events were added`);
    assert.ok(Math.abs(after.first - stopped.first) <= 1, `step ${step}: first line in view moved from event-${stopped.first} to event-${after.first}`);
    previous = after;
  }
  assert.deepEqual(errors, []);
  console.log("operation log scroll smoke passed");
} finally {
  await browser.close();
}
