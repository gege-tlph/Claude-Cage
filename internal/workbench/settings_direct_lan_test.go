package workbench

import (
	"testing"

	"github.com/sylvinus/agent-vm/internal/cage"
)

func TestSettingsDirectLANIsNormalizedAndValidated(t *testing.T) {
	old := cage.DefaultConfig()
	input := settingsView(old, Preferences{Keyboard: "native"})
	input.DirectLAN = " 192.168.1.20/24 "
	c, p, err := proposedSettings(input, old, t.TempDir())
	if err != nil || c.DirectLAN.String() != "192.168.1.0/24" || p.DirectLAN != "192.168.1.0/24" {
		t.Fatalf("direct LAN %v, saved %q, %v", c.DirectLAN, p.DirectLAN, err)
	}
	// A running environment is restarted for it: the gate reads it at start.
	if c.SameSettings(old) || c.Fingerprint() == old.Fingerprint() {
		t.Fatal("a new direct LAN network was not seen as a settings change")
	}
	input.DirectLAN = "8.8.8.0/24"
	if _, _, err := proposedSettings(input, old, t.TempDir()); err == nil {
		t.Fatal("a public network was accepted")
	}
	input.DirectLAN = ""
	if c, p, err := proposedSettings(input, old, t.TempDir()); err != nil || c.DirectLAN.IsValid() || p.DirectLAN != "" {
		t.Fatalf("empty direct LAN: %v %q %v", c.DirectLAN, p.DirectLAN, err)
	}
}
