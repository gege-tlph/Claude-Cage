package host

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
// when it can or cannot be read.
func UnsupportedVolume(dir string) string { return unsupportedVolume(dir) }
