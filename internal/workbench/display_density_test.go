package workbench

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// "Balanced display" renders the guest at 3/4 of a high-density screen's
// pixels, so the remote display is upscaled and blurry. The first time the
// window reports such a screen it is turned off once; a later choice to turn
// it back on is kept.
func TestBalancedDisplayOffOnceOnHighDensityScreens(t *testing.T) {
	s := testServer(t)
	s.preferences.BalancedDisplay = true
	report := func(ratio string) map[string]any {
		t.Helper()
		w := desktopRequestForTest(s, http.MethodPost, "/api/display-density", []byte(`{"device_pixel_ratio":`+ratio+`}`))
		var reply map[string]any
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &reply) != nil {
			t.Fatalf("display density %s: HTTP %d %s", ratio, w.Code, w.Body.String())
		}
		return reply
	}
	saved := func() Preferences {
		t.Helper()
		var p Preferences
		b, err := os.ReadFile(filepath.Join(s.profile, "workbench.json"))
		if err != nil || json.Unmarshal(b, &p) != nil {
			t.Fatal("workbench.json", err)
		}
		return p
	}

	if reply := report("1"); reply["changed"] != false || reply["balanced_display"] != true || s.preferences.DisplayDensityChecked {
		t.Fatalf("a standard screen changed the preference: %v", reply)
	}
	if reply := report("1.5"); reply["changed"] != true || reply["balanced_display"] != false {
		t.Fatalf("high-density screen kept balanced display: %v", reply)
	}
	if p := saved(); p.BalancedDisplay || !p.DisplayDensityChecked {
		t.Fatalf("change not saved: %+v", p)
	}

	// Saving settings keeps the server's record, whatever the client sends.
	w := desktopRequestForTest(s, http.MethodGet, "/api/settings", nil)
	var settings map[string]any
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &settings) != nil {
		t.Fatal("settings", w.Code)
	}
	settings["balanced_display"] = true
	settings["display_density_checked"] = false
	body, _ := json.Marshal(settings)
	r := httptest.NewRequest(http.MethodPost, s.origin+"/api/settings", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p, ok := s.proposed(rec, r)
	if !ok {
		t.Fatal("settings proposal rejected", rec.Code, rec.Body.String())
	}
	if !p.Preferences.BalancedDisplay || !p.Preferences.DisplayDensityChecked {
		t.Fatalf("settings save lost the user's choice or the record: %+v", p.Preferences)
	}
	s.preferences = p.Preferences

	// The user turned it back on: later reports leave it.
	if reply := report("2"); reply["changed"] != false || reply["balanced_display"] != true {
		t.Fatalf("user's choice overridden: %v", reply)
	}
	for _, bad := range []string{`{}`, `{"device_pixel_ratio":-1}`, `{"device_pixel_ratio":"2"}`} {
		if w := desktopRequestForTest(s, http.MethodPost, "/api/display-density", []byte(bad)); w.Code != http.StatusBadRequest {
			t.Fatalf("%s accepted: %d", bad, w.Code)
		}
	}
	if !saved().DisplayDensityChecked {
		t.Fatal("record missing from workbench.json")
	}
}
