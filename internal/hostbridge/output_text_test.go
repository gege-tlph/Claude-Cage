package hostbridge

import (
	"strings"
	"testing"
	"unicode/utf8"
)

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

// Pages of a background job's output end on character boundaries, so reading
// them one after another gives back the whole text (A13).
func TestJobPagesKeepCharactersWhole(t *testing.T) {
	text := strings.Repeat("构建输出 ok ", 40)
	buf := &jobBuffer{summary: cappedBuffer{limit: 64}, ringMax: 1 << 10}
	for _, r := range text {
		_, _ = buf.Write([]byte(string(r)))
	}
	var got strings.Builder
	for offset := 0; offset < len(text); {
		chunk, next, skipped, _ := buf.readFrom(offset, 7)
		if skipped != 0 || next <= offset || !utf8.ValidString(chunk) {
			t.Fatalf("page at %d: %q next=%d skipped=%d", offset, chunk, next, skipped)
		}
		got.WriteString(chunk)
		offset = next
	}
	if got.String() != text {
		t.Fatalf("pages joined to %q", got.String())
	}
	// After eviction the first page starts at the next whole character.
	small := &jobBuffer{summary: cappedBuffer{limit: 64}, ringMax: 10}
	_, _ = small.Write([]byte("中文中文中文"))
	chunk, next, skipped, _ := small.readFrom(0, 100)
	if chunk != "文中文" || next != 18 || skipped != 9 {
		t.Fatalf("after eviction: %q next=%d skipped=%d", chunk, next, skipped)
	}
}

// tailBytes keeps exactly the last bytes however the writes are split.
func TestTailBytesKeepsLastBytes(t *testing.T) {
	var tail tailBytes
	var all []byte
	for i := 0; i < 1000; i++ {
		p := []byte(strings.Repeat(string(rune('a'+i%26)), i%7+1))
		dropped := tail.write(p, 50)
		all = append(all, p...)
		if dropped != (len(all) > 50) {
			t.Fatalf("write %d: dropped=%v with %d bytes", i, dropped, len(all))
		}
		want := all[max(0, len(all)-50):]
		if string(tail.bytes(50)) != string(want) {
			t.Fatalf("write %d: kept %q, want %q", i, tail.bytes(50), want)
		}
		if len(tail.buf) > 100 {
			t.Fatalf("write %d: holds %d bytes", i, len(tail.buf))
		}
	}
	_ = tail.write([]byte(strings.Repeat("z", 80)), 50)
	if string(tail.bytes(50)) != strings.Repeat("z", 50) {
		t.Fatalf("large write kept %q", tail.bytes(50))
	}
}
