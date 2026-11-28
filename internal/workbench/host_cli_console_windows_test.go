//go:build windows

package workbench

import (
	"io"
	"testing"
)

// Ctrl+Z in the raw console goes on to the terminal instead of ending the
// input (B10); other reads and errors pass through.
func TestRawConsoleCtrlZIsInput(t *testing.T) {
	buf := make([]byte, 8)
	n, err := consoleCtrlZ(buf, func([]byte) (int, error) { return 0, io.EOF })
	if n != 1 || err != nil || buf[0] != 0x1A {
		t.Fatalf("Ctrl+Z read as n=%d err=%v %q", n, err, buf[:n])
	}
	n, err = consoleCtrlZ(buf, func(b []byte) (int, error) { return copy(b, "ab"), nil })
	if n != 2 || err != nil || string(buf[:n]) != "ab" {
		t.Fatalf("plain read: n=%d err=%v %q", n, err, buf[:n])
	}
	if _, err = consoleCtrlZ(buf, func([]byte) (int, error) { return 0, io.ErrClosedPipe }); err != io.ErrClosedPipe {
		t.Fatalf("error lost: %v", err)
	}
}
