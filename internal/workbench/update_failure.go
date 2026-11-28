package workbench

import (
	"fmt"

	"github.com/sylvinus/agent-vm/internal/usererror"
	"github.com/sylvinus/agent-vm/internal/version"
)

// The update's failures while the old version stops its environment for the
// new one (notes/failure-text-audit-2026-11-28.md U1). The desktop app shows
// the joined text and splits it back (ui/src/failure-text.mjs); the update
// stopped before installing, so this version is still the one in use.

func updateStopFailure(reason, next string) error {
	return usererror.Fail("更新", reason+"，仍在用 "+version.Version, next, nil).WithLevel(usererror.LevelWarn)
}

var (
	errUpdateLocked = usererror.Notice("更新需要先解锁", "原因是 Cage lite 已锁定", "解锁后重试更新", nil)
)

func errUpdateStopRefused() error {
	return updateStopFailure("原因是工作台没有接受停止环境", "重试更新")
}

func errUpdateStopChanged() error {
	return updateStopFailure("原因是停止环境时状态有变化", "确认现有任务后重试更新")
}

func errUpdateStopFailed() error {
	return updateStopFailure("原因是停止环境失败", "在工作台查看停止结果后重试更新")
}

func errUpdateStopTimeout() error {
	return updateStopFailure(fmt.Sprintf("原因是 %s内没有停止环境", usererror.Duration(stopHandoffWait)), "在工作台停止环境后重试更新")
}
