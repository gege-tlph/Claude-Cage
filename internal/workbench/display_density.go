package workbench

import (
	"encoding/json"
	"math"
	"net/http"
	"path/filepath"
)

// displayDensity turns "balanced display" off once for a high-density screen.
// Balanced display starts the guest at 1.5 instead of 2 display scale, so on a
// screen with a device pixel ratio above 1 the framebuffer holds 3/4 of the
// screen's pixels and the remote display is upscaled and blurry. Earlier
// installs saved the default, so a saved true does not show a choice; turning
// it back on afterwards is kept. It applies from the next environment start,
// when the guest display is configured.
func (s *Server) displayDensity(w http.ResponseWriter, r *http.Request) {
	var input struct {
		DevicePixelRatio *float64 `json:"device_pixel_ratio"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if input.DevicePixelRatio == nil || *input.DevicePixelRatio <= 0 || math.IsInf(*input.DevicePixelRatio, 0) || math.IsNaN(*input.DevicePixelRatio) {
		http.Error(w, "屏幕像素比无效", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	next := s.preferences
	changed := *input.DevicePixelRatio > 1 && !next.DisplayDensityChecked
	if changed {
		next.DisplayDensityChecked = true
		next.BalancedDisplay = false
	}
	s.mu.Unlock()
	if changed {
		if err := writePrivate(filepath.Join(s.profile, "workbench.json"), next); err != nil {
			requestError(w, err)
			return
		}
		s.mu.Lock()
		s.preferences.DisplayDensityChecked = true
		s.preferences.BalancedDisplay = false
		s.mu.Unlock()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"changed": changed, "balanced_display": next.BalancedDisplay})
}
