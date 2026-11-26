import assert from "node:assert/strict";
import fs from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { createWorkbench } from "./workbench-fixture.mjs";

// About shows the App's own updater (the sidebar's update button): no
// comparison with the main branch, no second request to GitHub of its own.
const output = new URL("../../output/about-settings/", import.meta.url);
await fs.mkdir(output, { recursive: true });
const epoch = new Date("2026-11-26T12:00:00Z");
for (const platform of ["darwin", "windows"]) {
  const build = { version: "0.2.3", commit: "a".repeat(40), modified: true };
  const context = { mode: "local", connection_id: "local", client_platform: platform, server_platform: platform, connection: { state: "disconnected" } };
  const calls = [];
  let controllerChecks = 0;
  let update = { phase: "current", currentVersion: "0.2.3", retryAt: "" };
  let next = update;
  const fixture = await createWorkbench({
    clientContext: context,
    beforeNavigate: page => page.clock.install({ time: epoch }),
    clientRequest: async (route, path) => {
      if (path === "/client/context") return route.fulfill({ json: context });
      if (path !== "/client/about") return route.fulfill({ json: {} });
      if (route.request().method() !== "GET") controllerChecks++;
      return route.fulfill({ json: build });
    },
    nativeRequest: async body => {
      if (body.action === "appReference") { calls.push("reference:" + body.kind); return true; }
      if (body.action !== "appUpdate") return null;
      calls.push(body.operation);
      if (body.operation === "check") update = next;
      return update;
    },
  });
  const { page, browser, errors } = fixture;
  try {
    await page.getByRole("button", { name: "设置", exact: true }).click();
    await page.getByRole("navigation", { name: "设置分组" }).getByRole("button", { name: "关于", exact: true }).click();
    const about = page.getByRole("region", { name: "关于 Cage lite", exact: true });
    await about.getByText("0.2.3", { exact: true }).first().waitFor();
    await about.getByText("aaaaaaaaaaaa", { exact: true }).waitFor();
    assert.ok(await about.getByText(/包含未提交的修改/).isVisible());
    await about.getByText("已是最新版本", { exact: true }).waitFor();
    assert.equal(await about.getByText(/源码|主分支/).count(), 0, "the main-branch comparison is gone");

    next = { phase: "available", currentVersion: "0.2.3", latestVersion: "0.2.4", retryAt: "" };
    await about.getByRole("button", { name: "检测更新", exact: true }).click();
    await about.getByText("发现新发行版", { exact: true }).waitFor();
    await about.getByText("0.2.4 · 正式版", { exact: true }).waitFor();
    await about.getByRole("button", { name: "下载更新", exact: true }).click();
    assert.ok(calls.includes("install"), "About's button runs the App's own update");
    await about.getByRole("link", { name: "查看发行说明与下载" }).click();
    assert.ok(calls.includes("reference:releases"));
    await page.screenshot({ path: fileURLToPath(new URL(`${platform}-available.png`, output)), fullPage: true });

    next = { phase: "failed", currentVersion: "0.2.3", message: "更新源请求受限，请稍后重试", retryable: true, retryAt: new Date(+epoch + 30 * 60 * 1000).toISOString() };
    update = { phase: "current", currentVersion: "0.2.3", retryAt: "" };
    await page.evaluate(() => window.dispatchEvent(new CustomEvent("cage-app-update", { detail: { phase: "current", currentVersion: "0.2.3", retryAt: "" } })));
    await about.getByText("已是最新版本", { exact: true }).waitFor();
    await about.getByRole("button", { name: "检测更新", exact: true }).click();
    await about.getByText("更新源请求受限，请稍后重试", { exact: true }).waitFor();
    await about.getByRole("button", { name: "等待重试", exact: true }).waitFor();
    assert.ok(await about.getByRole("button", { name: "等待重试", exact: true }).isDisabled());

    assert.equal(controllerChecks, 0, "About in the App must not check through the controller as well");
    assert.equal(await page.evaluate(() => document.querySelector(".settings-content").scrollWidth > document.querySelector(".settings-content").clientWidth), false);
    assert.deepEqual(errors, []);
    console.log(`PASS: ${platform} About shows the App's updater: current, available with its button, release notes, cooldown; no source comparison`);
  } finally { await browser.close(); }
}
