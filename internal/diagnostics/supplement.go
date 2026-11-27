package diagnostics

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// LimaLogMaxBytes bounds each Lima log in a bundle; its newest part is kept.
const LimaLogMaxBytes = 1 << 20

// Supplement is one more file of a bundle, under lima/. Export redacts it.
type Supplement struct {
	Name string
	Data []byte
}

// limaLogNames are an instance's own logs: the hostagent's events (its port
// forwards among them) and messages, and the guest console of the last boot.
var limaLogNames = []string{"ha.stdout.log", "ha.stderr.log", "serial.log"}

// LimaLogs reads the newest part of each Lima log of the instance in dir and
// files it under lima/<role>/. Only regular files are read; a missing
// instance gives nothing.
func LimaLogs(role, dir string) []Supplement {
	var out []Supplement
	for _, name := range limaLogNames {
		if b := tail(filepath.Join(dir, name), LimaLogMaxBytes); len(b) > 0 {
			out = append(out, Supplement{Name: "lima/" + role + "/" + name, Data: b})
		}
	}
	return out
}

// tail returns at most max bytes from the end of path, from a line start.
func tail(path string, max int64) []byte {
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	start := st.Size() - max
	if start < 0 {
		start = 0
	}
	if _, err = f.Seek(start, io.SeekStart); err != nil {
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(f, max))
	if start > 0 {
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			b = b[i+1:]
		}
	}
	return b
}

// sharedAddress matches addresses that are the same on every machine:
// loopback and unspecified. They tell which host port a forward uses, which
// Redact would erase together with the address.
var sharedAddress = regexp.MustCompile(`\b127(?:\.[0-9]{1,3}){3}\b|\b0\.0\.0\.0\b|\[::1?\]`)

var keptMark = regexp.MustCompile("\x00([0-9]+)\x00")

// redactSupplement is Redact, keeping loopback and unspecified addresses.
func redactSupplement(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	var kept []string
	s = sharedAddress.ReplaceAllStringFunc(s, func(address string) string {
		kept = append(kept, address)
		return "\x00" + strconv.Itoa(len(kept)-1) + "\x00"
	})
	return keptMark.ReplaceAllStringFunc(Redact(s), func(mark string) string {
		if i, err := strconv.Atoi(mark[1 : len(mark)-1]); err == nil && i < len(kept) {
			return kept[i]
		}
		return ""
	})
}
