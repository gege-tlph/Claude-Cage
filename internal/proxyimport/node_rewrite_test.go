package proxyimport

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sylvinus/agent-vm/internal/managedproxy"
	"go.yaml.in/yaml/v3"
)

func parseNodes(t *testing.T, text string) (map[string]any, []map[string]any, []map[string]any) {
	t.Helper()
	var input struct {
		Hosts   map[string]any   `yaml:"hosts"`
		Groups  []map[string]any `yaml:"proxy-groups"`
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal([]byte(text), &input); err != nil {
		t.Fatal(err)
	}
	return input.Hosts, input.Groups, input.Proxies
}

func byName(proxies []map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, p := range proxies {
		out[p["name"].(string)] = p
	}
	return out
}

func TestExpandNodeHostsKeepsTheOriginalTLSName(t *testing.T) {
	hosts, _, proxies := parseNodes(t, `
hosts:
  alias.example.test: 203.0.113.7
  Upper.Example.Test: [203.0.113.8, 203.0.113.9]
  cname.example.test: entry.example.test
  entry.example.test: 203.0.113.10
  '+.wild.example.test': 203.0.113.11
  '*.one.example.test': 203.0.113.12
  loop-a.example.test: loop-b.example.test
  loop-b.example.test: loop-a.example.test
proxies:
  - {name: trojan, type: trojan, server: alias.example.test, port: 443, password: p}
  - {name: trojan-sni, type: trojan, server: alias.example.test, port: 443, password: p, sni: cert.example.test}
  - {name: vmess-ws, type: vmess, server: upper.example.test, port: 443, uuid: u, tls: true, network: ws, ws-opts: {path: /ws}}
  - {name: vmess-plain, type: vmess, server: alias.example.test, port: 80, uuid: u}
  - {name: vless-host, type: vless, server: alias.example.test, port: 443, uuid: u, tls: true, network: ws, ws-opts: {headers: {host: cdn.example.test}}}
  - {name: cname, type: hysteria2, server: cname.example.test, port: 443, password: p}
  - {name: wild-self, type: ss, server: wild.example.test, port: 443, cipher: aes-128-gcm, password: p}
  - {name: wild-deep, type: ss, server: a.b.wild.example.test, port: 443, cipher: aes-128-gcm, password: p}
  - {name: one-level, type: ss, server: a.one.example.test, port: 443, cipher: aes-128-gcm, password: p}
  - {name: two-levels, type: ss, server: a.b.one.example.test, port: 443, cipher: aes-128-gcm, password: p}
  - {name: chained, type: trojan, server: alias.example.test, port: 443, password: p, dialer-proxy: trojan}
  - {name: address, type: ss, server: 198.51.100.1, port: 443, cipher: aes-128-gcm, password: p}
  - {name: loop, type: ss, server: loop-a.example.test, port: 443, cipher: aes-128-gcm, password: p}
  - {name: unlisted, type: ss, server: other.example.test, port: 443, cipher: aes-128-gcm, password: p}
`)
	expandNodeHosts(hosts, proxies)
	got := byName(proxies)
	check := func(name, key string, want any) {
		t.Helper()
		if got[name][key] != want {
			t.Fatalf("%s %s = %v, want %v", name, key, got[name][key], want)
		}
	}
	check("trojan", "server", "203.0.113.7")
	check("trojan", "sni", "alias.example.test")
	check("trojan-sni", "sni", "cert.example.test")
	check("vmess-ws", "server", "203.0.113.8")
	check("vmess-ws", "servername", "upper.example.test")
	if host := got["vmess-ws"]["ws-opts"].(map[string]any)["headers"].(map[string]any)["Host"]; host != "upper.example.test" {
		t.Fatalf("ws Host = %v", host)
	}
	check("vmess-plain", "server", "203.0.113.7")
	if _, ok := got["vmess-plain"]["servername"]; ok {
		t.Fatal("a plain node gained a TLS name")
	}
	headers := got["vless-host"]["ws-opts"].(map[string]any)["headers"].(map[string]any)
	if len(headers) != 1 || headers["host"] != "cdn.example.test" {
		t.Fatalf("explicit Host changed: %v", headers)
	}
	check("cname", "server", "203.0.113.10")
	check("cname", "sni", "cname.example.test")
	check("wild-self", "server", "203.0.113.11")
	check("wild-deep", "server", "203.0.113.11")
	check("one-level", "server", "203.0.113.12")
	check("two-levels", "server", "a.b.one.example.test")
	check("chained", "server", "alias.example.test")
	check("address", "server", "198.51.100.1")
	check("unlisted", "server", "other.example.test")
	if server := got["loop"]["server"]; server != "loop-a.example.test" && server != "loop-b.example.test" {
		t.Fatalf("loop server = %v", server)
	}
}

func TestResolveDialerGroupsUsesTheSavedChoiceOrTheFirstMember(t *testing.T) {
	_, groups, proxies := parseNodes(t, `
proxy-groups:
  - {name: auto, type: url-test, proxies: [DIRECT, SG-1, SG-2]}
  - {name: chosen, type: select, proxies: [SG-1, SG-2]}
  - {name: nested, type: select, proxies: [auto]}
  - {name: only-direct, type: select, proxies: [DIRECT, REJECT]}
  - {name: providers, type: select, use: [remote]}
  - {name: single, type: select, proxies: [SG-2]}
  - {name: cycle-a, type: select, proxies: [cycle-b]}
  - {name: cycle-b, type: select, proxies: [cycle-a]}
proxies:
  - {name: SG-1, type: ss, server: sg1.example.test, port: 443, cipher: aes-128-gcm, password: p}
  - {name: SG-2, type: ss, server: sg2.example.test, port: 443, cipher: aes-128-gcm, password: p}
  - {name: via-auto, type: socks5, server: home.example.test, port: 1080, dialer-proxy: auto}
  - {name: via-chosen, type: socks5, server: home.example.test, port: 1081, dialer-proxy: chosen}
  - {name: via-nested, type: socks5, server: home.example.test, port: 1082, dialer-proxy: nested}
  - {name: via-direct, type: socks5, server: home.example.test, port: 1083, dialer-proxy: only-direct}
  - {name: via-providers, type: socks5, server: home.example.test, port: 1084, dialer-proxy: providers}
  - {name: via-single, type: socks5, server: home.example.test, port: 1085, dialer-proxy: single}
  - {name: via-cycle, type: socks5, server: home.example.test, port: 1086, dialer-proxy: cycle-a}
  - {name: via-node, type: socks5, server: home.example.test, port: 1087, dialer-proxy: SG-1}
`)
	notes := resolveDialerGroups(proxies, groups, map[string]string{"chosen": "SG-2", "only-direct": "DIRECT"})
	got := byName(proxies)
	for name, want := range map[string]string{
		"via-auto": "SG-1", "via-chosen": "SG-2", "via-nested": "SG-1", "via-direct": "only-direct",
		"via-providers": "providers", "via-single": "SG-2", "via-cycle": "cycle-a", "via-node": "SG-1",
	} {
		if got[name]["dialer-proxy"] != want {
			t.Fatalf("%s dialer-proxy = %v, want %s", name, got[name]["dialer-proxy"], want)
		}
	}
	want := []string{"中转分组「auto」导入为「SG-1」", "中转分组「chosen」导入为「SG-2」", "中转分组「nested」导入为「SG-1」"}
	if strings.Join(notes, "\n") != strings.Join(want, "\n") {
		t.Fatalf("notes = %q", notes)
	}
}

// #736: a Clash Verge Rev runtime config whose static exit node is chained
// through a url-test group, and whose airport nodes use hosts aliases.
func TestScanVergeImportsChainsThroughGroupsAndHostsAliases(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, "clash-verge.yaml", `
hosts:
  entry-alias.example.test: 203.0.113.20
dns:
  proxy-server-nameserver: ['https://223.5.5.5/dns-query#DIRECT', 'udp://127.0.0.1:7874', system]
proxy-groups:
  - {name: AI稳定, type: url-test, proxies: [SG-1, SG-2]}
  - {name: 节点选择, type: select, proxies: [AI稳定, SG-1, SG-2]}
proxies:
  - {name: SG-1, type: trojan, server: entry-alias.example.test, port: 443, password: p}
  - {name: SG-2, type: trojan, server: sg2.example.test, port: 443, password: p}
  - {name: STATIC_NODE, type: socks5, server: home.example.test, port: 1080, dialer-proxy: AI稳定}
  - {name: STATIC_SELECTED, type: socks5, server: home.example.test, port: 1081, dialer-proxy: 节点选择}
`)
	writeConfig(t, root, "profiles.yaml", `current: active
items:
  - uid: active
    name: Airport
    type: remote
    file: active.yaml
    url: https://subscription.example.test/token
    selected:
      - {name: 节点选择, now: SG-2}
`)
	writeConfig(t, root, "profiles/active.yaml", testNode)
	configs := Scan([]Location{{"Clash Verge Rev", root, Verge, ""}}, []byte("session"))
	if len(configs) != 2 || configs[0].Error != "" || configs[0].Nodes != 4 || configs[0].Skipped != 0 {
		t.Fatalf("unexpected result: %s", mustJSON(t, configs))
	}
	if configs[0].Warning != "中转分组「AI稳定」导入为「SG-1」；中转分组「节点选择」导入为「SG-2」。" {
		t.Fatalf("warning = %q", configs[0].Warning)
	}
	var store managedproxy.Store
	if err := store.Import(configs[0].Name, "", configs[0].Data, configs[0].SourceID, 0, ""); err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, n := range store.Sources[0].Nodes {
		ids[n.Name] = n.ID
	}
	for _, n := range store.Sources[0].Nodes {
		switch n.Name {
		case "STATIC_NODE":
			if n.Config["dialer-proxy"] != ids["SG-1"] {
				t.Fatalf("STATIC_NODE chained through %v", n.Config["dialer-proxy"])
			}
		case "STATIC_SELECTED":
			if n.Config["dialer-proxy"] != ids["SG-2"] {
				t.Fatalf("STATIC_SELECTED chained through %v", n.Config["dialer-proxy"])
			}
		case "SG-1":
			if n.Config["server"] != "203.0.113.20" || n.Config["sni"] != "entry-alias.example.test" {
				t.Fatalf("SG-1 alias not expanded: %v", n.Config)
			}
		}
	}
	var data struct {
		DNS map[string][]string `json:"dns"`
	}
	if err := json.Unmarshal(configs[0].Data, &data); err != nil {
		t.Fatal(err)
	}
	if servers := data.DNS["proxy-server-nameserver"]; len(servers) != 1 || servers[0] != "https://223.5.5.5/dns-query" {
		t.Fatalf("node DNS = %v", servers)
	}
}

// hosts and proxy-groups only fill in nodes: a repeated name or an entry of
// the wrong kind there does not fail the import.
func TestNodeDataSkipsBrokenHostsAndGroupEntries(t *testing.T) {
	data := []byte(`
hosts:
  entry-alias.example.test: 203.0.113.20
  entry-alias.example.test: 203.0.113.99
  odd.example.test: {nested: map}
proxy-groups:
  - just-a-string
  - {name: AI稳定, type: url-test, proxies: [SG-1]}
proxies:
  - {name: SG-1, type: trojan, server: entry-alias.example.test, port: 443, password: p}
  - {name: HOME, type: socks5, server: home.example.test, port: 1080, dialer-proxy: AI稳定}
`)
	out, warning, err := nodeData(t.TempDir(), "", data, nil)
	if err != nil {
		t.Fatal(err)
	}
	var store managedproxy.Store
	if err := store.Import("x", "", out, "x", 0, ""); err != nil {
		t.Fatal(err, warning)
	}
	for _, n := range store.Sources[0].Nodes {
		if n.Name == "SG-1" && n.Config["server"] != "203.0.113.20" {
			t.Fatalf("first hosts entry not used: %v", n.Config)
		}
		if n.Name == "HOME" && n.Config["dialer-proxy"] == "AI稳定" {
			t.Fatalf("group after a broken entry not resolved: %v", n.Config)
		}
	}
}

// A blank host header is filled in place, not joined by a second "Host".
func TestKeepServerNameFillsABlankHostHeader(t *testing.T) {
	p := map[string]any{"type": "vless", "network": "ws", "ws-opts": map[string]any{"headers": map[string]any{"host": ""}}}
	keepServerName(p, "alias.example.test")
	headers := p["ws-opts"].(map[string]any)["headers"].(map[string]any)
	if len(headers) != 1 || headers["host"] != "alias.example.test" {
		t.Fatalf("headers = %v", headers)
	}
}
