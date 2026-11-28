// A terminal's replayed history can end inside a control sequence: a cursor
// position query cut as "\x1b[6" | "n". The live output then finishes it, and
// xterm would answer a query of the past screen into the shell's input. As
// upstream does (terminal-stream.mjs, #1090), the live bytes up to the next
// control introducer are parsed under the replay guard.

// nextLiveControl is how many of the live bytes still belong to the control
// the replay started: up to and including the next ESC or UTF-8 C1
// introducer, which ends it; 0 when bytes hold none, so all of them belong.
// liveC1Lead: the previous live bytes ended in 0xc2, a C1 lead.
export function nextLiveControl(bytes, liveC1Lead = false) {
  let previous = liveC1Lead ? 0xc2 : 0;
  for (let i = 0; i < bytes.length; i++) {
    const value = bytes[i];
    if (value === 0x1b || (previous === 0xc2 && value >= 0x80 && value < 0xa0)) return i + 1;
    previous = value;
  }
  return 0;
}

// pendingControl reports whether xterm's parser is inside a control sequence
// or after a UTF-8 C1 lead. xterm 6 has no public parser position, so this
// reads its internals; without them it reports none.
export function pendingControl(terminal) {
  try {
    const input = terminal._core._inputHandler;
    return input._parser.currentState !== input._parser.initialState || input._utf8Decoder.interim[0] === 0xc2;
  } catch {
    return false;
  }
}
