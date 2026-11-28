// Exercise the actual pointer paths in an isolated workbench.
import assert from "node:assert/strict";
import { createWorkbench } from "./workbench-fixture.mjs";

const { page, state, browser, errors } = await createWorkbench();
try {
  await page.getByRole("button", { name: "启动环境", exact: true }).click();
  await page.getByRole("heading", { name: "环境已就绪" }).waitFor();
  await page.getByRole("button", { name: "打开浏览器", exact: true }).click();
  await page.getByRole("tablist", { name: "浏览器标签页" }).waitFor();
  await page.getByRole("button", { name: "新建网页", exact: true }).click();
  const workspace = page.locator("main.workspace");
  const tabs = page.locator(".browser-tabs [role=tab]");
  const first = state.tabs[0].id;
  const second = state.tabs[1].id;
  async function drag(tab, fraction, cancel = false) {
    await tab.click({ trial: true });
    const from = await tab.boundingBox();
    const to = await workspace.boundingBox();
    await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
    await page.mouse.down();
    await page.mouse.move(to.x + fraction * to.width, to.y + to.height / 2, { steps: 6 });
    await workspace.locator(fraction < 0.5 ? ".split-drop.split-left" : ".split-drop.split-right").waitFor();
    if (cancel) await page.keyboard.press("Escape");
    await page.mouse.up();
    await workspace.locator(".split-drop").waitFor({ state: "hidden" });
  }

  // Reordering inside the strip must still work independently of pane drops.
  await page.waitForFunction(() => {
    const list = [...document.querySelectorAll(".browser-tab")];
    return list.length === 2 && list.every(tab => tab.getBoundingClientRect().width >= 100);
  });
  const start = await tabs.first().boundingBox();
  const end = await tabs.last().boundingBox();
  await page.mouse.move(start.x + start.width / 2, start.y + start.height / 2);
  await page.mouse.down();
  await page.mouse.move(end.x + end.width - 3, end.y + end.height / 2, { steps: 10 });
  await page.mouse.up();
  await page.waitForFunction(id => document.querySelector(".browser-tab").dataset.tabId === id, second);
  const firstTab = page.locator(`.browser-tab[data-tab-id="${first}"] [role=tab]`);
  await drag(firstTab, 0.25, true);
  assert.equal(await workspace.locator(".browser-header-cell").count(), 1);
  await drag(firstTab, 0.75);
  const cells = workspace.locator(".browser-header-cell");
  const left = cells.first(), right = cells.last();
  await right.locator(`[data-browser-tab="${first}"]`).waitFor();
  await left.locator(`[data-browser-tab="${second}"]`).waitFor();
  await workspace.locator(".remote-view:not(.concealed)").waitFor();
  const surface = await workspace.locator(".remote-view:not(.concealed)").boundingBox();
  const box = await workspace.boundingBox();
  assert.equal(Math.round(surface.width), Math.round(box.width));
  assert.ok(state.split.left === second && state.split.right === first);
  const leftAddress = left.getByRole("textbox", { name: "网页地址或搜索" });
  await leftAddress.fill("https://example.com/left");
  await leftAddress.press("Enter");
  assert.equal(state.requests.filter(r => r.path === "/browser/navigate").at(-1).body.id, second);
  assert.equal(await right.getByRole("textbox", { name: "网页地址或搜索" }).inputValue(), state.tabs.find(t => t.id === first).url);
  await left.getByRole("button", { name: "退出分屏" }).click();
  assert.equal(await tabs.count(), 2);
  await page.waitForFunction(() => document.querySelectorAll(".browser-header-cell").length === 1);

  await drag(firstTab, 0.25);
  await left.locator(`[data-browser-tab="${first}"]`).waitFor();
  await page.locator(`.browser-tab[data-tab-id="${first}"] .tab-close`).click();
  await page.waitForFunction(() => !document.querySelector(".workspace-split"));
  await page.setViewportSize({ width: 820, height: 558 });
  const term = { id: "split-term", title: "分屏终端", workspace: state.settings.workspace };
  state.terminals.push(term);
  const terminal = page.getByRole("tab", { name: term.title, exact: true });
  await terminal.waitFor();
  await drag(terminal, 0.75);
  const terminalRight = workspace.locator(".terminal-lane.split-right:visible");
  await terminalRight.getByText(term.title, { exact: true }).waitFor();
  await terminalRight.locator(".xterm-rows").getByText(/Linux terminal/).waitFor();
  const browserBox = await workspace.locator(".browser-lane .browser-chrome").boundingBox();
  const terminalBox = await terminalRight.locator("header").boundingBox();
  assert.ok(browserBox.x + browserBox.width <= terminalBox.x + 1);
  assert.ok((await terminalRight.locator(".terminal-view").boundingBox()).y <
    (await workspace.locator(".browser-lane .remote-view").boundingBox()).y);
  // Cage lite connects every terminal when it is listed (no upstream lazy
  // connection), so only changes in the count are checked.
  const beforeFocus = state.terminalConnections;
  await workspace.locator(".browser-chrome .address-bar input").click();
  await terminalRight.locator("textarea.xterm-helper-textarea").click({ force: true });
  assert.equal(await workspace.locator(".browser-chrome").count(), 1);
  assert.equal(state.terminalConnections, beforeFocus, "moving focus between visible panes keeps the stream");
  const secondTerm = { ...term, id: "split-term-2", title: "第二个终端" };
  state.terminals.push(secondTerm);
  const secondTerminal = page.getByRole("tab", { name: secondTerm.title, exact: true });
  await secondTerminal.waitFor();
  await drag(secondTerminal, 0.25);
  const terminalPane = id => workspace.locator(`[data-terminal-pane="terminal:${id}"]:visible`);
  const primaryPane = terminalPane(term.id);
  const newPane = terminalPane(secondTerm.id);
  await newPane.getByText(secondTerm.title, { exact: true }).waitFor();
  await primaryPane.getByText(term.title, { exact: true }).waitFor();
  await newPane.locator(".xterm-rows").getByText(/Linux terminal/).waitFor();
  const firstBox = await newPane.boundingBox();
  const secondBox = await primaryPane.boundingBox();
  assert.ok(firstBox.x + firstBox.width < secondBox.x);
  assert.equal(await workspace.getByRole("tablist").count(), 0);
  assert.equal(await workspace.locator(".terminal-chrome:visible").count(), 2);
  assert.equal(await workspace.locator(".terminal-view:not(.concealed)").count(), 2);
  await page.setViewportSize({ width: 1360, height: 850 });
  await primaryPane.getByRole("button", { name: "向下拆分" }).click();
  await page.getByRole("menuitem", { name: "普通终端", exact: true }).click();
  const third = terminalPane(state.terminals.at(-1).id);
  await third.waitFor();
  const below = await third.boundingBox();
  const above = await primaryPane.boundingBox();
  assert.equal(above.x, below.x);
  assert.ok(above.y + above.height < below.y);

  const vertical = workspace.getByRole("separator", { name: "调整终端窗格宽度" });
  const horizontal = workspace.getByRole("separator", { name: "调整终端窗格高度" });
  const value = separator => separator.getAttribute("aria-valuenow");
  const initialWidth = await value(vertical);
  await vertical.press("ArrowLeft");
  assert.ok(Number(await value(vertical)) < Number(initialWidth));
  const resizedWidth = await value(vertical);
  const grip = await horizontal.boundingBox();
  await page.mouse.move(grip.x + grip.width / 2, grip.y + grip.height / 2);
  await page.mouse.down();
  await page.mouse.move(grip.x + grip.width / 2, grip.y + 45, { steps: 5 });
  await page.mouse.up();
  assert.ok(Number(await value(horizontal)) > 50);
  assert.equal(await value(vertical), resizedWidth);
  if (process.env.CAGE_SPLIT_SCREENSHOT) await workspace.screenshot({ path: process.env.CAGE_SPLIT_SCREENSHOT });

  // Move the running left terminal above the bottom pane without reconnecting it.
  const beforeMove = state.terminalConnections;
  const handle = await secondTerminal.boundingBox();
  const dest = await third.boundingBox();
  await page.mouse.move(handle.x + handle.width / 2, handle.y + handle.height / 2);
  await page.mouse.down();
  await page.mouse.move(dest.x + dest.width / 2, dest.y + 52, { steps: 10 });
  await workspace.locator(".split-drop.split-top").waitFor();
  await page.mouse.up();
  await page.waitForFunction(() => document.querySelectorAll('.terminal-divider-y').length === 2);
  assert.equal(state.terminalConnections, beforeMove);
  await newPane.getByRole("button", { name: "关闭当前标签", exact: true }).click();
  await page.getByRole("dialog").getByRole("button", { name: "关闭终端", exact: true }).click();
  await newPane.waitFor({ state: "hidden" });
  await page.waitForFunction(() => document.querySelectorAll('[data-terminal-pane]:not([hidden])').length === 2);
  assert.equal(await third.getAttribute("data-focused"), "true");
  await third.getByRole("button", { name: "退出分屏" }).click();
  await horizontal.waitFor({ state: "hidden" });
  assert.equal(await workspace.locator("[data-terminal-pane]:visible").count(), 1);
  assert.equal(state.terminals.length, 2);
  assert.deepEqual(errors, []);
  console.log("Passed: cancel, left/right drops, pane focus, native split, independent headers and navigation, nested terminal splits, pointer/keyboard resize, move, close, exit and terminal continuity");
} finally {
  await browser.close();
}
