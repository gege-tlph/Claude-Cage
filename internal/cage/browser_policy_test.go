package cage

import (
	"strings"
	"testing"
)

// The browser runs with --disable-blink-features=AutomationControlled on
// purpose; a managed policy keeps Chromium from showing its "unsupported
// command-line flag" bar above the restored pages after every restart.
func TestGuestBrowserPolicySilencesFlagWarning(t *testing.T) {
	if !strings.Contains(guestServices, `'{"CommandLineFlagSecurityWarningsEnabled":false}' | sudo tee "$cage_browser_policy_dir/cage-command-line-flags.json"`) {
		t.Fatal("guest services no longer write the command-line flag policy")
	}
	if !strings.Contains(browserAgent, `"--disable-blink-features=AutomationControlled"`) {
		t.Skip("the flag is gone; the policy may go too")
	}
}
