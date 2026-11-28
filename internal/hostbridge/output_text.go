package hostbridge

import "unicode/utf8"

// readableOutput is command output as text: UTF-8 as it is, anything else
// read in the system's console code page (fromConsoleCodePage). A piece cut
// from a longer output may start or end inside a character; those edges do
// not make UTF-8 output count as another encoding.
func readableOutput(b []byte) string {
	if utf8.Valid(utf8Inner(b)) {
		return string(b)
	}
	return fromConsoleCodePage(b)
}

// utf8Inner is b without a character cut off at either edge.
func utf8Inner(b []byte) []byte {
	for i := 0; i < utf8.UTFMax-1 && len(b) > 0 && !utf8.RuneStart(b[0]); i++ {
		b = b[1:]
	}
	start := len(b) - 1
	for start > 0 && len(b)-start < utf8.UTFMax && !utf8.RuneStart(b[start]) {
		start--
	}
	if start >= 0 && !utf8.FullRune(b[start:]) {
		b = b[:start]
	}
	return b
}
