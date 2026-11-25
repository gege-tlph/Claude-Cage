package guestos

import (
	"fmt"
	"io"
	"testing"
)

func TestWindowsInspectionReportsDebianWorkbench(t *testing.T) {
	pass := func(io.Writer) bool { return true }
	fail := func(w io.Writer) bool { fmt.Fprintln(w, "请启用 Windows Hypervisor Platform"); return false }
	unused := func(io.Writer) bool { t.Fatal("host preflight must not run on an unsupported host"); return false }
	if r := windowsInspection("amd64", true, pass); r.Guest != Debian13 || r.WorkbenchSupport != "supported" || !r.HostCheckPassed {
		t.Fatalf("supported build ready: %+v", r)
	}
	if r := windowsInspection("amd64", true, fail); r.Guest != Debian13 || r.WorkbenchSupport != "supported" || r.HostCheckPassed || r.Detail != "请启用 Windows Hypervisor Platform" {
		t.Fatalf("missing hypervisor: %+v", r)
	}
	if r := windowsInspection("amd64", false, unused); r.Guest != Debian13 || r.WorkbenchSupport != "unsupported-host" || r.HostCheckPassed {
		t.Fatalf("unsupported build: %+v", r)
	}
	if r := windowsInspection("arm64", true, unused); r.Guest != Debian13 || r.WorkbenchSupport != "unsupported-host" {
		t.Fatalf("Windows on ARM: %+v", r)
	}
}
