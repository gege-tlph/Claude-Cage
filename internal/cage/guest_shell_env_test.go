package cage

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Programs that read a login shell's environment must receive only standard
// settings from the guest profile, whatever the service environment holds.
func TestGuestShellProfileExportsOnlyStandardSettings(t *testing.T) {
	_, profile, ok := strings.Cut(guestServices, "sudo tee /etc/profile.d/workbench.sh >/dev/null <<'SCRIPT'\n")
	if !ok {
		t.Fatal("missing shell profile")
	}
	profile, _, _ = strings.Cut(profile, "\nSCRIPT\n")
	dir := t.TempDir()
	environment := filepath.ToSlash(filepath.Join(dir, "environment"))
	// Extra entries a service environment may hold, without a final newline.
	if err := os.WriteFile(environment, []byte("TZ=Asia/Shanghai\nLANG=zh_CN.UTF-8\nLC_ALL=zh_CN.UTF-8\nBROWSER_LANG=zh-CN\nBROWSER=/usr/local/bin/xdg-open"), 0600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "profile.sh")
	if err := os.WriteFile(file, []byte(strings.ReplaceAll(profile, "/etc/workbench/environment", environment)), 0600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		env     []string
		browser string
		pulse   string
		tz      string
	}{
		// What a process started before the update hands down.
		{"earlier values", []string{"CAGE_LANGUAGE=en-US", "BROWSER=/usr/local/bin/cage-open", "PULSE_SERVER=unix:/run/cage-audio/native", "TZ=Europe/London"}, "/usr/local/bin/xdg-open", "", "Asia/Shanghai"},
		{"empty", nil, "/usr/local/bin/xdg-open", "", "Asia/Shanghai"},
		{"launcher values", []string{"BROWSER=/tmp/runtime-1/opener.py", "PULSE_SERVER=unix:/tmp/runtime-1/paths/audio"}, "/tmp/runtime-1/opener.py", "unix:/tmp/runtime-1/paths/audio", "Asia/Shanghai"},
		// An account command started through its proxy node keeps that
		// node's zone in the login shell it runs (#909).
		{"account zone", []string{"ACCOUNT_TZ=America/New_York", "TZ=America/New_York"}, "/usr/local/bin/xdg-open", "", "America/New_York"},
	}
	for _, shell := range []string{"sh", "bash", "zsh"} {
		if _, err := exec.LookPath(shell); err != nil {
			t.Logf("%s unavailable", shell)
			continue
		}
		for _, c := range cases {
			t.Run(shell+"/"+c.name, func(t *testing.T) {
				// Inherited values are exported by the command itself: a zsh
				// reads the system zshenv before the command runs.
				inherited := "export https_proxy=http://127.0.0.1:1"
				for _, value := range c.env {
					inherited += " '" + value + "'"
				}
				cmd := exec.Command(shell, "-c", inherited+`; . "$1"; env`, shell, filepath.ToSlash(file))
				cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + dir}
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("%v: %s", err, out)
				}
				got := map[string]string{}
				for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
					key, value, _ := strings.Cut(strings.TrimRight(line, "\r"), "=")
					got[key] = value
				}
				want := map[string]string{"TZ": c.tz, "LANG": "zh_CN.UTF-8", "LC_ALL": "zh_CN.UTF-8", "BROWSER": c.browser, "PULSE_SERVER": c.pulse, "BROWSER_LANG": ""}
				for key, value := range want {
					if got[key] != value {
						t.Errorf("%s=%q, want %q", key, got[key], value)
					}
				}
				// The account name and working directories are not set by the profile.
				var names []string
				for key, value := range got {
					if key == "HOME" || key == "PWD" || key == "OLDPWD" || key == "LOGNAME" || key == "USER" {
						continue
					}
					if strings.HasPrefix(strings.ToUpper(key), "CAGE") || strings.HasPrefix(key, "env_") || strings.Contains(strings.ToLower(value), "cage") || strings.Contains(strings.ToLower(key), "proxy") {
						names = append(names, key)
					}
				}
				sort.Strings(names)
				if len(names) > 0 {
					t.Errorf("shell environment keeps %v: %s", names, out)
				}
			})
		}
	}
}
