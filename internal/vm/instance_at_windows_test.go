//go:build windows

package vm

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A moved storage is reached through the profile's state junction. Lima
// resolving that home fails on Windows, so the in-process inspection takes it
// as logical, as cmd/cage does, and still finds the VM (#508).
func TestInstanceAtFindsVMBehindStorageJunction(t *testing.T) {
	root := t.TempDir()
	moved := filepath.Join(root, "storage", "state")
	if err := os.MkdirAll(filepath.Join(moved, "lima", "work"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moved, "lima", "work", "lima.yaml"), []byte("vmType: qemu\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(root, "profile")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(profile, "state")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, moved).CombinedOutput(); err != nil {
		t.Skipf("cannot create a junction here: %v %s", err, out)
	}
	home := filepath.Join(link, "lima")
	if _, err := InstanceIn(context.Background(), home, "work"); err != nil {
		t.Logf("resolved home: %v", err)
	}
	t.Setenv(logicalLimaHomeEnv, "")
	os.Unsetenv(logicalLimaHomeEnv)
	inst, err := InstanceAt(context.Background(), home, true, "work")
	if errors.Is(err, ErrNotFound) || err != nil {
		t.Fatalf("VM behind the junction not found: %v", err)
	}
	if inst.Dir != filepath.Join(home, "work") {
		t.Fatalf("Dir = %q, want the logical path", inst.Dir)
	}
	if v, set := os.LookupEnv(logicalLimaHomeEnv); set {
		t.Fatalf("%s left set to %q", logicalLimaHomeEnv, v)
	}
}
