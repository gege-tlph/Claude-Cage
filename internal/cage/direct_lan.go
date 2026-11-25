package cage

import (
	"errors"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/sylvinus/agent-vm/internal/netguard"
)

// A direct LAN lets the guest reach one private IPv4 network of the user's,
// such as a NAS or a development server, over TCP without the proxy. It
// follows the gate: only while networking is open after a passed check, and
// its connections end when the gate closes. Addresses of this host stay
// unreachable, so no host listener is exposed through a LAN address.
var directLANRanges = [...]netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
}

// Networks Cage itself places the guest on: Lima user-mode networking and the
// container network.
var directLANReserved = [...]netip.Prefix{
	netip.MustParsePrefix("192.168.5.0/24"),
	netip.MustParsePrefix("192.168.64.0/24"),
}

var (
	errDirectLAN         = errors.New("局域网直连须填写私有 IPv4 网段，如 192.168.1.0/24")
	errDirectLANReserved = errors.New("局域网直连不能包含环境自身使用的 192.168.5.0/24 或 192.168.64.0/24")
)

// ValidateDirectLAN accepts no network, or a canonical private IPv4 CIDR.
func ValidateDirectLAN(p netip.Prefix) error {
	if p == (netip.Prefix{}) {
		return nil
	}
	if !p.IsValid() || !p.Addr().Is4() || p != p.Masked() {
		return errDirectLAN
	}
	private := false
	for _, r := range directLANRanges {
		private = private || p.Bits() >= r.Bits() && r.Contains(p.Addr())
	}
	if !private {
		return errDirectLAN
	}
	for _, r := range directLANReserved {
		if p.Overlaps(r) {
			return errDirectLANReserved
		}
	}
	return nil
}

// ParseDirectLAN reads the settings field. Empty means none; host bits are
// cleared, so 192.168.1.20/24 is 192.168.1.0/24.
func ParseDirectLAN(value string) (netip.Prefix, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return netip.Prefix{}, nil
	}
	p, err := netip.ParsePrefix(value)
	if err != nil {
		return netip.Prefix{}, errDirectLAN
	}
	p = p.Masked()
	return p, ValidateDirectLAN(p)
}

// Tests replace these: a LAN host cannot be part of a unit test.
var (
	directLANOffHost = netguard.OffHost
	directLANDial    = (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext
)

// directLAN reports a TCP destination in the configured LAN that is not an
// address of this host.
func directLAN(lan netip.Prefix, network string, dst netip.AddrPort, offHost func(netip.Addr) bool) bool {
	a := dst.Addr().Unmap()
	return lan.IsValid() && network == "tcp" && dst.Port() != 0 && a.Is4() && lan.Contains(a) && offHost(a)
}
