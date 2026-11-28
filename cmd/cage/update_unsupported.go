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
// still reports a newer release.
func installHostUpdate(context.Context, string, appupdate.State, *os.File, *os.File, *os.File, bool) error {
	return errors.New("请在 Cage lite 应用的「设置 > 关于」中安装更新")
}
