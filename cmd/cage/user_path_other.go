//go:build !windows

package main

import "errors"

// On macOS and Linux the package manager or the user puts cage on PATH.
func editUserPath(add bool, dir string) error {
	return errors.New("cage user-path is only used by the Windows installer")
}
