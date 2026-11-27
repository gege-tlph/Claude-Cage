package diagnostics

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLimaLogsKeepNewestLines(t *testing.T) {
	dir := t.TempDir()
	var long strings.Builder
	for long.Len() <= LimaLogMaxBytes+4096 {
		long.WriteString("old line of the hostagent stderr log\n")
	}
	long.WriteString("newest line\n")
	if err := os.WriteFile(filepath.Join(dir, "ha.stderr.log"), []byte(long.String()), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ha.stdout.log"), []byte("{\"status\":{}}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "serial.log"), 0700); err != nil {
		t.Fatal(err)
	}
	logs := LimaLogs("environment", dir)
	got := map[string][]byte{}
	for _, log := range logs {
		got[log.Name] = log.Data
	}
	if len(got) != 2 || got["lima/environment/ha.stdout.log"] == nil {
		t.Fatalf("logs %v", names(logs))
	}
	stderr := got["lima/environment/ha.stderr.log"]
	if len(stderr) > LimaLogMaxBytes || !bytes.HasSuffix(stderr, []byte("newest line\n")) || !bytes.HasPrefix(stderr, []byte("old line")) {
		t.Fatalf("stderr tail: %d bytes, starts %q", len(stderr), stderr[:min(len(stderr), 20)])
	}
	if LimaLogs("environment", filepath.Join(dir, "missing")) != nil {
		t.Fatal("logs from a missing instance")
	}
}

func TestSupplementRedactionKeepsSharedAddresses(t *testing.T) {
	in := "\x00" + `{"guestAddr":"127.0.0.1:15900","hostAddr":"127.0.0.1:5573"}
{"guestAddr":"[::1]:15900","hostAddr":"127.0.0.1:15900"} {"guestAddr":"0.0.0.0:22"} [::]:22
dial 203.0.113.7:443 via proxy.example.com:8080 token=s3cret-value C:\Users\alice\.claude-cage\state alice@example.com
`
	out := redactSupplement(in)
	for _, kept := range []string{"127.0.0.1:15900", "127.0.0.1:5573", "[::1]:15900", "0.0.0.0:22", "[::]:22"} {
		if !strings.Contains(out, kept) {
			t.Errorf("%s erased: %s", kept, out)
		}
	}
	for _, secret := range []string{"203.0.113.7", "proxy.example.com", "s3cret-value", "alice", "\x00"} {
		if strings.Contains(out, secret) {
			t.Errorf("%q kept: %s", secret, out)
		}
	}
}

func TestExportAddsRedactedSupplements(t *testing.T) {
	s := New(t.TempDir())
	var bundle bytes.Buffer
	extra := []Supplement{{Name: "lima/environment/ha.stderr.log", Data: []byte("forward 127.0.0.1:15900 password=lima-secret\n")}}
	if err := s.Export(&bundle, "test", nil, "offline", extra...); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(bundle.Bytes()), int64(bundle.Len()))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range archive.File {
		if f.Name != extra[0].Name {
			continue
		}
		r, _ := f.Open()
		b, _ := io.ReadAll(r)
		r.Close()
		if !strings.Contains(string(b), "127.0.0.1:15900") || strings.Contains(string(b), "lima-secret") {
			t.Fatalf("supplement %q", b)
		}
		return
	}
	t.Fatal("supplement missing from the bundle")
}

func names(logs []Supplement) []string {
	var out []string
	for _, log := range logs {
		out = append(out, log.Name)
	}
	return out
}
