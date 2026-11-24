package cage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sylvinus/agent-vm/internal/guestprofile"
)

// The guest profile is loaded from its own file into a new pointer every
// time, so two loads of one configuration never compared equal: every
// settings save restarted a running environment (cage-30, upstream 558e4e78).
func TestConfigSameSettingsIgnoresLoadedGuestProfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	c := DefaultConfig()
	if err := writeConfigForTest(path, c); err != nil {
		t.Fatal(err)
	}
	profile, err := guestprofile.New(guestprofile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := guestprofile.Replace(dir, profile); err != nil {
		t.Fatal(err)
	}
	a, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if a.GuestProfile == nil || a == b {
		t.Fatal("fixture: loads should carry separate guest profile pointers")
	}
	if !a.SameSettings(b) {
		t.Fatal("two loads of one configuration differ")
	}
	// The settings form never carries the guest profile.
	form := a
	form.GuestProfile = nil
	if !a.SameSettings(form) {
		t.Fatal("a settings form without the guest profile counts as a change")
	}
	form.Timezone = "Asia/Tokyo"
	if a.SameSettings(form) {
		t.Fatal("a changed setting went unnoticed")
	}
}

func writeConfigForTest(path string, c Config) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}
