package host

import (
	"path/filepath"

	"github.com/sylvinus/agent-vm/internal/paths"
)

// HypervisorReady reports whether Windows Hypervisor Platform can run a
// virtual machine: nil elsewhere, or the HypervisorPlatformMissing /
// HypervisorNotReady error. Read-only, for checking the computer after a
// failed start.
func HypervisorReady() error { return windowsHypervisorReady() }

// Volume kinds that cannot hold an environment: no private permissions, or
// not a local disk.
const (
	VolumeNetwork = "network"
)

// UnsupportedVolume names the file system of dir's volume when it cannot hold
// an environment (exFAT, FAT32, or a network drive as VolumeNetwork), and ""
// when it can or cannot be read. dir is read where it really is: storage
// moved to another disk is reached through a junction in the profile.
func UnsupportedVolume(dir string) string { return unsupportedVolume(physicalDir(dir)) }

// physicalDir is dir, or its nearest parent that exists, with links and
// junctions resolved; dir itself when none resolves.
func physicalDir(dir string) string {
	for current := dir; ; {
		if real, err := paths.Resolve(current); err == nil {
			return real
		}
		parent := filepath.Dir(current)
		if parent == current {
			return dir
		}
		current = parent
	}
}
