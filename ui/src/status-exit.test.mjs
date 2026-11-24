import assert from "node:assert/strict";
import test from "node:test";
import { statusExitText } from "./status-exit.mjs";

const tr = text => `[${text}]`;
const status = {
  safety: { exit_ip: "203.0.113.7" },
  location: { ip: "198.51.100.2", country: "日本", city: "东京" },
};

test("statusExitText shows the exit address and place", () => {
  assert.equal(statusExitText(status, false, tr), "203.0.113.7 · 日本 · 东京");
  assert.equal(statusExitText({ location: status.location }, false, tr), "198.51.100.2 · 日本 · 东京");
  assert.equal(statusExitText({ safety: status.safety }, false, tr), "203.0.113.7");
});

test("statusExitText leaves out address and place while hidden (#804)", () => {
  for (const value of [status, { location: status.location }, { safety: status.safety }]) {
    const text = statusExitText(value, true, tr);
    assert.equal(text, "[IP 已隐藏]");
    for (const secret of ["203.0.113.7", "198.51.100.2", "日本", "东京"]) {
      assert.ok(!text.includes(secret), `${secret} leaked into ${text}`);
    }
  }
});

test("statusExitText keeps states that reveal nothing", () => {
  for (const hidden of [false, true]) {
    assert.equal(statusExitText({ ...status, network_error: "timeout" }, hidden, tr), "[出口待确认]");
    assert.equal(statusExitText({}, hidden, tr), "[指定代理出口]");
    assert.equal(statusExitText(null, hidden, tr), "[指定代理出口]");
  }
});
