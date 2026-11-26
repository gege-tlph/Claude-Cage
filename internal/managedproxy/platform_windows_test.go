package managedproxy

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// A PID reused after a reboot must neither be terminated nor block startup.
func TestStopWindowsCoreForgetsReusedPID(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	core := filepath.Join(filepath.Dir(exe), "mihomo.exe")
	if err := os.WriteFile(core, []byte("placeholder"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(core) })
	for name, pid := range map[string]uint32{"same user": uint32(os.Getpid()), "system": 4} {
		t.Run(name, func(t *testing.T) {
			profile := t.TempDir()
			b, _ := json.Marshal(windowsCoreRecord{PID: pid, Executable: core, Created: 1})
			if err := Write(windowsCoreRecordPath(profile), b); err != nil {
				t.Fatal(err)
			}
			if err := stopWindowsCore(profile); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(windowsCoreRecordPath(profile)); !os.IsNotExist(err) {
				t.Fatal("stale core record was kept")
			}
		})
	}
}

// A core recorded by an install in another directory blocked every later
// start ("not from this install directory") even after it had exited. It is
// this profile's core: an exited one is forgotten, a running one is stopped.
func TestStopWindowsCoreFromAnotherInstallDirectory(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(filepath.Dir(exe), "mihomo.exe")
	if err := os.WriteFile(current, []byte("placeholder"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(current) })
	old := filepath.Join(t.TempDir(), "old-install", "mihomo.exe")
	if err := os.MkdirAll(filepath.Dir(old), 0700); err != nil {
		t.Fatal(err)
	}
	ping, err := os.ReadFile(filepath.Join(os.Getenv("SystemRoot"), "System32", "PING.EXE"))
	if err != nil {
		t.Skip(err)
	}
	if err := os.WriteFile(old, ping, 0700); err != nil {
		t.Fatal(err)
	}

	t.Run("exited", func(t *testing.T) {
		profile := t.TempDir()
		b, _ := json.Marshal(windowsCoreRecord{PID: uint32(os.Getpid()), Executable: old, Created: 1})
		if err := Write(windowsCoreRecordPath(profile), b); err != nil {
			t.Fatal(err)
		}
		if err := stopWindowsCore(profile); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(windowsCoreRecordPath(profile)); !os.IsNotExist(err) {
			t.Fatal("stale core record was kept")
		}
	})

	t.Run("running", func(t *testing.T) {
		profile := t.TempDir()
		cmd := exec.Command(old, "-n", "60", "127.0.0.1")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		t.Cleanup(func() { _ = cmd.Process.Kill(); <-done })
		handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(cmd.Process.Pid))
		if err != nil {
			t.Fatal(err)
		}
		image, created, err := identifyCore(handle)
		windows.CloseHandle(handle)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(windowsCoreRecord{PID: uint32(cmd.Process.Pid), Executable: image, Created: created})
		if err := Write(windowsCoreRecordPath(profile), b); err != nil {
			t.Fatal(err)
		}
		if err := stopWindowsCore(profile); err != nil {
			t.Fatal(err)
		}
		select {
		case <-done:
			done <- nil
		case <-time.After(5 * time.Second):
			t.Fatal("the recorded core was not stopped")
		}
		if _, err := os.Lstat(windowsCoreRecordPath(profile)); !os.IsNotExist(err) {
			t.Fatal("core record was kept")
		}
	})
}

// A record naming anything but the core is refused, never terminated.
func TestStopWindowsCoreRefusesOtherExecutables(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	core := filepath.Join(filepath.Dir(exe), "mihomo.exe")
	if err := os.WriteFile(core, []byte("placeholder"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(core) })
	for _, executable := range []string{exe, "mihomo.exe"} {
		profile := t.TempDir()
		b, _ := json.Marshal(windowsCoreRecord{PID: uint32(os.Getpid()), Executable: executable, Created: 1})
		if err := Write(windowsCoreRecordPath(profile), b); err != nil {
			t.Fatal(err)
		}
		if err := stopWindowsCore(profile); err == nil {
			t.Fatalf("record naming %s was accepted", executable)
		}
		if _, err := os.Lstat(windowsCoreRecordPath(profile)); err != nil {
			t.Fatal("refused record was removed")
		}
	}
}
