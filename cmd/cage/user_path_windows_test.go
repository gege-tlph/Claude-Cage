package main

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// The real HKCU\Environment stays untouched: the test edits a scratch key.
func TestUserPathRegistryKeepsTypeAndOtherEntries(t *testing.T) {
	scratch := `Software\CageLiteTest\` + strings.ReplaceAll(t.Name(), "/", "_")
	old := userEnvironmentKey
	userEnvironmentKey = scratch
	t.Cleanup(func() {
		userEnvironmentKey = old
		_ = registry.DeleteKey(registry.CURRENT_USER, scratch)
		_ = registry.DeleteKey(registry.CURRENT_USER, `Software\CageLiteTest`)
	})
	key, _, err := registry.CreateKey(registry.CURRENT_USER, scratch, registry.ALL_ACCESS)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Close()
	long := strings.Repeat(`C:\a-long-entry;`, 700) + `%USERPROFILE%\bin`
	if err := key.SetExpandStringValue("Path", long); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for range 2 {
		if err := editUserPath(true, dir); err != nil {
			t.Fatal(err)
		}
	}
	got, kind, err := key.GetStringValue("Path")
	if err != nil || kind != registry.EXPAND_SZ || got != long+";"+dir {
		t.Fatalf("after add: type %d, %d bytes, suffix %q, %v", kind, len(got), got[max(0, len(got)-80):], err)
	}
	if err := editUserPath(false, dir); err != nil {
		t.Fatal(err)
	}
	if got, kind, err = key.GetStringValue("Path"); err != nil || kind != registry.EXPAND_SZ || got != long {
		t.Fatalf("after remove: type %d, %d bytes, %v", kind, len(got), err)
	}
	// A plain REG_SZ value stays plain; a PATH that held only this entry goes.
	if err := key.SetStringValue("Path", ""); err != nil {
		t.Fatal(err)
	}
	if err := editUserPath(true, dir); err != nil {
		t.Fatal(err)
	}
	if got, kind, _ = key.GetStringValue("Path"); kind != registry.SZ || got != dir {
		t.Fatalf("plain value: type %d %q", kind, got)
	}
	if err := editUserPath(false, dir); err != nil {
		t.Fatal(err)
	}
	if _, _, err = key.GetStringValue("Path"); err != registry.ErrNotExist {
		t.Fatalf("empty PATH kept: %v", err)
	}
}
