package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// user-path adds the directory holding cage.exe to the user's PATH, so a
// terminal can run `cage` (#861). The Windows installer calls it after
// installing and the uninstaller before removing files:
//
//	cage user-path add|remove DIR
func userPathCommand(args []string) int {
	if len(args) != 2 || args[0] != "add" && args[0] != "remove" || args[1] == "" {
		fmt.Fprintln(os.Stderr, "Usage: cage user-path add|remove DIR")
		return 2
	}
	dir, err := filepath.Abs(args[1])
	if err != nil {
		return fail(err)
	}
	if err := editUserPath(args[0] == "add", dir); err != nil {
		return fail(err)
	}
	return 0
}

// samePathEntry compares one PATH entry with dir as Windows resolves it:
// without case, surrounding quotes or a trailing separator, and with
// %VARIABLES% expanded.
func samePathEntry(entry, dir string, expand func(string) string) bool {
	clean := func(s string) string {
		s = strings.Trim(strings.TrimSpace(s), `"`)
		if expand != nil {
			s = expand(s)
		}
		return strings.ToLower(strings.TrimRight(s, `\/`))
	}
	return entry != "" && clean(entry) == clean(dir)
}

// withPathEntry appends dir unless PATH already lists it; other entries and
// their order stay exactly as they were.
func withPathEntry(path, dir string, expand func(string) string) (string, bool) {
	for _, entry := range strings.Split(path, ";") {
		if samePathEntry(entry, dir, expand) {
			return path, false
		}
	}
	if path == "" || strings.HasSuffix(path, ";") {
		return path + dir, true
	}
	return path + ";" + dir, true
}

// withoutPathEntry removes every entry that names dir.
func withoutPathEntry(path, dir string, expand func(string) string) (string, bool) {
	entries := strings.Split(path, ";")
	kept := entries[:0]
	for _, entry := range entries {
		if !samePathEntry(entry, dir, expand) {
			kept = append(kept, entry)
		}
	}
	if len(kept) == len(entries) {
		return path, false
	}
	return strings.Join(kept, ";"), true
}
