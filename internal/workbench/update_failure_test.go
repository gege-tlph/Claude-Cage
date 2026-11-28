package workbench

import (
	"testing"

	"github.com/sylvinus/agent-vm/internal/usererror"
	"github.com/sylvinus/agent-vm/internal/version"
)

// Stopping the environment for an update fails in the three-part wording the
// desktop app splits back; the version in use stays (U1).
func TestUpdateStopFailures(t *testing.T) {
	f, _ := usererror.AsFailure(errUpdateStopTimeout())
	if f.Error() != "更新失败，原因是 4 分钟内没有停止环境，仍在用 "+version.Version+"。解决建议：在工作台停止环境后重试更新。" || f.Level != usererror.LevelWarn {
		t.Fatalf("%q %s", f.Error(), f.Level)
	}
	if errUpdateLocked.Level != usererror.LevelInfo || errUpdateLocked.Error() != "更新需要先解锁，原因是 Cage lite 已锁定。下一步：解锁后重试更新。" {
		t.Fatalf("%q", errUpdateLocked.Error())
	}
}
