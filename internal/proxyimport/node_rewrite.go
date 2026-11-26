package proxyimport

import (
	"fmt"
	"net/netip"
	"strings"
)

// A local client's own core applies its top-level hosts when it dials a node
// server; the managed core never reads hosts (use-hosts and use-system-hosts
// stay off). The import therefore writes each alias out to its target, and
// keeps the original name wherever the node would otherwise derive its TLS
// name or WebSocket Host from the server, so the certificate still matches.
// Nodes with their own dialer-proxy are left alone: their transit hop
// resolves the server name, not the client's hosts.
func expandNodeHosts(hosts map[string]any, proxies []map[string]any) {
	if len(hosts) == 0 {
		return
	}
	exact := map[string]string{}
	var patterns []hostPattern
	for key, value := range hosts {
		key = strings.ToLower(strings.TrimSpace(key))
		target := hostTarget(value)
		if key == "" || target == "" {
			continue
		}
		switch {
		case strings.HasPrefix(key, "+."):
			patterns = append(patterns, hostPattern{suffix: key[2:], self: true, deep: true, target: target})
		case strings.HasPrefix(key, "*."):
			patterns = append(patterns, hostPattern{suffix: key[2:], target: target})
		case strings.HasPrefix(key, "."):
			patterns = append(patterns, hostPattern{suffix: key[1:], deep: true, target: target})
		case !strings.Contains(key, "*"):
			exact[key] = target
		}
	}
	lookup := func(name string) string {
		if target, ok := exact[name]; ok {
			return target
		}
		best, length := "", -1
		for _, p := range patterns {
			if p.matches(name) && len(p.suffix) > length {
				best, length = p.target, len(p.suffix)
			}
		}
		return best
	}
	for _, p := range proxies {
		if dialer, _ := p["dialer-proxy"].(string); dialer != "" {
			continue
		}
		server, ok := p["server"].(string)
		original := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(server), "."))
		if !ok || original == "" {
			continue
		}
		if _, err := netip.ParseAddr(strings.Trim(original, "[]")); err == nil {
			continue
		}
		// A domain target is itself looked up again, as the client's core does.
		name, seen := original, map[string]bool{}
		for range 4 {
			target := lookup(name)
			if target == "" || seen[name] {
				break
			}
			seen[name] = true
			name = strings.ToLower(strings.TrimSuffix(target, "."))
			if _, err := netip.ParseAddr(strings.Trim(name, "[]")); err == nil {
				break
			}
		}
		if name == original {
			continue
		}
		keepServerName(p, strings.TrimSuffix(strings.TrimSpace(server), "."))
		p["server"] = strings.Trim(name, "[]")
	}
}

type hostPattern struct {
	suffix, target string
	self, deep     bool
}

func (p hostPattern) matches(name string) bool {
	if name == p.suffix {
		return p.self
	}
	prefix, ok := strings.CutSuffix(name, "."+p.suffix)
	return ok && prefix != "" && (p.deep || !strings.Contains(prefix, "."))
}

// hostTarget reads one hosts value: an address, a domain, or a list whose first
// entry the client's core tries first.
func hostTarget(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		if len(v) > 0 {
			if s, ok := v[0].(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func keepServerName(p map[string]any, name string) {
	typ, _ := p["type"].(string)
	typ = strings.ToLower(typ)
	tls := truthy(p["tls"])
	switch typ {
	case "vmess", "vless":
		if (tls || p["reality-opts"] != nil) && blank(p["servername"]) {
			p["servername"] = name
		}
	case "trojan", "hysteria", "hysteria2", "tuic", "anytls":
		if blank(p["sni"]) {
			p["sni"] = name
		}
	case "http", "socks5":
		if tls && blank(p["sni"]) {
			p["sni"] = name
		}
	}
	if network, _ := p["network"].(string); strings.EqualFold(network, "ws") {
		ws, _ := p["ws-opts"].(map[string]any)
		if ws == nil {
			ws = map[string]any{}
			p["ws-opts"] = ws
		}
		headers, _ := ws["headers"].(map[string]any)
		if headers == nil {
			headers = map[string]any{}
			ws["headers"] = headers
		}
		for key, value := range headers {
			if strings.EqualFold(key, "Host") && !blank(value) {
				return
			}
		}
		headers["Host"] = name
	}
}

func truthy(value any) bool {
	b, ok := value.(bool)
	return ok && b
}

func blank(value any) bool {
	s, ok := value.(string)
	return value == nil || ok && strings.TrimSpace(s) == ""
}

// builtinPolicy names the core's own policies, which are not transit nodes.
func builtinPolicy(name string) bool {
	switch strings.ToUpper(strings.TrimSpace(name)) {
	case "DIRECT", "REJECT", "REJECT-DROP", "PASS", "COMPATIBLE", "GLOBAL":
		return true
	}
	return false
}

// resolveDialerGroups rewrites a dialer-proxy that names a proxy group to one
// node of that group, because Cage dials a fixed transit hop: the member the
// client saved as the group's choice (selected), else the group's first listed
// member. It returns what it chose for groups with more than one member.
// Groups whose members come only from providers or filters, built-in policies
// and cycles stay unresolved, and the importer then skips the node.
func resolveDialerGroups(proxies []map[string]any, groups []map[string]any, selected map[string]string) []string {
	if len(groups) == 0 {
		return nil
	}
	nodes := map[string]bool{}
	for _, p := range proxies {
		if name, _ := p["name"].(string); name != "" {
			nodes[name] = true
		}
	}
	members := map[string][]string{}
	for _, g := range groups {
		name, _ := g["name"].(string)
		raw, _ := g["proxies"].([]any)
		if name == "" || nodes[name] {
			continue
		}
		var list []string
		for _, m := range raw {
			if s, ok := m.(string); ok && s != "" {
				list = append(list, s)
			}
		}
		members[name] = list
	}
	choose := func(group string) string {
		if pick := selected[group]; pick != "" && !builtinPolicy(pick) && (nodes[pick] || members[pick] != nil) {
			return pick
		}
		for _, m := range members[group] {
			if !builtinPolicy(m) {
				return m
			}
		}
		return ""
	}
	var notes []string
	noted := map[string]bool{}
	for _, p := range proxies {
		dialer, _ := p["dialer-proxy"].(string)
		if dialer == "" || nodes[dialer] {
			continue
		}
		if _, ok := members[dialer]; !ok {
			continue
		}
		target, several, seen := dialer, false, map[string]bool{}
		for range 8 {
			if nodes[target] || seen[target] {
				break
			}
			seen[target] = true
			several = several || len(members[target]) > 1
			target = choose(target)
			if target == "" {
				break
			}
		}
		if !nodes[target] {
			continue
		}
		p["dialer-proxy"] = target
		if several && !noted[dialer] {
			noted[dialer] = true
			notes = append(notes, fmt.Sprintf("中转分组「%s」导入为「%s」", dialer, target))
		}
	}
	return notes
}
