package netguard

import (
	"errors"
	"net"
	"unsafe"

	"golang.org/x/sys/windows"
)

// hostAddrs lists this machine's addresses for every new flow (offHost).
// net.InterfaceAddrs asks GetAdaptersAddresses for every adapter's full
// description, about 9 ms per call, and offHost runs for each connection the
// guest opens and each UDP packet it sends. The unicast address table holds
// the same addresses in well under a millisecond. It is still read fresh for
// every flow: nothing is cached, so a newly assigned address is seen at once.
func hostAddrs() ([]net.Addr, error) {
	var table *windows.MibUnicastIpAddressTable
	if err := windows.GetUnicastIpAddressTable(windows.AF_UNSPEC, &table); err != nil {
		return nil, err
	}
	defer windows.FreeMibTable(unsafe.Pointer(table))
	if table.NumEntries == 0 {
		return nil, nil
	}
	rows := unsafe.Slice(&table.Table[0], table.NumEntries)
	addrs := make([]net.Addr, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		var ip net.IP
		switch row.Address.Family {
		case windows.AF_INET:
			// SOCKADDR_INET is a union: an IPv4 row is a sockaddr_in.
			v4 := (*windows.RawSockaddrInet4)(unsafe.Pointer(&row.Address))
			ip = net.IP(append([]byte(nil), v4.Addr[:]...))
		case windows.AF_INET6:
			ip = net.IP(append([]byte(nil), row.Address.Addr[:]...))
		default:
			// An address offHost cannot read: treat the list as unknown.
			return nil, errors.New("unknown address family in the unicast address table")
		}
		addrs = append(addrs, &net.IPNet{IP: ip, Mask: net.CIDRMask(int(row.OnLinkPrefixLength), len(ip)*8)})
	}
	return addrs, nil
}
