package main

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// userEnvironmentKey is where Windows keeps the user's own variables. Tests
// point it at a scratch key.
var userEnvironmentKey = `Environment`

func editUserPath(add bool, dir string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, userEnvironmentKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	// The value keeps its type: an expandable PATH (%USERPROFILE%\...) must
	// stay REG_EXPAND_SZ, or its entries would stop resolving.
	path, kind, err := key.GetStringValue("Path")
	if errors.Is(err, registry.ErrNotExist) {
		path, kind, err = "", registry.EXPAND_SZ, nil
	}
	if err != nil {
		return err
	}
	expand := func(s string) string {
		if expanded, err := registry.ExpandString(s); err == nil {
			return expanded
		}
		return s
	}
	var changed bool
	if add {
		path, changed = withPathEntry(path, dir, expand)
	} else {
		path, changed = withoutPathEntry(path, dir, expand)
	}
	if !changed {
		return nil
	}
	if path == "" {
		err = key.DeleteValue("Path")
	} else if kind == registry.SZ {
		err = key.SetStringValue("Path", path)
	} else {
		err = key.SetExpandStringValue("Path", path)
	}
	if err != nil {
		return err
	}
	announceEnvironmentChange()
	return nil
}

// announceEnvironmentChange tells Explorer to reload the environment, so
// terminals opened afterwards see the new PATH without signing out.
func announceEnvironmentChange() {
	const wmSettingChange = 0x001A
	const smtoAbortIfHung = 0x0002
	name, err := windows.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	var result uintptr
	sendMessageTimeout := windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")
	// HWND_BROADCAST; a window that does not answer within 5 s is skipped.
	_, _, _ = sendMessageTimeout.Call(uintptr(0xffff), wmSettingChange, 0, uintptr(unsafe.Pointer(name)), smtoAbortIfHung, 5000, uintptr(unsafe.Pointer(&result)))
}
