//go:build !linux

package main

import (
	"context"
	"errors"
	"os"

	"github.com/sylvinus/agent-vm/internal/appupdate"
)

// Cage lite installs updates from the app only: its Windows updater keeps the
// install, rollback and confirmation steps in one place. `cage update --check`
// still reports a newer release; help and the version gate say where to
// install it.
const (
	updateHelp = `cage update [--check]   Check for a Cage lite release
                   Install it in Cage lite under Settings > About.
`
	updateHint = "update Cage lite in Settings > About"
)

func installHostUpdate(context.Context, string, appupdate.State, *os.File, *os.File, *os.File, bool) error {
	return errors.New("请在 Cage lite 应用的「设置 > 关于」中安装更新")
}
