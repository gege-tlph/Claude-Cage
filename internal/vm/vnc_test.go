package vm

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/lima-vm/lima/v2/pkg/limatype"
)

func displayRule(port int) []limatype.PortForward {
	return []limatype.PortForward{
		{GuestPortRange: [2]int{40000, 40000}, HostPortRange: [2]int{40001, 40001}},
		{GuestPortRange: [2]int{vncGuestPort, vncGuestPort}, HostPortRange: [2]int{port, port}},
	}
}

func unusedPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port
}

// The port of the previous start stays while nothing holds it, so a running
// display keeps its address across restarts.
func TestVNCHostPortKeepsPrevious(t *testing.T) {
	previous := unusedPort(t)
	got, err := vncHostPort(context.Background(), displayRule(previous))
	if err != nil || got != previous {
		t.Errorf("got %d, %v; want %d", got, err, previous)
	}
}

// A held port, as one in a range Windows reserved for WinNAT is, is never
// chosen again: the forward to it would fail and the display stay dark.
func TestVNCHostPortLeavesHeldPort(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	port := held.Addr().(*net.TCPAddr).Port
	got, err := vncHostPort(context.Background(), displayRule(port))
	if err != nil || got == port || got <= 0 {
		t.Errorf("got %d, %v; held %d", got, err, port)
	}
	// The guest's own port held too: the system picks another one.
	guest, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(vncGuestPort)))
	if err != nil {
		t.Skipf("%d is held on this host: %v", vncGuestPort, err)
	}
	defer guest.Close()
	got, err = vncHostPort(context.Background(), displayRule(port))
	if err != nil || got == port || got == vncGuestPort || got <= 0 {
		t.Errorf("got %d, %v; held %d and %d", got, err, port, vncGuestPort)
	}
}

func TestVNCAddress(t *testing.T) {
	dir := t.TempDir()
	if got := VNCAddress(dir); got != DefaultVNCAddress {
		t.Errorf("no lima.yaml: %s", got)
	}
	write := func(content string) {
		if err := os.WriteFile(filepath.Join(dir, "lima.yaml"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A VM started before the rule: Lima's default, the same port.
	write("portForwards:\n- guestPortRange: [16080, 16080]\n  ignore: true\n")
	if got := VNCAddress(dir); got != DefaultVNCAddress {
		t.Errorf("without the rule: %s", got)
	}
	write("portForwards:\n- guestPortRange: [16080, 16080]\n  ignore: true\n" +
		"- guestIP: 127.0.0.1\n  guestPortRange: [15900, 15900]\n  hostIP: 127.0.0.1\n  hostPortRange: [49321, 49321]\n  proto: tcp\n")
	if got := VNCAddress(dir); got != "127.0.0.1:49321" {
		t.Errorf("with the rule: %s", got)
	}
	write("portForwards: [")
	if got := VNCAddress(dir); got != DefaultVNCAddress {
		t.Errorf("unreadable: %s", got)
	}
}
