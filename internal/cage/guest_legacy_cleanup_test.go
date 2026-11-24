package cage

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sylvinus/agent-vm/internal/guestprofile"
)

// The cleanup runs inside the component update under `set -euo pipefail`. An
// empty last line of a list must not fail the loop over it: the update would
// stop there on every start, leaving the environment without its manifest.
func TestLegacyCleanupFinishesUnderErrexit(t *testing.T) {
	shell, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash unavailable")
	}
	for _, c := range []struct {
		name   string
		legacy string // exit status of the check for pre-update processes
		want   []string
	}{
		{"no process from before the update", "1", []string{
			"rm -f -- /usr/local/bin/cage-open",
			"rm -f -- /usr/local/lib/cage-browser-agent.mjs",
			"rm -rf --one-file-system -- /usr/local/lib/cage-phone-desktop",
			"rm -rf --one-file-system /home/cage-browser/.cache/cage-display /home/cage-browser/.cache/cage-input /home/cage/.cache/cage-input",
			"rm -rf --one-file-system /etc/systemd/journald@cage.conf.d",
		}},
		// The former programs stay for the processes that still use them.
		{"processes from before the update", "0", []string{"systemctl daemon-reload"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			log := filepath.ToSlash(filepath.Join(dir, "sudo.log"))
			// Commands are recorded, never run: the paths are the guest's.
			stubs := `sudo() {
  if [ "$1" = python3 ]; then cat >/dev/null; return ` + c.legacy + `; fi
  if [ "$1" = tee ]; then cat >/dev/null; fi
  printf '%s\n' "$*" >> '` + log + `'
}
systemctl() { return 3; }
getent() { return 2; }
`
			cmd := exec.Command(shell, "-c", "set -euo pipefail\n"+stubs+guestLegacyCleanup+"\necho cleanup-finished")
			out, err := cmd.CombinedOutput()
			if err != nil || !strings.Contains(string(out), "cleanup-finished") {
				t.Fatalf("cleanup stopped: %v\n%s", err, out)
			}
			recorded, _ := os.ReadFile(filepath.Join(dir, "sudo.log"))
			for _, want := range c.want {
				if !strings.Contains(string(recorded), want+"\n") {
					t.Errorf("missing %q in:\n%s", want, recorded)
				}
			}
			if c.legacy == "0" && strings.Contains(string(recorded), "rm -f -- /usr/local/bin/") {
				t.Errorf("removed a former program still in use:\n%s", recorded)
			}
		})
	}
}

// With a guest profile, the former caches are those of the profile's
// accounts: the whole start script, cleanup included, is rendered for them.
func TestLegacyCleanupUsesProfileHomes(t *testing.T) {
	p, err := guestprofile.New(guestprofile.Options{Username: "alex", BrowserUsername: "web"})
	if err != nil {
		t.Fatal(err)
	}
	c := DefaultConfig()
	c.GuestProfile = p
	script := c.StartScript()
	want := "sudo rm -rf --one-file-system " + p.BrowserHomePath() + "/.cache/cage-display " + p.BrowserHomePath() + "/.cache/cage-input " + p.WorkHomePath() + "/.cache/cage-input"
	if !strings.Contains(script, want) {
		t.Fatalf("cleanup does not name the profile homes; want %q", want)
	}
}
