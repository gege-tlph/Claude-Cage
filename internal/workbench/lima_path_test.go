package workbench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #913: a profile so deep that Lima could not create base-building is
// refused before setup starts, with the reason the summary used to hide.
func TestStartRefusesALimaHomeTooDeepForSockets(t *testing.T) {
	// The path need not exist; t.TempDir() is itself too deep on Windows.
	root := filepath.Join(os.TempDir(), "p")
	if err := (&Server{profile: filepath.Join(root, "profile")}).requireLimaPathFits(); err != nil {
		t.Fatalf("short profile refused: %v", err)
	}
	deep := filepath.Join(root, strings.Repeat("deeper-configuration-folder-", 5), "profile")
	err := (&Server{profile: deep}).requireLimaPathFits()
	if err == nil || !strings.Contains(err.Error(), "路径太长") || !strings.HasPrefix(err.Error(), "启动环境失败：") {
		t.Fatalf("deep profile: %v", err)
	}
}
