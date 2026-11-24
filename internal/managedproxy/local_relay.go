package managedproxy

import (
	"encoding/json"
	"errors"
	"math"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
)

// Local relays (#595, #787): a proxy port on this computer that the user
// allowed, for hosts whose path to the remote node needs a local VPN or proxy
// client. Every other local address stays refused. An allowed port is only
// the first hop of a chain: the final exit is always a concrete remote node,
// so the local client's routing rules never choose where Cage exits, and an
// account route never exits through it.
//
// A relay node is exactly 127.0.0.1 on an allowed port, other than Cage's own
// mixed port, as a plain socks5 or http proxy with optional credentials
// (import turns UDP off for every node).
// Other spellings of the address and any other dial option are refused, and
// it cannot itself dial through another node.

// ErrLocalRelayExit reports an allowed relay placed anywhere but first, or
// used alone. It matches ErrLocalProxyNode, so the saved route stays closed
// and is reported as needing to be reconfigured, but keeps its own message.
var ErrLocalRelayExit error = relayExitError{}

type relayExitError struct{}

func (relayExitError) Error() string { return "本机中转只能作为代理链第一跳" }
func (relayExitError) Unwrap() error { return ErrLocalProxyNode }

const localRelayHost = "127.0.0.1"

var localRelayKeys = map[string]bool{"name": true, "type": true, "server": true, "port": true, "username": true, "password": true, "udp": true}

// localRelays is the authorization a check runs against: the allowed ports
// and Cage's own mixed port, which can never be a relay.
type localRelays struct {
	ports []int
	own   int
}

func (s Store) relays() localRelays { return localRelays{ports: s.LocalRelays, own: s.Port} }

// relay reports whether a node configuration is an allowed relay. ignore names
// a key the caller adds itself (the runtime's interface binding).
func (r localRelays) relay(config map[string]any, ignore ...string) bool {
	if len(r.ports) == 0 {
		return false
	}
	typ, _ := config["type"].(string)
	server, _ := config["server"].(string)
	if typ != "socks5" && typ != "http" || server != localRelayHost {
		return false
	}
	port := nodePort(config["port"])
	if port == 0 || port == r.own || !slices.Contains(r.ports, port) {
		return false
	}
	for key := range config {
		if !localRelayKeys[key] && !slices.Contains(ignore, key) {
			return false
		}
	}
	return true
}

// nodePort reads a port from YAML (int) or from the JSON store (float64).
func nodePort(v any) int {
	var port float64
	switch x := v.(type) {
	case int:
		port = float64(x)
	case int64:
		port = float64(x)
	case uint64:
		port = float64(x)
	case float64:
		port = x
	default:
		return 0
	}
	if port < 1 || port > 65535 || port != math.Trunc(port) {
		return 0
	}
	return int(port)
}

// LocalRelay reports whether node id is an allowed relay.
func (s Store) LocalRelay(id string) bool {
	n, ok := s.Find(id)
	return ok && s.relays().relay(n.Config)
}

// AllowLocalRelay adds a port; RevokeLocalRelay removes it. Revoking blocks
// the port's nodes again, and every route through them, on the next check.
func (s *Store) AllowLocalRelay(port int) error {
	if port < 1 || port > 65535 {
		return errors.New("端口应为 1–65535")
	}
	if port == s.Port {
		return errors.New("不能使用 Cage 自己的代理端口")
	}
	if !slices.Contains(s.LocalRelays, port) {
		s.LocalRelays = append(s.LocalRelays, port)
		slices.Sort(s.LocalRelays)
	}
	return nil
}

func (s *Store) RevokeLocalRelay(port int) {
	s.LocalRelays = slices.DeleteFunc(s.LocalRelays, func(p int) bool { return p == port })
	if len(s.LocalRelays) == 0 {
		s.LocalRelays = nil
	}
}

// relaysBeside reads the authorization saved next to a runtime config, so the
// checks of a running core follow a revocation without a new apply. A store
// that cannot be read allows nothing.
func relaysBeside(configPath string) localRelays {
	b, err := os.ReadFile(filepath.Join(filepath.Dir(configPath), "sources.json"))
	if err != nil {
		return localRelays{}
	}
	var saved struct {
		Port        int   `json:"port"`
		LocalRelays []int `json:"local_relays"`
	}
	if json.Unmarshal(b, &saved) != nil {
		return localRelays{}
	}
	return localRelays{ports: saved.LocalRelays, own: saved.Port}
}

// loopbackInterface is the interface a relay hop is bound to, so the global
// binding to the physical uplink does not send it elsewhere: lo0 on macOS, lo
// on Linux, and the loopback pseudo-interface on Windows.
func loopbackInterface() string {
	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagLoopback != 0 && iface.Flags&net.FlagUp != 0 {
				return iface.Name
			}
		}
	}
	switch runtime.GOOS {
	case "darwin":
		return "lo0"
	case "windows":
		return "Loopback Pseudo-Interface 1"
	}
	return "lo"
}
