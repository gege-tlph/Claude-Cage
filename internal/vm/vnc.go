package vm

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"github.com/lima-vm/lima/v2/pkg/limatype"
	"github.com/lima-vm/lima/v2/pkg/limatype/filenames"
	"github.com/lima-vm/lima/v2/pkg/portfwd"
	"go.yaml.in/yaml/v3"
)

// vncGuestPort is the guest's display server (Xtigervnc) on its loopback.
// The host reaches it through a forward to a host port chosen at each start,
// not the same port: Windows can reserve a port range for WinNAT or Hyper-V
// (netsh "excludedportrange") where no program can bind, and another program
// can hold the port. Either left the display unreachable.
const vncGuestPort = 15900

// vncDefaultPort is the host port of Lima's default forward of the display,
// the guest's own, which a start tries after the previous one. Tests replace
// it with DefaultVNCAddress: a developer's running environment holds 15900.
var vncDefaultPort = vncGuestPort

// DefaultVNCAddress is where the display of a VM started without the rule is
// served: Lima's default forward, to the same port on the host.
var DefaultVNCAddress = net.JoinHostPort("127.0.0.1", strconv.Itoa(vncDefaultPort))

// vncRule is the host port of the display forward among rules, 0 for none.
func vncRule(rules []limatype.PortForward) int {
	for _, rule := range rules {
		if rule.Ignore || rule.GuestSocket != "" || rule.GuestPortRange != [2]int{vncGuestPort, vncGuestPort} {
			continue
		}
		if rule.HostPortRange[0] > 0 && rule.HostPortRange[0] == rule.HostPortRange[1] {
			return rule.HostPortRange[0]
		}
	}
	return 0
}

// vncHostPort keeps the port of the previous start while it can be bound,
// then tries the guest's own port, then takes one the system picks: it never
// picks one from a reserved range.
func vncHostPort(ctx context.Context, rules []limatype.PortForward) (int, error) {
	free := func(port int) bool {
		return port > 0 && portfwd.InUse(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port))) == nil
	}
	if previous := vncRule(rules); free(previous) {
		return previous, nil
	}
	if free(vncDefaultPort) {
		return vncDefaultPort, nil
	}
	for range 8 {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return 0, err
		}
		port := listener.Addr().(*net.TCPAddr).Port
		_ = listener.Close()
		if free(port) {
			return port, nil
		}
	}
	return 0, errors.New("no free host port for the guest display")
}

// VNCAddress is the host address of the display of the VM in instanceDir, as
// its lima.yaml forwards it.
func VNCAddress(instanceDir string) string {
	var configured struct {
		PortForwards []struct {
			GuestPortRange [2]int `yaml:"guestPortRange"`
			HostPortRange  [2]int `yaml:"hostPortRange"`
			Ignore         bool   `yaml:"ignore"`
		} `yaml:"portForwards"`
	}
	b, err := os.ReadFile(filepath.Join(instanceDir, filenames.LimaYAML))
	if err != nil || yaml.Unmarshal(b, &configured) != nil {
		return DefaultVNCAddress
	}
	rules := make([]limatype.PortForward, 0, len(configured.PortForwards))
	for _, rule := range configured.PortForwards {
		rules = append(rules, limatype.PortForward{GuestPortRange: rule.GuestPortRange, HostPortRange: rule.HostPortRange, Ignore: rule.Ignore})
	}
	if port := vncRule(rules); port > 0 {
		return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	}
	return DefaultVNCAddress
}
