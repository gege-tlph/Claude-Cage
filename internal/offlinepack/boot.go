package offlinepack

import "strings"

// InstallEnv is set, in the setup process, while the base is built from the
// data, for the VM's creation to read.
const InstallEnv = "CAGE_OFFLINE_INSTALL"

// FinishedMarker is written in the guest at the end of every Cage setup
// (guest-finish.sh).
const FinishedMarker = "/etc/workbench/offline-install-finished"

// BootScript wraps the Debian preparation that runs at every boot (the APT
// bootstrap and the mirror probe) for a base built from the data: until the
// installation has finished in that disk, it would only reach the network;
// the setup script runs it again against the copied repositories. The
// environments cloned from the finished base run it as usual.
func BootScript(script string) string {
	body, shebang := strings.CutPrefix(script, "#!/bin/sh\n")
	head := ""
	if shebang {
		head = "#!/bin/sh\n"
	}
	return head + "if [ ! -e " + FinishedMarker + " ]; then\n" +
		"  echo 'Installing from the installation files: the APT bootstrap and the mirror probe run during setup.'\n" +
		"  exit 0\nfi\n" + body
}
