package workbench

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sylvinus/agent-vm/internal/cage"
	"github.com/sylvinus/agent-vm/internal/vmname"
)

// #846: the base marker can outlive the Lima files it describes (deleted
// storage, a cleaned AppData). The environment is prepared only when the work
// instance exists, or the template a new instance is cloned from; otherwise
// the next start must run setup again instead of failing in `up`.
func TestEnvironmentPreparedNeedsLimaFiles(t *testing.T) {
	s := testServer(t)
	state := filepath.Join(s.profile, "state")
	lima := filepath.Join(state, "lima")
	marker := filepath.Join(state, ".agent-vm-base-version")
	write := func(path string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("1\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if s.environmentPrepared() {
		t.Fatal("empty profile reported prepared")
	}
	write(marker)
	if s.environmentPrepared() {
		t.Fatal("a marker without any Lima files reported prepared")
	}
	// Lima keeps an instance's directory; without lima.yaml it is no VM.
	if err := os.MkdirAll(filepath.Join(lima, vmname.Template), 0o700); err != nil {
		t.Fatal(err)
	}
	if s.environmentPrepared() {
		t.Fatal("a template directory without lima.yaml reported prepared")
	}
	write(filepath.Join(lima, vmname.Template, "lima.yaml"))
	if !s.environmentPrepared() {
		t.Fatal("marker and template should be prepared")
	}
	// An existing work VM has its own disks and runs without the template.
	if err := os.RemoveAll(filepath.Join(lima, vmname.Template)); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(lima, cage.InstanceName(s.profile, s.environmentDir()), "lima.yaml"))
	if !s.environmentPrepared() {
		t.Fatal("marker and work instance should be prepared")
	}
	// Setup never finished: the marker decides, as `cage up` requires it.
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if s.environmentPrepared() {
		t.Fatal("an instance without the base marker reported prepared")
	}
}
