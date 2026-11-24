import assert from "node:assert/strict";
import test from "node:test";
import { isSourceFormDirty } from "./proxy-source-form.mjs";

const emptyForm = { name: "", url: "", content: "", interval_hours: 24, host_download: false };
const emptyManual = { name: "", type: "socks5", server: "", port: "1080", username: "", password: "", sshAuth: "password", privateKey: "", passphrase: "", hostKey: "" };
const source = { id: "s1", name: "机场", interval_hours: 12, nodes: [{ id: "n1", server: "203.0.113.1", port: 443 }] };

test("an untouched add or edit form does not block setup", () => {
  assert.equal(isSourceFormDirty(null, "url", emptyManual), false);
  for (const mode of ["url", "file", "manual"]) {
    assert.equal(isSourceFormDirty({ ...emptyForm }, mode, emptyManual), false, mode);
  }
  const edit = { ...emptyForm, id: "s1", name: "机场", interval_hours: 12 };
  assert.equal(isSourceFormDirty(edit, "url", emptyManual, [source], { id: "n1", server: "203.0.113.1", port: "443" }), false);
});

test("typed values, a changed edit or an unknown source block setup", () => {
  assert.equal(isSourceFormDirty({ ...emptyForm, url: "https://example.com/sub" }, "url", emptyManual), true);
  assert.equal(isSourceFormDirty({ ...emptyForm, interval_hours: 6 }, "url", emptyManual), true);
  assert.equal(isSourceFormDirty({ ...emptyForm, host_download: true }, "url", emptyManual), true);
  assert.equal(isSourceFormDirty({ ...emptyForm, content: "proxies: []" }, "file", emptyManual), true);
  assert.equal(isSourceFormDirty({ ...emptyForm }, "manual", { ...emptyManual, server: "198.51.100.2" }), true);
  assert.equal(isSourceFormDirty({ ...emptyForm }, "manual", { ...emptyManual, type: "ssh", port: "22" }), true);
  assert.equal(isSourceFormDirty({ ...emptyForm, name: "新来源" }, "url", emptyManual), true);
  const edit = { ...emptyForm, id: "s1", name: "机场", interval_hours: 12 };
  assert.equal(isSourceFormDirty({ ...edit, name: "改名" }, "url", emptyManual, [source]), true);
  assert.equal(isSourceFormDirty({ ...edit, content: "proxies: []" }, "url", emptyManual, [source]), true);
  assert.equal(isSourceFormDirty(edit, "url", emptyManual, [source], { id: "n1", server: "203.0.113.9", port: "443" }), true);
  assert.equal(isSourceFormDirty(edit, "url", emptyManual, []), true);
});
