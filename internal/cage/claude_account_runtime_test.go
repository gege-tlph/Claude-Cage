package cage

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
)

// #605 moved claude-account to /usr/local/libexec while client_runtime.py
// stayed in /usr/local/bin. Laid out as the start script installs them, the
// account helper must find and load the runtime.
func TestClaudeAccountFindsTheInstalledRuntime(t *testing.T) {
	python := ""
	for _, name := range []string{"python3", "python"} {
		if path, err := exec.LookPath(name); err == nil {
			python = path
			break
		}
	}
	if python == "" {
		t.Skip("python unavailable")
	}
	script := DefaultConfig().StartScript()
	installed := func(pattern string) string {
		t.Helper()
		m := regexp.MustCompile(`sudo tee (/usr/local/[a-z]+/` + pattern + `) >/dev/null`).FindStringSubmatch(script)
		if m == nil {
			t.Fatalf("the start script does not install %s", pattern)
		}
		return m[1]
	}
	root := t.TempDir()
	account := filepath.Join(root, filepath.FromSlash(installed(`claude-account`)))
	runtime := filepath.Join(root, filepath.FromSlash(installed(`client_runtime\.py`)))
	for path, content := range map[string]string{
		account: claudeAccountHelper,
		// Loading the runtime is enough: it stops the helper with its own code.
		runtime: "raise SystemExit(42)\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	out, err := exec.Command(python, "-I", account).CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 42 {
		t.Fatalf("claude-account did not load %s: %v\n%s", runtime, err, out)
	}
}
