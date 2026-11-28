package hostbridge

import "testing"

// UTF-8 output cut inside a character at either edge stays UTF-8.
func TestReadableOutputKeepsCutUTF8(t *testing.T) {
	text := []byte("中文 output 输出")
	for _, b := range [][]byte{text, text[1:], text[:len(text)-1], text[2 : len(text)-2]} {
		if got := readableOutput(b); got != string(b) {
			t.Errorf("%q read as %q", b, got)
		}
	}
	if readableOutput(nil) != "" {
		t.Error("empty output")
	}
}
