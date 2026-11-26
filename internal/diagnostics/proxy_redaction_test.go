package diagnostics

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const proxyFailure = `time="2026-11-14T10:20:30+08:00" level=warning msg="[TCP] dial CAGE (match Match/) 192.0.2.8:63839 --> api.github.com:443 error: static.example-provider.io:1337 connect error: dns resolve failed: context deadline exceeded"`

func TestRedactBacktickQuotedLimaSocketPaths(t *testing.T) {
	const prefix = "Error: instance name `base-building` too long: `"
	const suffix = "` must be less than UNIX_PATH_MAX=108 characters, but is 175"
	for _, path := range []string{
		`C:\Users\Test User\Cage\state\lima\base-building\ssh.sock.1234`,
		`C:/Users/Test User/Cage/state/lima/base-building/ssh.sock.1234`,
		`\\example\share\Test User\Cage\state\lima\base-building\ssh.sock.1234`,
		"/Users/alice/Cage Data/state/lima/base-building/ssh.sock.1234",
		"/home/alice/Cage Data/state/lima/base-building/ssh.sock.1234",
		"/data/alice's Cage:work/state/lima/base-building/ssh.sock.1234",
	} {
		got := Redact(prefix + path + suffix)
		if want := prefix + "[home path]" + suffix; got != want || Redact(got) != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}

const setupHint = "run 'cage agent call network.check --wait 5m' Locale=en_US.UTF-8"

func TestRedactProxyAddresses(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{setupHint, setupHint},
		{"cannot change locale (zh_CN.UTF-8)", "cannot change locale (zh_CN.UTF-8)"},
		{"call settings.probe then proxy.select", "call settings.probe then proxy.select"},
		{"writing lima.yaml and setup.sh", "writing lima.yaml and setup.sh"},
		{"panic at main.go:699", "panic at main.go:699"},
		{"dial setup.sh and main.go:699", "dial [host] and [host]"},
		{"cage agent call network.check.example:80 then proxy.select:443", "cage agent call [host] then [host]"},
		{"worker.network.check en_US.UTF-8:443 en_US.UTF-8.example", "[host] [host] [host]"},
		{"writing setup.sh.example at https://lima.yaml/secret", "writing [host] at [url]"},
		{"static.example-provider.io:1337 api.github.com", "[host] [host]"},
		{"STATIC.EXAMPLE-PROVIDER.IO:1337 xn--bcher-kva.example.", "[host] [host]"},
		{"edge.xn--p1ai:443 _gateway.example:1337", "[host] [host]"},
		{"192.0.2.8:63839", "[ip]"},
		{"[ip]:63839 --> static.example-provider.io:1337", "[ip] --> [host]"},
		{"[2001:db8::1]:443 [fe80::1%en0]:53", "[ip] [ip]"},
		{"2001:db8:1234:5678:90ab:cdef:1234:5678 2001:db8::1 ::1 ::", "[ip] [ip] [ip] [ip]"},
		{"[::ffff:192.0.2.8]:443 ::ffff:192.0.2.8", "[ip] [ip]"},
		{"proxy 0123456789abcdef01234567 uuid=12345678-1234-1234-1234-123456789abc", "proxy [redacted id] uuid=[redacted id]"},
		{"https://sub.example-provider.io/sub?token=private-subscription token=private-token", "[url] token=[redacted]"},
		{"Could not connect to 10.0.2.2:7890 (10.0.2.2). - connect (110: Connection timed out)", "Could not connect to [ip] ([ip]). - connect (110: Connection timed out)"},
		{"Err:1 https://deb.debian.org/debian trixie InRelease", "Err:1 [url] trixie InRelease"},
		{"E: Unable to locate package ripgrep", "E: Unable to locate package ripgrep"},
		// #791: the browser command that timed out stays readable.
		{"Login browser open failed: Browser command timed out: Page.navigate", "Login browser open failed: Browser command timed out: Page.navigate"},
		{"Target.createTarget failed near static.example-provider.io:1337", "Target.createTarget failed near [host]"},
		{"page.example-provider.io target.createtarget.example", "[host] [host]"},
		{"Login browser open failed (stage=create-target): Browser command timed out: Target/createTarget", "Login browser open failed (stage=create-target): Browser command timed out: Target/createTarget"},
		{"Login browser open failed (stage=navigate): Browser command timed out: Page/navigate at api.example.com", "Login browser open failed (stage=navigate): Browser command timed out: Page/navigate at [host]"},
		{"2026-11-14T10:20:30+08:00 v0.1.7 HTTP 502 dns resolve failed: context deadline exceeded", "2026-11-14T10:20:30+08:00 v0.1.7 HTTP 502 dns resolve failed: context deadline exceeded"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got := Redact(tc.input)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			if Redact(got) != got {
				t.Fatal("repeated redaction changed the output")
			}
		})
	}
}

func assertProxyFailureRedacted(t *testing.T, text string) {
	t.Helper()
	for _, secret := range []string{"192.0.2.8", "63839", "api.github.com", "static.example-provider.io", "1337"} {
		if strings.Contains(text, secret) {
			t.Errorf("leaked %q: %s", secret, text)
		}
	}
	for _, evidence := range []string{"2026-11-14T10:20:30+08:00", "warning", "[TCP]", "dns resolve failed: context deadline exceeded"} {
		if !strings.Contains(text, evidence) {
			t.Errorf("lost diagnostic evidence %q: %s", evidence, text)
		}
	}
}

func TestProxyStreamsRedactBeforeStorageAndSink(t *testing.T) {
	for _, source := range []string{"proxy", "proxy-validation"} {
		t.Run(source, func(t *testing.T) {
			store := New(t.TempDir())
			var sink strings.Builder
			stream := NewStream(store, source, "error", func(line string) { sink.WriteString(line) })
			// Producers can split hostnames and credentials between writes.
			for _, b := range []byte(proxyFailure + "\n") {
				if _, err := stream.Write([]byte{b}); err != nil {
					t.Fatal(err)
				}
			}
			if err := stream.Close(); err != nil {
				t.Fatal(err)
			}
			assertProxyFailureRedacted(t, sink.String())
			events := readEvents(t, store)
			if len(events) != 1 || events[0].Source != source || events[0].Level != "warn" {
				t.Fatalf("unexpected records: %+v", events)
			}
			assertProxyFailureRedacted(t, events[0].Message)
		})
	}
}

func TestStreamConsumerKeepsStoredRecordsRedacted(t *testing.T) {
	store := New(t.TempDir())
	var sink, consumed strings.Builder
	stream := NewStreamWithConsumer(store, "task/setup", "info", func(line string) { sink.WriteString(line) }, func(line string) bool {
		consumed.WriteString(line)
		return true
	})
	if _, err := stream.Write([]byte(proxyFailure + "\n")); err != nil {
		t.Fatal(err)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	if consumed.String() != proxyFailure || sink.Len() != 0 {
		t.Fatal("internal consumer did not receive the original complete line or failed to suppress display")
	}
	events := readEvents(t, store)
	if len(events) != 1 {
		t.Fatalf("consumed line was lost from diagnostic storage: %+v", events)
	}
	assertProxyFailureRedacted(t, events[0].Message)
}

func TestExportRedactsLegacyProxyMessages(t *testing.T) {
	store := New(t.TempDir())
	if err := store.Clean(); err != nil {
		t.Fatal(err)
	}
	var legacy bytes.Buffer
	// A fixed time keeps the whole-file leak check from matching digits of
	// the current clock, such as "1337" in a nanosecond fraction.
	const legacyTime = "2026-11-14T02:20:30Z"
	for _, source := range []string{"proxy", "proxy-validation"} {
		message := strings.Replace(proxyFailure, "192.0.2.8", "[ip]", 1)
		if err := json.NewEncoder(&legacy).Encode(Event{Time: legacyTime, Level: "warn", Source: source, Message: message}); err != nil {
			t.Fatal(err)
		}
	}
	// Simulate a log retained from a version with incomplete redaction.
	if err := os.WriteFile(filepath.Join(store.dir, "events.jsonl"), legacy.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	var bundle bytes.Buffer
	if err := store.Export(&bundle, "test", []byte(proxyFailure), "collected"); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(bundle.Bytes()), int64(bundle.Len()))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, file := range archive.File {
		if file.Name != "logs/events.jsonl" && file.Name != "guest/services.log" {
			continue
		}
		r, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		assertProxyFailureRedacted(t, string(data))
		checked++
	}
	if checked != 2 {
		t.Fatal("export omitted diagnostic logs")
	}
}

func TestGuestRunnerRedactsProxyAddresses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("guest runner executes on Linux; POSIX shell fixture")
	}
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv unavailable")
	}
	t.Setenv("UV_CACHE_DIR", t.TempDir())
	script := filepath.Join(t.TempDir(), "runner.py")
	if err := os.WriteFile(script, []byte(GuestRunner()), 0600); err != nil {
		t.Fatal(err)
	}
	input := setupHint + "\n" + proxyFailure + "\n[::ffff:192.0.2.8]:443 fe80::1%en0 0123456789abcdef01234567 token=private-token" +
		"\nError: socket path `/Users/alice/deep folder/state/ssh.sock.1234` is too long: >= UNIX_PATH_MAX=104"
	cmd := exec.Command("uv", "run", "--no-project", "python", script, "/bin/sh", "-c", `printf '%s\n' "$1"`, "sh", input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("guest runner failed: %s: %v", stderr.String(), err)
	}
	assertProxyFailureRedacted(t, string(out))
	if want := Redact(input) + "\n"; string(out) != want {
		t.Fatalf("host and guest redaction differ: got %q, want %q", out, want)
	}
}

// The guest applies the exported table with Python's re module; it must give
// the host's result. Runs wherever a Python interpreter is installed.
func TestGuestRulesMatchHostRedaction(t *testing.T) {
	python := ""
	for _, name := range []string{"python3", "python"} {
		if path, err := exec.LookPath(name); err == nil {
			python = path
			break
		}
	}
	if python == "" {
		t.Skip("python unavailable")
	}
	inputs := []string{
		proxyFailure,
		"Login browser open failed: Browser command timed out: Page.navigate",
		"Target.createTarget failed near static.example-provider.io:1337 [::ffff:192.0.2.8]:443",
		"page.example-provider.io token=private-token 0123456789abcdef01234567",
	}
	dir := t.TempDir()
	rules := filepath.Join(dir, "rules.json")
	if err := os.WriteFile(rules, []byte(guestRules()), 0600); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(inputs)
	lines := filepath.Join(dir, "inputs.json")
	if err := os.WriteFile(lines, data, 0600); err != nil {
		t.Fatal(err)
	}
	// The endpoint rule with its known diagnostic forms runs last, as in
	// GuestRunner.
	hosts := filepath.Join(dir, "hosts.json")
	data, _ = json.Marshal([]string{hostInMessage.String(), publicDiagnostic.String()})
	if err := os.WriteFile(hosts, data, 0600); err != nil {
		t.Fatal(err)
	}
	program := `import json, re, sys
rules = [(re.compile(p), r) for p, r in json.load(open(sys.argv[1], encoding="utf-8"))]
host_in_message, public_diagnostic = [re.compile(p) for p in json.load(open(sys.argv[3], encoding="utf-8"))]
def redact_host(match):
    return match[0] if public_diagnostic.fullmatch(match[0]) else match[1] + '[host]'
out = []
for line in json.load(open(sys.argv[2], encoding="utf-8")):
    for pattern, replacement in rules:
        line = pattern.sub(lambda m: m.expand(replacement), line)
    out.append(host_in_message.sub(redact_host, line))
sys.stdout.write(json.dumps(out))
`
	output, err := exec.Command(python, "-I", "-c", program, rules, lines, hosts).Output()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal(output, &got); err != nil {
		t.Fatalf("%v: %s", err, output)
	}
	for i, input := range inputs {
		if got[i] != Redact(input) {
			t.Errorf("guest %q, host %q", got[i], Redact(input))
		}
	}
}
