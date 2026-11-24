package workbench

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/sylvinus/agent-vm/internal/cage"
	"github.com/sylvinus/agent-vm/internal/guestprofile"
)

// Settings read only at environment start are reported as pending while the
// environment runs with older ones, and no longer once it has restarted.
func TestRestartPendingNamesSettingsSavedWhileRunning(t *testing.T) {
	s := testServer(t)
	s.preferences.BalancedDisplay = true
	pending := func(running bool) []string {
		t.Helper()
		saved := s.savedRestartSettings()
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.restartPendingLocked(running, saved)
	}
	if got := pending(false); len(got) != 0 {
		t.Fatalf("stopped environment has pending settings: %v", got)
	}
	// Already running when the workbench started: the saved ones are in use.
	if got := pending(true); len(got) != 0 {
		t.Fatalf("unknown start reported pending: %v", got)
	}
	s.preferences.BalancedDisplay = false
	s.preferences.ProjectFileCache = true
	// The VM's disk is 24 GiB now.
	if err := os.MkdirAll(s.instanceDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.instanceDir(), "lima.yaml"), []byte("disk: 24GiB\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Saving the memory writes the present disk size with it: not a disk change.
	if err := writePrivate(cage.ResourceAllocationPath(s.configPath), cage.ResourceAllocation{MemoryGiB: 2, CPUs: 1, DiskGiB: 24}); err != nil {
		t.Fatal(err)
	}
	got := pending(true)
	for _, id := range got {
		if id == "disk" {
			t.Fatalf("disk at its present size reported pending: %v", got)
		}
	}
	if len(got) < 3 || got[0] != "balanced_display" || got[1] != "project_file_cache" {
		t.Fatalf("saved changes not pending: %v", got)
	}
	if err := writePrivate(cage.ResourceAllocationPath(s.configPath), cage.ResourceAllocation{MemoryGiB: 2, CPUs: 1, DiskGiB: 40}); err != nil {
		t.Fatal(err)
	}
	if got := pending(true); got[len(got)-1] != "disk" {
		t.Fatalf("a larger disk is not pending: %v", got)
	}
	// Changing a setting back clears it.
	s.preferences.BalancedDisplay = true
	if got := pending(true); len(got) == 0 || got[0] == "balanced_display" {
		t.Fatalf("setting changed back still pending: %v", got)
	}
	// A start applies everything saved (here the disk grew to 40 GiB).
	if err := os.WriteFile(filepath.Join(s.instanceDir(), "lima.yaml"), []byte("disk: 40GiB\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.markApplied()
	if got := pending(true); len(got) != 0 {
		t.Fatalf("pending after a start: %v", got)
	}
	// Stopping forgets; the status reports the list.
	pending(false)
	if s.applied != nil {
		t.Fatal("stopped environment kept its applied settings")
	}
	w := desktopRequestForTest(s, http.MethodGet, "/api/status", nil)
	var view struct {
		RestartPending []string `json:"restart_pending"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &view) != nil || view.RestartPending == nil {
		t.Fatalf("status lacks restart_pending: %d %s", w.Code, w.Body.String())
	}
	// Restarting needs a running environment.
	if w := desktopRequestForTest(s, http.MethodPost, "/api/environment/restart", []byte(`{}`)); w.Code != http.StatusConflict {
		t.Fatalf("restart of a stopped environment: %d", w.Code)
	}
	_ = os.Remove(cage.ResourceAllocationPath(s.configPath))
}

// Saving a preference applied at the next start (balanced display) must not
// count as an environment change, or the save restarts the environment at
// once. With a guest profile on disk every load compared unequal before.
func TestPreferenceSaveDoesNotRestartEnvironment(t *testing.T) {
	s := testServer(t)
	profile, err := guestprofile.New(guestprofile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := guestprofile.Replace(s.profile, profile); err != nil {
		t.Fatal(err)
	}
	old, err := loadConfig(s.configPath)
	if err != nil || old.GuestProfile == nil {
		t.Fatal("fixture needs a loaded guest profile", err)
	}
	form := settingsView(old, s.preferences)
	form.BalancedDisplay = !form.BalancedDisplay
	form.Clock12Hour = !form.Clock12Hour
	c, p, err := proposedSettings(form, old, s.profile)
	if err != nil {
		t.Fatal(err)
	}
	if old == c {
		t.Fatal("fixture: the form's configuration should differ by pointer only")
	}
	if environmentSettingsChanged(old, s.preferences, savedProfile{c, p}) {
		t.Fatal("a preference-only save restarts the environment")
	}
	form.Timezone = "Asia/Tokyo"
	if c, p, err = proposedSettings(form, old, s.profile); err != nil {
		t.Fatal(err)
	}
	if !environmentSettingsChanged(old, s.preferences, savedProfile{c, p}) {
		t.Fatal("a time zone change went unnoticed")
	}
}
