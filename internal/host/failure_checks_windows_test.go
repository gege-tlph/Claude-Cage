package host

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The volume of an environment moved to another disk is read through the
// profile's junction (state -> the other disk), not where the junction is.
func TestPhysicalDirFollowsAJunction(t *testing.T) {
	root := t.TempDir()
	moved := filepath.Join(root, "moved")
	if err := os.Mkdir(moved, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "profile-state")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, moved).CombinedOutput(); err != nil {
		t.Skipf("cannot create a junction here: %v %s", err, out)
	}
	want, err := filepath.EvalSymlinks(moved)
	if err != nil {
		t.Fatal(err)
	}
	// A Lima home not made yet is read from its nearest parent.
	got := physicalDir(filepath.Join(link, "lima", "not-yet"))
	if !strings.EqualFold(got, want) {
		t.Fatalf("physicalDir = %q, want %q", got, want)
	}
}
