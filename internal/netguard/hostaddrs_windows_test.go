package netguard

import (
	"net"
	"net/netip"
	"testing"
)

func addrSet(t *testing.T, addrs []net.Addr) map[netip.Addr]bool {
	t.Helper()
	set := map[netip.Addr]bool{}
	for _, a := range addrs {
		n, ok := a.(*net.IPNet)
		if !ok {
			t.Fatalf("address %v is %T, not *net.IPNet", a, a)
		}
		ip, ok := netip.AddrFromSlice(n.IP)
		if !ok {
			t.Fatalf("unreadable address %v", n.IP)
		}
		set[ip.Unmap()] = true
	}
	return set
}

// The fast table must never know fewer addresses than net.InterfaceAddrs:
// one it missed would count as off this machine.
func TestHostAddrsCoverInterfaceAddrs(t *testing.T) {
	var missing []netip.Addr
	// Addresses can change between the two reads; a real gap persists.
	for attempt := 0; attempt < 5; attempt++ {
		standard, err := net.InterfaceAddrs()
		if err != nil {
			t.Fatal(err)
		}
		fast, err := hostAddrs()
		if err != nil {
			t.Fatal(err)
		}
		if len(fast) == 0 {
			t.Fatal("no host addresses")
		}
		have := addrSet(t, fast)
		missing = missing[:0]
		for a := range addrSet(t, standard) {
			if !have[a] {
				missing = append(missing, a)
			}
		}
		if len(missing) == 0 {
			return
		}
	}
	t.Fatalf("unicast table lacks %v", missing)
}
