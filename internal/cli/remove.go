package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// removeAll is os.RemoveAll for the leftovers of an interrupted VM creation.
// Lima writes cloud-config.yaml and lima-version read-only (0444). Go asks
// Windows to ignore that attribute when deleting, but on some volumes the
// deletion still fails with "Access is denied", and an interrupted setup could
// then never be retried (#839). On Windows a failed removal therefore makes
// every file under path writable, as `attrib -R` does, and tries once more.
func removeAll(path string) error {
	if runtime.GOOS != "windows" {
		return os.RemoveAll(path)
	}
	return removeAllWritable(path, os.RemoveAll)
}

// removeAllWritable calls remove, and after a failure clears the read-only
// bit of every file and directory under path and calls it once more. Links
// are left alone, so nothing outside path changes.
func removeAllWritable(path string, remove func(string) error) error {
	if err := remove(path); err == nil {
		return nil
	}
	_ = filepath.WalkDir(path, func(p string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() && !entry.IsDir() {
			return nil
		}
		if info, err := entry.Info(); err == nil && info.Mode().Perm()&0o200 == 0 {
			_ = os.Chmod(p, info.Mode().Perm()|0o200)
		}
		return nil
	})
	return remove(path)
}
