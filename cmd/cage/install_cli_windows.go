//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// installTerminalCommand puts the installed app's resources folder on the
// user's PATH. The Cage lite installer already does this with
// `cage user-path add` (#861); this command repairs it by hand with the same
// PATH editing, so the value keeps its type and the other entries.
func installTerminalCommand() int {
	exe, err := os.Executable()
	if err != nil {
		return fail(err)
	}
	dir := filepath.Dir(exe)
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "Cage.exe")); err != nil || !strings.EqualFold(filepath.Base(dir), "resources") {
		return fail(errors.New("请使用完整 Cage lite 应用 resources 目录中的 cage.exe 安装终端命令"))
	}
	if err := editUserPath(true, dir); err != nil {
		return fail(err)
	}
	fmt.Println("已注册 Cage lite 终端命令，重新打开终端即可使用。")
	return 0
}
