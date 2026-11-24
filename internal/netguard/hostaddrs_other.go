//go:build !windows

package netguard

import "net"

// hostAddrs lists this machine's addresses for every new flow (offHost).
var hostAddrs = net.InterfaceAddrs
