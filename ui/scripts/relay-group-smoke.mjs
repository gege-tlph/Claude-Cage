// Relay auto-switch groups in our chain editor (#1063, user design 2026-11-28).
// The 前置 row switches to an auto-switch group, edits its range and
// strategy, picks nodes in the list, and the apply sends hops; a saved group
// reads back with its node in use.
import assert from "node:assert/strict";
import fs from "node:fs/promises";
import path from "node:path";
import { tmpdir } from "node:os";
import { createWorkbench } from "./workbench-fixture.mjs";


const out = process.env.CAGE_RELAY_QA_OUTPUT || path.join(tmpdir(), "cage-relay-group");
await fs.mkdir(out, { recursive: true });
const proxy = {
  sources: [
    { id: "air", name: "旅行订阅", nodes: [{ id: "la", name: "USA Los Angeles 02", type: "anytls" }, { id: "tokyo", name: "Tokyo Transit", type: "trojan" }, { id: "hk", name: "HK 01", type: "vless" }] },
    { id: "home", name: "住宅代理", nodes: [{ id: "exit", name: "住宅 IP · 节点001", type: "vless" }] },
  ],
  selected: "exit", route: ["la", "exit"], route_hops: [{ node_id: "la" }, { node_id: "exit" }],
  active: true, running: true, core_available: true, port: 17896, state_label: "已就绪", route_error: "",
};
const f = await createWorkbench({ viewport: { width: 1280, height: 1000 }, clientContext: { mode: "local", connection_id: "local", client_platform: "windows", server_platform: "windows", connection: { state: "disconnected" } } });
const { page, browser, state } = f;
const posts = [];
try {
  await page.route("**/api/proxy", (r) => r.fulfill({ json: proxy }));
  await page.route("**/api/proxy/route", (r) => {
    posts.push(r.request().postDataJSON());
    state.status.job = { id: "route-1", kind: "proxy", running: false, error: "", message: "节点已应用" };
    return r.fulfill({ json: { id: "route-1" } });
  });
  await page.reload();
  await page.getByRole("button", { name: "设置", exact: true }).click();
  await page.getByRole("navigation", { name: "设置分组" }).getByRole("button", { name: "网络与代理", exact: true }).click();
  const editor = page.locator(".proxy-route-editor");
  await editor.waitFor();
  const kind = editor.getByRole("group", { name: "前置方式" });
  await kind.getByRole("button", { name: "自动", exact: true }).click();
  await editor.getByText("自动 · 故障转移 · 旅行订阅").waitFor();
  await editor.getByText("自动（旅行订阅）").waitFor();
  await editor.getByRole("group", { name: "策略" }).getByRole("button", { name: "最低延迟" }).click();
  await editor.getByText("自动 · 最低延迟 · 旅行订阅").waitFor();
  await page.screenshot({ path: path.join(out, "group-source.png") });

  // 自选节点: the list ticks nodes into the group.
  await editor.getByRole("radio", { name: "自选节点" }).check();
  await page.getByRole("button", { name: "中转组节点 Tokyo Transit" }).click();
  await page.getByRole("button", { name: "中转组节点 HK 01" }).click();
  await editor.getByText("已选 2 个").waitFor();
  assert.equal(await page.getByRole("button", { name: "中转组节点 住宅 IP · 节点001" }).isDisabled(), true, "the fixed exit cannot join the group");
  // 故障转移 tries the chosen nodes in their listed order, which the arrows change.
  await editor.getByRole("group", { name: "策略" }).getByRole("button", { name: "故障转移" }).click();
  const pool = editor.getByRole("list", { name: "自选节点" });
  await pool.getByRole("button", { name: "下移" }).first().click();
  assert.deepEqual(await pool.locator(".proxy-group-node-name").allTextContents(), ["HK 01", "Tokyo Transit"]);
  await page.screenshot({ path: path.join(out, "group-nodes.png") });

  // Apply from the settings submit bar.
  const apply = page.getByRole("button", { name: "保存", exact: true }).last();
  await apply.click();
  await page.waitForTimeout(800);
  assert.deepEqual(posts.at(-1)?.hops, [{ relay: { strategy: "fallback", node_ids: ["hk", "tokyo"] } }, { node_id: "exit" }], JSON.stringify(posts));

  // Saved: the group reads back with its node in use; 固定 turns it back.
  // The backend names the group by its core group in route.
  proxy.route = ["CAGE-RELAY-0", "exit"];
  proxy.route_hops = [{ relay: { strategy: "url-test", node_ids: ["tokyo", "hk"] }, candidate_count: 2, active_node_id: "tokyo" }, { node_id: "exit" }];
  await page.reload();
  await page.getByRole("button", { name: "设置", exact: true }).click();
  await page.getByRole("navigation", { name: "设置分组" }).getByRole("button", { name: "网络与代理", exact: true }).click();
  await editor.getByText("自动 · 最低延迟 · 自选 2 个").waitFor();
  await editor.getByText("当前 Tokyo Transit").waitFor();
  assert.equal(await page.getByText("待应用：", { exact: false }).count(), 0, "a saved group is not a pending change");
  assert.equal(await page.getByRole("button", { name: "重新选择前置" }).count(), 0, "an auto 前置 has no node to pick again");
  await page.screenshot({ path: path.join(out, "group-saved.png") });

  // A second auto 前置 over the same nodes shows why and cannot be saved.
  await editor.getByRole("button", { name: "添加前置" }).click();
  await editor.getByRole("group", { name: "前置方式" }).nth(1).getByRole("button", { name: "自动", exact: true }).click();
  await editor.getByText("和其他自动前置重复").nth(1).waitFor();
  assert.equal(await editor.getByText("和其他自动前置重复").count(), 2, "both groups say they overlap");
  assert.equal(await page.getByRole("button", { name: "保存", exact: true }).last().isDisabled(), true, "an overlapping group can be saved");
  await page.screenshot({ path: path.join(out, "group-overlap.png") });
  await editor.getByRole("button", { name: "移除前置" }).nth(1).click();

  await editor.getByRole("group", { name: "前置方式" }).getByRole("button", { name: "固定", exact: true }).click();
  await editor.getByText("在下面选择节点").waitFor();
  assert.deepEqual(f.errors, []);
  console.log("relay group ok");
} finally {
  await browser.close();
}
