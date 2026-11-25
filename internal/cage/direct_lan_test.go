package cage

import (
	"context"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
)

func TestDirectLANAcceptsOnlyAPrivateIPv4Network(t *testing.T) {
	for _, tc := range []struct {
		value, want string
		ok          bool
	}{
		{"", "", true},
		{"192.168.1.0/24", "192.168.1.0/24", true},
		{" 192.168.1.20/24 ", "192.168.1.0/24", true},
		{"10.0.0.0/8", "10.0.0.0/8", true},
		{"172.20.0.0/16", "172.20.0.0/16", true},
		{"172.32.0.0/16", "", false},   // public
		{"8.8.8.0/24", "", false},      // public
		{"0.0.0.0/0", "", false},       // everything
		{"192.168.0.0/16", "", false},  // contains the guest's own network
		{"192.168.5.0/24", "", false},  // Lima user-mode network
		{"192.168.64.0/25", "", false}, // container network
		{"fd00::/8", "", false},        // IPv6
		{"192.168.1.1", "", false},     // not a network
		{"169.254.0.0/16", "", false},  // link-local
	} {
		p, err := ParseDirectLAN(tc.value)
		if (err == nil) != tc.ok || (tc.ok && tc.want != "" && p.String() != tc.want) || (tc.ok && tc.want == "" && p.IsValid()) {
			t.Errorf("ParseDirectLAN(%q) = %v, %v", tc.value, p, err)
		}
	}
	if ValidateDirectLAN(netip.MustParsePrefix("192.168.1.20/24")) == nil {
		t.Error("a non-canonical saved network was accepted")
	}
}

func TestDirectLANIsLoadedFromWorkbenchAndChangesFingerprint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data := `{"proxy_url":"socks5://127.0.0.1:17896","dns_url":"https://1.1.1.1/dns-query","timezone":"Etc/UTC","locale":"en_US.UTF-8","browser_language":"en-US","exit_policy":{}}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	without, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for value, want := range map[string]string{`"192.168.1.0/24"`: "192.168.1.0/24", `"8.8.8.0/24"`: "invalid Prefix"} {
		if err := os.WriteFile(filepath.Join(dir, "workbench.json"), []byte(`{"direct_lan":`+value+`}`), 0600); err != nil {
			t.Fatal(err)
		}
		with, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if with.DirectLAN.String() != want {
			t.Fatalf("direct_lan %s loaded as %v", value, with.DirectLAN)
		}
		if changed := with.Fingerprint() != without.Fingerprint(); changed != with.DirectLAN.IsValid() {
			t.Fatalf("direct_lan %s: fingerprint changed=%t", value, changed)
		}
	}
}

func TestGateAllowsDirectLANOnlyWhileOpen(t *testing.T) {
	offHost, dial := directLANOffHost, directLANDial
	t.Cleanup(func() { directLANOffHost, directLANDial = offHost, dial })
	host := netip.MustParseAddr("192.168.77.2")
	directLANOffHost = func(a netip.Addr) bool { return a != host }
	var far net.Conn
	directLANDial = func(_ context.Context, network, address string) (net.Conn, error) {
		if network != "tcp4" || address != "192.168.77.10:445" {
			t.Errorf("dialed %s %s", network, address)
		}
		near, other := net.Pipe()
		far = other
		return near, nil
	}
	cfg := DefaultConfig()
	cfg.ProxyURL = "http://127.0.0.1:9"
	cfg.DirectLAN = netip.MustParsePrefix("192.168.77.0/24")
	g, err := NewEgressGate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close("test complete")
	nas := netip.MustParseAddrPort("192.168.77.10:445")
	if g.Allows("tcp", nas) {
		t.Fatal("direct LAN allowed before the check")
	}
	if _, err := g.DialContext(context.Background(), "tcp", nas.String()); err == nil {
		t.Fatal("direct LAN dialed before the check")
	}
	fixtureGrant(t, g)
	if !g.Allows("tcp", nas) {
		t.Fatal("direct LAN refused while networking is open")
	}
	for _, refused := range []struct {
		network string
		dst     netip.AddrPort
	}{
		{"udp", nas},
		{"tcp", netip.MustParseAddrPort("192.168.78.10:445")},
		{"tcp", netip.AddrPortFrom(host, 445)},
	} {
		if g.Allows(refused.network, refused.dst) {
			t.Errorf("allowed %s %v", refused.network, refused.dst)
		}
	}
	conn, err := g.DialContext(context.Background(), "tcp", nas.String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	defer far.Close()
	go func() { b := make([]byte, 4); _, _ = far.Read(b) }()
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	g.Close("closed by test")
	if _, err := conn.Write([]byte("must not send")); err == nil {
		t.Fatal("a direct LAN connection outlived the closed gate")
	}
	if g.Allows("tcp", nas) {
		t.Fatal("direct LAN allowed after the gate closed")
	}
}
