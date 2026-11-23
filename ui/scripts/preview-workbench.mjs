// Opens the workbench in a visible browser window against the mock backend,
// for looking at the interface without a VM. The window follows its own size.
//
//   CAGE_PLAYWRIGHT_MODULE=... CAGE_TEST_BROWSER_PATH=... node scripts/preview-workbench.mjs
//
// PREVIEW_URL (default http://127.0.0.1:5198, a running `npm run dev`),
// PREVIEW_LANG (default zh-CN). The interface language can be switched in
// Settings; the mock backend remembers it until the window closes.
import { createWorkbench } from "./workbench-fixture.mjs";

const url = process.env.PREVIEW_URL || "http://127.0.0.1:5198";
let language = process.env.PREVIEW_LANG || "zh-CN";
// PREVIEW_STARTUP_DELAY (ms) holds the first workspace status after each page
// load, so the startup splash stays visible; reload the page to replay it.
const startupDelay = Number(process.env.PREVIEW_STARTUP_DELAY || 0);
let holdNextStatus = startupDelay > 0;

const { browser, page } = await createWorkbench({
  url,
  viewport: null,
  launchOptions: { headless: false, args: ["--window-size=1400,900"] },
  clientContext: { mode: "local", connection_id: "local", client_platform: "windows", server_platform: "windows", connection: { state: "disconnected" } },
  beforeNavigate: async (page, state) => {
    Object.assign(state.status, { ready: true, vm_running: true });
    await page.route("**/api/client/language", async (route) => {
      const request = route.request();
      if (request.method() === "POST") {
        const next = request.postDataJSON()?.language;
        if (next) language = next === "system" ? "zh-CN" : next;
      }
      await route.fulfill({ json: { language, active: language, system: "zh-CN" } });
    });
    if (startupDelay > 0) {
      page.on("framenavigated", (frame) => { if (frame === page.mainFrame()) holdNextStatus = true; });
      await page.route("**/api/status", async (route) => {
        if (holdNextStatus) {
          holdNextStatus = false;
          await new Promise((resolve) => setTimeout(resolve, startupDelay));
        }
        await route.fallback();
      });
    }
    await page.route("**/api/vault", (route) => route.fulfill({ json: { entries: [
      { id: "1", origin: "https://mail.example.com", username: "lin@example.com" },
    ], blocked: [] } }));
  },
});
console.log(`Preview open at ${url} (mock backend). Close the window to stop.`);
await new Promise((resolve) => {
  browser.on("disconnected", resolve);
  page.on("close", resolve);
});
await browser.close().catch(() => {});
