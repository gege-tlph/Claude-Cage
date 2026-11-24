package workbench

import (
	"encoding/json"
	"net/http"
	"os"
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
	same := func(got []string, want ...string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
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
	if err := writePrivate(cage.ResourceAllocationPath(s.configPath), cage.ResourceAllocation{MemoryGiB: 2, CPUs: 1, DiskGiB: 9999}); err != nil {
		t.Fatal(err)
	}
	got := pending(true)
	if !same(got, "balanced_display", "project_file_cache", "memory", "cpus", "disk") && !same(got, "balanced_display", "project_file_cache", "memory", "disk") && !same(got, "balanced_display", "project_file_cache", "cpus", "disk") {
		t.Fatalf("saved changes not pending: %v", got)
	}
	// Changing a setting back clears it.
	s.preferences.BalancedDisplay = true
	if got := pending(true); len(got) == 0 || got[0] == "balanced_display" {
		t.Fatalf("setting changed back still pending: %v", got)
	}
	// A start applies everything saved.
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
