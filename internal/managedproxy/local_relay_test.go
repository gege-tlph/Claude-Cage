package managedproxy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// relayStore imports a source with a local relay and a remote node that
// dials through it, plus an independent remote node.
func relayStore(t *testing.T, allow bool) (string, Store, string, string, string) {
	t.Helper()
	profile := t.TempDir()
	s, err := Load(profile)
	if err != nil {
		t.Fatal(err)
	}
	if allow {
		if err := s.AllowLocalRelay(7890); err != nil {
			t.Fatal(err)
		}
	}
	const data = "proxies: [{name: relay, type: socks5, server: 127.0.0.1, port: 7890, udp: true}, {name: via, type: http, server: remote.example, port: 8080, dialer-proxy: relay}, {name: direct, type: socks5, server: direct.example, port: 1080}]"
	if err := s.Import("mixed", "", []byte(data), "mixed", 0, ""); err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, n := range s.Sources[0].Nodes {
		ids[n.Name] = n.ID
	}
	return profile, s, ids["relay"], ids["via"], ids["direct"]
}

func TestLocalRelayRefusedByDefault(t *testing.T) {
	_, s, relay, via, direct := relayStore(t, false)
	if relay != "" || via != "" || direct == "" {
		t.Fatalf("local node imported without permission: %+v", s.Sources[0].Nodes)
	}
	if !strings.Contains(s.Sources[0].Skipped[0].Reason, ErrLocalProxyNode.Error()) {
		t.Fatalf("skip reason: %+v", s.Sources[0].Skipped)
	}
}

func TestLocalRelayOnlyAsFirstHop(t *testing.T) {
	profile, s, relay, via, direct := relayStore(t, true)
	if relay == "" || via == "" || len(s.BlockedNodes()) != 0 || !s.LocalRelay(relay) || s.LocalRelay(via) {
		t.Fatalf("allowed relay not imported: %+v %v", s.Sources[0], s.BlockedNodes())
	}
	// Imported dialer: relay -> via.
	s.Selected = via
	if route, err := s.Route(); err != nil || !slices.Equal(route, []string{relay, via}) {
		t.Fatalf("imported relay chain: %v %v", route, err)
	}
	// Explicit chain: relay -> direct.
	s.Selected, s.Chain = direct, []string{relay, direct}
	if route, err := s.Route(); err != nil || !slices.Equal(route, []string{relay, direct}) {
		t.Fatalf("explicit relay chain: %v %v", route, err)
	}
	for _, bad := range []struct {
		selected string
		chain    []string
	}{
		{relay, nil},
		{relay, []string{direct, relay}},
		{direct, []string{via, relay, direct}},
	} {
		s.Selected, s.Chain = bad.selected, bad.chain
		if _, err := s.Route(); !errors.Is(err, ErrLocalRelayExit) || !errors.Is(err, ErrLocalProxyNode) {
			t.Errorf("relay accepted outside the first hop %v: %v", bad.chain, err)
		}
		if _, err := s.Runtime(profile); err == nil {
			t.Errorf("runtime generated for %v", bad.chain)
		}
	}
	// Accounts never exit through the relay; a remote node behind it is fine.
	if _, err := s.NodeEndpoint(relay); !errors.Is(err, ErrLocalProxyNode) {
		t.Errorf("account endpoint through relay: %v", err)
	}
	if _, err := s.NodeEndpoint(via); err != nil {
		t.Errorf("account endpoint behind relay: %v", err)
	}
	s.Selected, s.Chain = via, nil
	wire, err := s.Runtime(profile)
	if err != nil {
		t.Fatal(err)
	}
	var core struct {
		Proxies        []map[string]any
		Authentication []string
		Rules          []string
	}
	if err := yaml.Unmarshal(wire, &core); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(core.Authentication, " "), nodeUser(relay)) || strings.Contains(strings.Join(core.Rules, " "), nodeUser(relay)) {
		t.Fatalf("relay has an account route: %v", core.Rules)
	}
	found := false
	for _, p := range core.Proxies {
		if p["name"] == relay {
			found = true
		}
		if p["name"] == via && p["dialer-proxy"] != relay {
			t.Fatalf("remote node does not dial through relay: %v", p)
		}
	}
	if !found {
		t.Fatal("relay missing from runtime")
	}
}

func TestLocalRelayRejectsOtherSpellingsPortsAndOptions(t *testing.T) {
	relays := localRelays{ports: []int{7890}, own: 17896}
	good := map[string]any{"name": "id", "type": "socks5", "server": "127.0.0.1", "port": 7890, "username": "u", "password": "p", "udp": false}
	if !relays.relay(good) {
		t.Fatal("plain relay refused")
	}
	if !relays.relay(map[string]any{"name": "id", "type": "http", "server": "127.0.0.1", "port": float64(7890)}) {
		t.Fatal("http relay from the JSON store refused")
	}
	for name, change := range map[string]func(map[string]any){
		"localhost":     func(c map[string]any) { c["server"] = "localhost" },
		"127.1":         func(c map[string]any) { c["server"] = "127.1" },
		"other loop":    func(c map[string]any) { c["server"] = "127.0.0.2" },
		"ipv6 loopback": func(c map[string]any) { c["server"] = "::1" },
		"other port":    func(c map[string]any) { c["port"] = 7891 },
		"fraction port": func(c map[string]any) { c["port"] = 7890.5 },
		"protocol":      func(c map[string]any) { c["type"] = "vmess" },
		"dialer":        func(c map[string]any) { c["dialer-proxy"] = "other" },
		"tls":           func(c map[string]any) { c["tls"] = true },
		"interface":     func(c map[string]any) { c["interface-name"] = "en0" },
		"routing mark":  func(c map[string]any) { c["routing-mark"] = 1 },
		"tfo":           func(c map[string]any) { c["tfo"] = true },
	} {
		c := map[string]any{}
		for k, v := range good {
			c[k] = v
		}
		change(c)
		if relays.relay(c) {
			t.Errorf("%s accepted as relay", name)
		}
	}
	if (localRelays{ports: []int{17896}, own: 17896}).relay(map[string]any{"type": "socks5", "server": "127.0.0.1", "port": 17896}) {
		t.Error("Cage's own port accepted as relay")
	}
	if (localRelays{}).relay(good) {
		t.Error("relay accepted without permission")
	}
	s := Store{Port: 17896}
	if err := s.AllowLocalRelay(17896); err == nil {
		t.Error("allowed Cage's own port")
	}
	for _, port := range []int{0, 65536} {
		if err := s.AllowLocalRelay(port); err == nil {
			t.Errorf("allowed port %d", port)
		}
	}
}

func TestRevokingLocalRelayBlocksItsRoutes(t *testing.T) {
	profile, s, relay, via, direct := relayStore(t, true)
	s.Selected = via
	s.Revision = "applied"
	if err := s.Save(profile); err != nil {
		t.Fatal(err)
	}
	wire, err := s.Runtime(profile)
	if err != nil {
		t.Fatal(err)
	}
	physical := "eth-test"
	bound, err := BindInterface(wire, physical)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(Dir(profile), "config.yaml")
	if err := os.WriteFile(path, bound, 0o600); err != nil {
		t.Fatal(err)
	}
	var core struct {
		Proxies []map[string]any
	}
	if err := yaml.Unmarshal(bound, &core); err != nil {
		t.Fatal(err)
	}
	for _, p := range core.Proxies {
		if p["name"] == relay && p["interface-name"] != loopbackInterface() {
			t.Fatalf("relay not bound to loopback: %v", p)
		}
	}
	if _, err := BoundInterface(path); err != nil {
		t.Fatalf("running relay route refused: %v", err)
	}
	if !BindingMatches(path, physical) {
		t.Fatal("relay binding not recognised")
	}
	if _, err := Applied(profile, s.Endpoint(), s.Revision); err != nil {
		t.Fatalf("applied relay route refused: %v", err)
	}

	s.RevokeLocalRelay(7890)
	if s.LocalRelays != nil {
		t.Fatalf("revoke left %v", s.LocalRelays)
	}
	if err := s.Save(profile); err != nil {
		t.Fatal(err)
	}
	blocked := s.BlockedNodes()
	if blocked[relay] == "" || blocked[via] == "" || blocked[direct] != "" {
		t.Fatalf("revoked relay and its dependents not blocked: %v", blocked)
	}
	if _, err := s.Route(); !errors.Is(err, ErrLocalProxyNode) {
		t.Fatalf("route through revoked relay: %v", err)
	}
	// The core still running the old config fails closed at once.
	if _, err := BoundInterface(path); !errors.Is(err, ErrLocalProxyNode) {
		t.Fatalf("running config kept revoked relay: %v", err)
	}
	if BindingMatches(path, physical) {
		t.Fatal("binding of revoked relay still matches")
	}
	if _, err := Applied(profile, s.Endpoint(), s.Revision); !errors.Is(err, ErrLocalProxyNode) {
		t.Fatalf("applied after revoke: %v", err)
	}
}

func TestLocalRelayBindingNeedsLoopbackInterface(t *testing.T) {
	profile, s, _, via, _ := relayStore(t, true)
	s.Selected = via
	if err := s.Save(profile); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(Dir(profile), "config.yaml")
	for name, iface := range map[string]string{"physical": "eth-test", "missing": ""} {
		config := "interface-name: eth-test\nproxies: [{name: r, type: socks5, server: 127.0.0.1, port: 7890, udp: false"
		if iface != "" {
			config += ", interface-name: " + iface
		}
		if err := os.WriteFile(path, []byte(config+"}]\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if BindingMatches(path, "eth-test") {
			t.Errorf("relay bound to %s accepted", name)
		}
	}
}

// A local relay goes first and fixed only: not after an auto group, where it
// would dial through the group, and not among a whole source's candidates.
func TestLocalRelayAndRelayGroups(t *testing.T) {
	_, s, relay, via, direct := relayStore(t, true)
	if err := s.Import("remote", "", []byte("proxies: [{name: r1, type: socks5, server: r1.example, port: 1080}, {name: r2, type: socks5, server: r2.example, port: 1080}, {name: exit, type: socks5, server: exit.example, port: 1080}]"), "remote", 0, ""); err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, n := range s.Sources[1].Nodes {
		ids[n.Name] = n.ID
	}
	g := s
	err := g.SetRouteHops([]RouteHop{{Relay: &RelayGroup{Strategy: RelayStrategyFallback, SourceID: "remote"}}, {NodeID: relay}, {NodeID: ids["exit"]}})
	if !errors.Is(err, ErrLocalRelayExit) {
		t.Fatalf("a local relay after a group: %v", err)
	}
	g = s
	if err := g.SetRouteHops([]RouteHop{{NodeID: ids["r1"]}, {Relay: &RelayGroup{Strategy: RelayStrategyFallback, SourceID: "mixed"}}, {NodeID: ids["exit"]}}); err != nil {
		t.Fatal(err)
	}
	hops, err := g.ResolveRoute()
	if err != nil || slices.Contains(hops[1].Candidates, relay) || !slices.Contains(hops[1].Candidates, via) || !slices.Contains(hops[1].Candidates, direct) {
		t.Fatalf("whole-source candidates: %+v %v", hops, err)
	}
	// First and fixed, before a group: allowed.
	g = s
	if err := g.SetRouteHops([]RouteHop{{NodeID: relay}, {Relay: &RelayGroup{Strategy: RelayStrategyFallback, SourceID: "remote"}}, {NodeID: ids["exit"]}}); err != nil {
		t.Fatal(err)
	}
}

// A subscription update whose route fails for a reason other than an empty
// group keeps that reason: here a whole source grown past 300 nodes.
func TestRelayRefreshKeepsOtherReasons(t *testing.T) {
	s, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	nodes := func(n int) []byte {
		var b strings.Builder
		b.WriteString("proxies:\n")
		for i := range n {
			fmt.Fprintf(&b, "  - {name: n%d, type: socks5, server: n%d.example, port: 1080}\n", i, i)
		}
		return []byte(b.String())
	}
	if err := s.Import("big", "", nodes(3), "big", 0, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Import("home", "", []byte("proxies: [{name: exit, type: socks5, server: exit.example, port: 1080}]"), "home", 0, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRouteHops([]RouteHop{{Relay: &RelayGroup{Strategy: RelayStrategyURLTest, SourceID: "big"}}, {NodeID: s.Sources[1].Nodes[0].ID}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Import("big", "", nodes(MaxRelayGroupNodes+1), "big", 0, ""); !errors.Is(err, ErrRelayTooMany) {
		t.Fatalf("grown past the limit: %v", err)
	}
	// Chosen nodes all dropped: the group has none.
	if err := s.SetRouteHops([]RouteHop{{Relay: &RelayGroup{Strategy: RelayStrategyFallback, NodeIDs: []string{s.Sources[0].Nodes[0].ID, s.Sources[0].Nodes[1].ID}}}, {NodeID: s.Sources[1].Nodes[0].ID}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Import("big", "", []byte("proxies: [{name: other, type: socks5, server: other.example, port: 1080}]"), "big", 0, ""); !errors.Is(err, ErrRelayRefreshEmptied) {
		t.Fatalf("emptied group: %v", err)
	}
}
