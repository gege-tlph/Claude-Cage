package workbench

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sylvinus/agent-vm/internal/diagnostics"
)

// The advanced view asks for the run's events with ?level=; the simple view's
// reads stay without them.
func TestJobLogAddsEventsOnlyForALevel(t *testing.T) {
	previous := diagnostics.Default()
	store := diagnostics.New(t.TempDir())
	diagnostics.SetDefault(store)
	t.Cleanup(func() { diagnostics.SetDefault(previous) })
	if err := store.Record("warn", "proxy", "job-log-level-sentinel"); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	for _, tc := range []struct {
		query string
		want  bool
	}{{"", false}, {"?level=debug", true}, {"?level=warn", true}, {"?level=error", false}} {
		w := httptest.NewRecorder()
		s.jobLog(w, httptest.NewRequest("GET", "/api/jobs/log"+tc.query, nil))
		body := w.Body.String()
		if got := strings.Contains(body, "job-log-level-sentinel"); got != tc.want {
			t.Errorf("%q: sentinel %v, want %v: %s", tc.query, got, tc.want, body)
		}
		if tc.query == "" && strings.Contains(body, `"events"`) {
			t.Errorf("events without a level: %s", body)
		}
	}
}
