import assert from "node:assert/strict";
import test from "node:test";

const stored = new Map();
globalThis.localStorage = {
  getItem: (key) => (stored.has(key) ? stored.get(key) : null),
  setItem: (key, value) => stored.set(key, String(value)),
};
const { MASKED_NAME, accountName, privacyMasked, setPrivacyMasked } = await import("./privacy-mask.mjs");

test("accountName hides emails while masked and keeps names the user gave (#836)", () => {
  const email = { email: "lin@example.com", label: "lin@example.com", organization: "Example Co" };
  const named = { email: "lin@example.com", label: "工作号" };
  assert.equal(accountName(email, false), "lin@example.com");
  assert.equal(accountName(named, false), "工作号");
  assert.equal(accountName({ email: "lin@example.com" }, false), "lin@example.com");
  assert.equal(accountName(named, true), "工作号");
  for (const account of [email, { email: "lin@example.com" }, { label: "other@example.org" }, {}, null]) {
    const shown = accountName(account, true);
    assert.equal(shown, MASKED_NAME);
    assert.ok(!shown.includes("@"));
  }
});

test("the mask is saved and every subscriber reads the same value", () => {
  assert.equal(privacyMasked(), false);
  setPrivacyMasked(true);
  assert.equal(stored.get("cage-privacy-mask"), "true");
  assert.equal(privacyMasked(), true);
  setPrivacyMasked(false);
  assert.equal(privacyMasked(), false);
  stored.set("cage-privacy-mask", "not json");
  assert.equal(privacyMasked(), false);
});
