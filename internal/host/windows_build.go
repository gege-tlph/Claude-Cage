package host

// Windows 10 22H2 (19045) is the oldest general release whose Hypervisor
// Platform runs the bundled QEMU. Windows 10 Enterprise LTSC 2021 and IoT
// Enterprise LTSC 2021 share build 19044 with 21H2 and run the same Debian
// workbench; unlike general 21H2, which is out of support, they are still
// serviced. cyx3705/Claude-Cage verified LTSC 2021 (19044.7807) end to end.
const (
	windowsMinimumBuild  = 19045
	windowsLTSC2021Build = 19044
)

// WindowsBuildSupported reports whether a Windows 10 or 11 build, with the
// EditionID from the registry, can run the Debian workbench. The installer
// (packaging/windows/Cage.nsi) and the desktop shell (Program.cs) repeat it.
func WindowsBuildSupported(build uint32, edition string) bool {
	if build >= windowsMinimumBuild {
		return true
	}
	if build != windowsLTSC2021Build {
		return false
	}
	switch edition {
	case "EnterpriseS", "EnterpriseSN", "IoTEnterpriseS":
		return true
	}
	return false
}
