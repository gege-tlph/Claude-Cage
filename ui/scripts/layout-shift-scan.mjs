// Layout-shift scan: clicks through the workbench against the mock backend and
// records every element that moves after a click (Chromium Layout Instability API).
//
//   CAGE_PLAYWRIGHT_MODULE=... CAGE_TEST_BROWSER_PATH=... node scripts/layout-shift-scan.mjs
//
// SCAN_LANGS (default zh-CN,ru,ar), SCAN_WIDTHS (default 1280,960), SCAN_OUT (default .layout-scan)
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { createServer } from "vite";
import { createWorkbench } from "./workbench-fixture.mjs";

const root = fileURLToPath(new URL("..", import.meta.url));
const out = process.env.SCAN_OUT || join(root, ".layout-scan");
const langs = (process.env.SCAN_LANGS || "zh-CN,ru,ar").split(",");
const widths = (process.env.SCAN_WIDTHS || "1280,960").split(",").map(Number);
const settle = Number(process.env.SCAN_SETTLE || 1500);
mkdirSync(out, { recursive: true });

function observer() {
  window.__shifts = [];
  const describe = (node) => {
    const el = node?.nodeType === 1 ? node : node?.parentElement;
    if (!el) return "?";
    const path = [];
    for (let e = el, i = 0; e && e !== document.body && i < 4; e = e.parentElement, i++) {
      const cls = typeof e.className === "string" && e.className.trim() ? "." + e.className.trim().split(/\s+/).slice(0, 2).join(".") : "";
      path.unshift(e.tagName.toLowerCase() + cls);
    }
    const text = (el.innerText || el.textContent || "").trim().replace(/\s+/g, " ").slice(0, 40);
    return `${path.join(" > ")}${text ? ` "${text}"` : ""}`;
  };
  const rect = (r) => ({ x: Math.round(r.x), y: Math.round(r.y), w: Math.round(r.width), h: Math.round(r.height) });
  new PerformanceObserver((list) => {
    for (const e of list.getEntries()) {
      window.__shifts.push({
        t: Math.round(e.startTime),
        value: e.value,
        sources: e.sources.map((s) => ({ node: describe(s.node), prev: rect(s.previousRect), cur: rect(s.currentRect) })),
      });
    }
  }).observe({ type: "layout-shift", buffered: true });
}

const take = (page) => page.evaluate(() => { const s = window.__shifts || []; window.__shifts = []; return s; });

const vite = await createServer({ root, configFile: `${root}/vite.config.js`, server: { host: "127.0.0.1", port: 0 } });
await vite.listen();
const url = `http://127.0.0.1:${vite.httpServer.address().port}`;
const report = [];

async function step(page, tag, label, click) {
  await take(page);
  const before = Date.now();
  try {
    await click();
  } catch (error) {
    report.push({ tag, label, error: String(error.message).split("\n")[0] });
    return;
  }
  await page.waitForTimeout(settle);
  const shifts = await take(page);
  const score = shifts.reduce((a, s) => a + s.value, 0);
  const file = `${tag}--${String(report.length).padStart(3, "0")}.png`;
  if (score > 0) await page.screenshot({ path: join(out, file) });
  report.push({ tag, label, score: +score.toFixed(4), ms: Date.now() - before, shifts, screenshot: score > 0 ? file : undefined });
}

try {
  for (const lang of langs) {
    for (const width of widths) {
      const tag = `${lang}-${width}`;
      const { browser, page, errors } = await createWorkbench({
        url,
        viewport: { width, height: 820 },
        clientContext: { mode: "local", connection_id: "local", client_platform: "windows", server_platform: "windows", connection: { state: "disconnected" } },
        beforeNavigate: async (page, state) => {
          Object.assign(state.status, { ready: true, vm_running: true });
          await page.addInitScript(observer);
          await page.route("**/api/client/language", (route) => route.fulfill({ json: { language: lang, active: lang, system: lang } }));
          // The shared fixture has no password vault.
          await page.route("**/api/vault", (route) => route.fulfill({ json: { entries: [
            { id: "1", origin: "https://mail.example.com", username: "lin@example.com" },
            { id: "2", origin: "https://accounts.example.org", username: "design-lab-team" },
          ], blocked: ["https://news.example.net"] } }));
        },
      });
      try {
        await page.waitForTimeout(2500);
        await take(page);
        const nav = page.locator(".workspace-sidebar .sidebar-navigation > button");
        const navCount = await nav.count();
        const navLabels = [];
        for (let i = 0; i < navCount; i++) navLabels.push((await nav.nth(i).getAttribute("aria-label", { timeout: 3000 })) || (await nav.nth(i).innerText()));
        // Each sidebar entry starts from a freshly loaded home page, so one
        // entry's panel cannot hide the next entry.
        for (let i = 0; i < navCount; i++) {
          await page.reload();
          await page.waitForTimeout(2500);
          await step(page, tag, `sidebar: ${navLabels[i]}`, () => nav.nth(i).click({ timeout: 3000 }));
          // A second visit, after the first one loaded its data.
          await page.keyboard.press("Escape").catch(() => {});
          await page.locator(".workspace-sidebar .sidebar-brand, .workspace-sidebar [aria-label='工作台']").first().click({ timeout: 1500 }).catch(() => {});
          await page.waitForTimeout(800);
          await step(page, tag, `sidebar (again): ${navLabels[i]}`, () => nav.nth(i).click({ timeout: 3000 }));
        }
        await page.reload();
        await page.waitForTimeout(2500);
        const settings = page.locator(".workspace-sidebar button.icon-button:has(svg.lucide-settings)").first();
        await step(page, tag, "open settings", () => settings.click({ timeout: 3000 }));
        const tabs = page.locator(".settings-sidebar nav button");
        const tabLabels = (await tabs.allInnerTexts()).map((text) => text.trim().replace(/\s+/g, " "));
        const tab = async (i) => {
          // A section that crashed leaves the error boundary; start over.
          if (await page.getByText("React will try", { exact: false }).count() || !(await settings.isVisible())) {
            await page.reload();
            await page.waitForTimeout(2500);
          }
          // Some sections replace the settings sidebar; reopen settings first.
          if (!(await tabs.count())) {
            await settings.click({ timeout: 3000 });
            await page.waitForTimeout(800);
            await take(page);
          }
          await tabs.nth(i).click({ timeout: 3000 });
        };
        for (let i = 0; i < tabLabels.length; i++) await step(page, tag, `settings: ${tabLabels[i]}`, () => tab(i));
        // Revisit each settings tab a second time: data is cached, so any
        // remaining movement is layout, not loading.
        for (let i = 0; i < tabLabels.length; i++) await step(page, tag, `settings (again): ${tabLabels[i]}`, () => tab(i));
        if (errors.length) report.push({ tag, label: "page errors", errors });
      } finally {
        await browser.close();
      }
      console.log(`done ${tag}`);
    }
  }
} finally {
  await vite.close();
  writeFileSync(join(out, "report.json"), JSON.stringify(report, null, 2));
  console.log(`report: ${join(out, "report.json")}`);
}
