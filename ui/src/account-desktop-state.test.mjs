import assert from "node:assert/strict";
import test from "node:test";
import {
  addDesktopBeside,
  desktopClipboardState,
  desktopMergeNoticeTarget,
  desktopLabel,
  desktopTitle,
  mergedDefaultInstance,
  reconcileAccountDesktopPanes,
} from "./account-desktop-state.mjs";
import { setMessages } from "./message-catalog.mjs";

const instanceA = { id: "a".repeat(24), number: 1, label: "工作账号", running: true };
const instanceB = { id: "b".repeat(24), number: 2, label: "备用账号", running: true };
const defaultDesktop = { id: "default", label: "默认 Desktop", running: true };

test("default and independent Desktop split left/right from either entry without duplicating an instance", () => {
  assert.deepEqual(addDesktopBeside([], instanceA, defaultDesktop), [defaultDesktop, instanceA]);
  assert.deepEqual(addDesktopBeside([instanceA], defaultDesktop, defaultDesktop), [defaultDesktop, instanceA]);
  assert.deepEqual(addDesktopBeside([defaultDesktop, instanceA], instanceA, defaultDesktop), [defaultDesktop, instanceA]);
  assert.deepEqual(addDesktopBeside([defaultDesktop, instanceA], instanceB, defaultDesktop), [defaultDesktop, instanceB]);
  assert.deepEqual(addDesktopBeside([], instanceA, null), [instanceA]);
});

test("catalog refresh keeps the mixed split, but merging accounts never shows the same guest twice", () => {
  const mixed = [defaultDesktop, instanceA];
  assert.deepEqual(reconcileAccountDesktopPanes(mixed, { instances: [defaultDesktop, instanceA] }), mixed);
  assert.deepEqual(reconcileAccountDesktopPanes(mixed, { instances: [instanceA] }), mixed);
  assert.deepEqual(reconcileAccountDesktopPanes(mixed, { instances: [instanceA], redirects: { default: instanceA.id } }), [instanceA]);
  assert.deepEqual(reconcileAccountDesktopPanes(mixed, { instances: [defaultDesktop] }), [defaultDesktop]);
});

test("isolated desktop clipboard stays connected across window occlusion and pauses polling only", () => {
  const active = desktopClipboardState({
    ready: true,
    focused: true,
    visible: true,
    windowVisible: true,
    native: true,
  });
  assert.deepEqual(active, { connected: true, pollingVisible: true });

  // Switching to another full-screen/occluding macOS window must not tear down the clipboard socket.
  const occluded = desktopClipboardState({
    ready: true,
    focused: true,
    visible: true,
    windowVisible: false,
    native: true,
  });
  assert.deepEqual(occluded, { connected: true, pollingVisible: false });

  // Opening settings pauses polling while keeping the slot warm.
  const settingsOpen = desktopClipboardState({
    ready: true,
    focused: true,
    visible: false,
    windowVisible: true,
    native: true,
  });
  assert.deepEqual(settingsOpen, { connected: true, pollingVisible: false });

  // Losing pane focus or stopping the VM disconnects cleanly.
  assert.deepEqual(
    desktopClipboardState({ ready: true, focused: false, visible: true, windowVisible: true, native: true }),
    { connected: false, pollingVisible: false },
  );
  assert.deepEqual(
    desktopClipboardState({ ready: false, focused: true, visible: true, windowVisible: true, native: true }),
    { connected: false, pollingVisible: false },
  );
});

test("reconcileAccountDesktopPanes switches off retired default Desktop and emits merge notice once", () => {
  const catalog = {
    instances: [instanceA, instanceB],
    redirects: { default: instanceA.id },
  };
  assert.equal(desktopTitle(instanceA), "#01 工作账号");
  assert.deepEqual(mergedDefaultInstance(catalog), instanceA);
  assert.deepEqual(reconcileAccountDesktopPanes([], catalog, true), [instanceA]);
  assert.deepEqual(reconcileAccountDesktopPanes([], catalog, false), []);

  // Redirecting an account pane back to default clears account panes.
  assert.deepEqual(
    reconcileAccountDesktopPanes([instanceB], { instances: [instanceA], redirects: { [instanceB.id]: "default" } }, false),
    [],
  );

  // Merge notice fires only when default redirect changes to a new target.
  assert.deepEqual(desktopMergeNoticeTarget("", catalog), instanceA);
  assert.equal(desktopMergeNoticeTarget(instanceA.id, catalog), null);
});

test("instances nobody named are shown in the interface language, named ones as given", t => {
  t.after(() => setMessages({}));
  setMessages({ "默认 Desktop": "Default Desktop", "待登录 Desktop": "Desktop (not signed in)", "工作账号": "Work" });
  assert.equal(desktopLabel(defaultDesktop), "Default Desktop");
  assert.equal(desktopLabel({ id: "c".repeat(24), label: "待登录 Desktop" }), "Desktop (not signed in)");
  assert.equal(desktopTitle({ ...instanceA, label: "待登录 Desktop" }), "#01 Desktop (not signed in)");
  // A name the user gave is never translated, even when the catalog has it.
  assert.equal(desktopLabel(instanceA), "工作账号");
  setMessages({});
  assert.equal(desktopLabel(defaultDesktop), "默认 Desktop");
});
