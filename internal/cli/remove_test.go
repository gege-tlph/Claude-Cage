package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// The leftovers of an interrupted VM creation hold the files Lima writes
// read-only. Where Windows refuses to delete a read-only file, the cleanup
// clears the attribute and succeeds on the second try, as `attrib -R` does
// by hand (#839).
func TestRemoveAllWritableClearsReadOnlyFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "base-building")
	if err := os.MkdirAll(filepath.Join(dir, "sock"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"cloud-config.yaml", "lima-version", filepath.Join("sock", "note")} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o444); err != nil {
			t.Fatal(err)
		}
	}
	// Like a volume that does not honour "ignore read-only": deletion fails
	// while any file under the directory is still read-only.
	calls := 0
	refusing := func(path string) error {
		calls++
		readOnly := false
		_ = filepath.WalkDir(path, func(_ string, entry fs.DirEntry, err error) error {
			if err == nil && !entry.IsDir() {
				if info, err := entry.Info(); err == nil && info.Mode().Perm()&0o200 == 0 {
					readOnly = true
				}
			}
			return nil
		})
		if readOnly {
			return &fs.PathError{Op: "unlinkat", Path: path, Err: fs.ErrPermission}
		}
		return os.RemoveAll(path)
	}
	if err := removeAllWritable(dir, refusing); err != nil {
		t.Fatalf("removeAllWritable: %v", err)
	}
	if calls != 2 {
		t.Fatalf("remove called %d times, want 2", calls)
	}
	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("directory left behind: %v", err)
	}
}

func TestRemoveAllWritableReportsAPersistentFailure(t *testing.T) {
	dir := t.TempDir()
	denied := errors.New("Access is denied")
	calls := 0
	err := removeAllWritable(dir, func(string) error { calls++; return denied })
	if !errors.Is(err, denied) || calls != 2 {
		t.Fatalf("err=%v calls=%d, want the second failure reported after two tries", err, calls)
	}
	// One successful attempt needs no second.
	calls = 0
	if err := removeAllWritable(dir, func(string) error { calls++; return nil }); err != nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}
