package offlinepack

import "strings"

// InstallEnv is set, in the setup process, while the base is built from the
// data, for the VM's creation to read.
const InstallEnv = "CAGE_OFFLINE_INSTALL"

// FinishedMarker is written in the guest at the end of every Cage setup
// (guest-finish.sh).
const FinishedMarker = "/etc/workbench/offline-install-finished"

// AptConfig is the APT configuration of an installation from the data:
// at first boot one without sources, after the copy one that reads the
// copied repositories (guest-prepare.sh), removed at the end.
const AptConfig = "/etc/apt/apt.conf.d/00cage-offline"

// BootScript wraps the Debian preparation that runs at every boot (the APT
// bootstrap and the mirror probe), a dependency script Lima runs just before
// installing its own dependencies, for a base built from the data. Until the
// installation has finished in that disk, both would only reach the network:
// the preparation is skipped (the setup script runs it against the copied
// repositories), and APT gets no sources, so Lima's step fails at once
// instead of trying the network (guest-prepare.sh runs it again). The
// environments cloned from the finished base run everything as usual.
func BootScript(script string) string {
	body, shebang := strings.CutPrefix(script, "#!/bin/sh\n")
	head := ""
	if shebang {
		head = "#!/bin/sh\n"
	}
	// The configuration guest-prepare.sh writes, over an empty source list
	// until the copy fills it.
	apt := GuestRoot + "/apt"
	return head + "if [ ! -e " + FinishedMarker + " ]; then\n" +
		"  echo 'Installing from the installation files: the APT bootstrap, the mirror probe and Lima'\"'\"'s dependencies run during setup.'\n" +
		"  if [ ! -e " + AptConfig + " ]; then\n" +
		"    install -d -m 0755 " + apt + "/sources.list.d\n" +
		"    : > " + apt + "/sources.list\n" +
		"    printf '%s\\n' 'Dir::Etc::sourcelist \"" + apt + "/sources.list\";' 'Dir::Etc::sourceparts \"" + apt + "/sources.list.d\";' 'Acquire::Languages \"none\";' > " + AptConfig + "\n" +
		"  fi\n" +
		"  exit 0\nfi\n" + body
}
