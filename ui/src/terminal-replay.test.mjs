import assert from "node:assert/strict";
import test from "node:test";
import { nextLiveControl, pendingControl } from "./terminal-replay.mjs";

const bytes = (text) => new TextEncoder().encode(text);

test("live bytes belong to the replay's control up to the next introducer", () => {
  // "\x1b[6" | "n\r\nLive\x1b[c": the n and the text up to the new ESC.
  assert.equal(nextLiveControl(bytes("n\r\nLive\x1b[c")), "n\r\nLive\x1b".length);
  assert.equal(nextLiveControl(bytes("no control here")), 0);
  // A UTF-8 C1 introducer (0xc2 0x9b is CSI) ends it too, also across frames.
  assert.equal(nextLiveControl(Uint8Array.of(0x6e, 0xc2, 0x9b, 0x63)), 3);
  assert.equal(nextLiveControl(Uint8Array.of(0x9b, 0x63), true), 1);
  assert.equal(nextLiveControl(Uint8Array.of(0x9b, 0x63), false), 0);
});

test("pendingControl reads xterm's parser, and none without it", () => {
  const terminal = (state, interim = 0) => ({ _core: { _inputHandler: { _parser: { currentState: state, initialState: 0 }, _utf8Decoder: { interim: [interim] } } } });
  assert.equal(pendingControl(terminal(0)), false);
  assert.equal(pendingControl(terminal(3)), true);
  assert.equal(pendingControl(terminal(0, 0xc2)), true);
  assert.equal(pendingControl({}), false);
});
