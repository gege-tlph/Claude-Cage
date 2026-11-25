package guestos

import (
	"context"
	"io"
	"runtime"

	"github.com/sylvinus/agent-vm/internal/host"
	"golang.org/x/sys/windows"
)

// A Windows host runs the shared Debian workbench through WHPX and QEMU.
func Inspect(_ context.Context) Inspection {
	supported := host.WindowsBuildSupported(windows.RtlGetVersion().BuildNumber, host.WindowsEdition())
	return windowsInspection(runtime.GOARCH, supported, func(w io.Writer) bool {
		return host.WindowsPrereqs(io.Discard, w)
	})
}
