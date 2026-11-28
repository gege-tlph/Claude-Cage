package host

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func unsupportedVolume(dir string) string {
	volume := filepath.VolumeName(dir)
	if volume == "" {
		return ""
	}
	root := volume + `\`
	path, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return ""
	}
	if windows.GetDriveType(path) == windows.DRIVE_REMOTE || strings.HasPrefix(volume, `\\`) {
		return VolumeNetwork
	}
	name := make([]uint16, windows.MAX_PATH+1)
	if err := windows.GetVolumeInformation(path, nil, 0, nil, nil, nil, &name[0], uint32(len(name))); err != nil {
		return ""
	}
	switch fs := windows.UTF16ToString(name); strings.ToUpper(fs) {
	case "EXFAT", "FAT32", "FAT":
		return fs
	}
	return ""
}
