import test from "node:test";
import assert from "node:assert/strict";
import { pendingSummary } from "./settings-pending.mjs";

test("the submit bar names what is pending", () => {
  const current = { dns_url: "https://1.1.1.1/dns-query", timezone: "Asia/Tokyo", auto_timezone: false, keyboard: "mac" };
  assert.equal(pendingSummary({ ...current }, current, null), "");
  assert.equal(pendingSummary({ ...current, dns_url: "https://dns.example/dns-query" }, current, null), "加密 DNS");
  assert.equal(pendingSummary({ ...current, timezone: "Europe/Paris" }, current, { summary: "出口 日本 01" }), "出口 日本 01 · 时区 Europe/Paris");
  assert.equal(pendingSummary({ ...current, dns_url: "x", timezone: "Europe/Paris", keyboard: "windows", clock_12_hour: true }, current, { summary: "出口 日本 01" }),
    "出口 日本 01 · 加密 DNS · 时区 Europe/Paris · 等 2 项");
});
