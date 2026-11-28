package workbench

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/sylvinus/agent-vm/internal/usererror"
)

// Opening Claude Desktop reads what the guest's start script printed
// (failure-text-audit F1), instead of 「程序提前结束（退出码 1）」.
func TestDesktopStartFailureReadsTheScript(t *testing.T) {
	cause := errors.New("exit status 1")
	for _, tc := range []struct {
		output, lead, reason, level string
		tools                       []string
	}{
		{"Claude Desktop 未能启动或密钥环未能解锁；已有登录数据保持不变。", "", "原因是 Claude Desktop 启动后退出，登录状态没有解锁（登录数据未改动）", "error", []string{"open-desktop", "diagnostics"}},
		{"图形会话未能启动。", "", "原因是环境的桌面会话没有启动", "error", []string{"restart", "diagnostics"}},
		{"等待凭据解锁通道超时。", "", "原因是 15 秒内没有等到登录解锁", "error", []string{"open-desktop", "diagnostics"}},
		{"Claude Desktop 启动超时。", "", "原因是 Claude Desktop 30 秒内没有完成启动", "error", []string{"open-desktop", "log"}},
		{"请先保存并关闭现有 Claude Desktop，再使用 Cage 账户重新打开。", "Claude Desktop 需要重新打开", "原因是现在的窗口不是用 Cage 账户打开的", "info", []string{"reopen-desktop"}},
		{"请先安装 Claude Desktop。", "打开 Claude Desktop 前需要先安装", "", "info", []string{"install-desktop"}},
		{"something else\nError: x failed", "", "原因未识别", "error", []string{"open-desktop", "log", "diagnostics"}},
	} {
		f := desktopStartFailure(cause, tc.output)
		if f.Lead != tc.lead || f.Reason != tc.reason || f.Level != tc.level || !reflect.DeepEqual(f.Tools, tc.tools) {
			t.Errorf("%q: %+v", tc.output, f)
		}
	}
	if f := desktopStartFailure(errors.Join(context.DeadlineExceeded, cause), ""); f.Reason != "原因是打开 Claude Desktop 超时（55 秒）" {
		t.Fatalf("%+v", f)
	}
}

// Installing Claude Desktop names the step and offers its own retry (F2).
func TestDesktopInstallFailureRetriesItself(t *testing.T) {
	err := desktopInstallFailure(false, errors.New("exit status 1"), "curl: (28) Operation timed out after 30000 milliseconds\n")
	f, _ := usererror.AsFailure(err)
	if f.Reason != "原因是下载并安装 Claude Desktop 时连接超时" || f.Next != "换一个节点后重试" || !reflect.DeepEqual(f.Tools, []string{"settings:network", "install-desktop"}) {
		t.Fatalf("%+v", f)
	}
	err = desktopInstallFailure(true, errors.New("exit status 1"), "weird\nexit status 2")
	if f, _ := usererror.AsFailure(err); f.Reason != "卡在下载并更新 Claude Desktop，原因未识别" || f.Tools[0] != "update-desktop" {
		t.Fatalf("%+v", f)
	}
	if err := desktopInstallFailure(false, context.Canceled, ""); !errors.Is(err, context.Canceled) {
		t.Fatal("a cancel is a cancel")
	}
}

// File transfer: the user's steps are notices; a stopped transfer and HTTP
// errors are failures (F3–F5).
func TestFileTransferFailures(t *testing.T) {
	f, _ := usererror.AsFailure(fileServiceFailure(400, []byte("上传窗口已关闭，请重新选择文件")))
	if f.Error() != "传递文件失败，原因是上传窗口已关闭。下一步：在应用里重新点上传，再选择文件。" || f.Level != usererror.LevelInfo {
		t.Fatalf("%q %s", f.Error(), f.Level)
	}
	f, _ = usererror.AsFailure(serviceHTTPFailure("打开网页", 503))
	if f.Error() != "打开网页失败，原因是环境里的服务没有响应（HTTP 503）。解决建议：重启环境。" || f.Tools[0] != "restart" {
		t.Fatalf("%q", f.Error())
	}
	if f := desktopFilesStopped("stderr: desktop file service state=failed\n"); f == nil || f.Reason != "原因是 Claude Desktop 的文件传递已停止" || f.Tools[0] != "reopen-desktop" {
		t.Fatalf("%+v", f)
	}
	if desktopFilesStopped("other") != nil {
		t.Fatal("matched other output")
	}
}

// A Latin name gets a space before 失败.
func TestLeadSpacing(t *testing.T) {
	if usererror.LeadOf("打开 Claude Desktop") != "打开 Claude Desktop 失败" || usererror.LeadOf("启动环境") != "启动环境失败" {
		t.Fatal(usererror.LeadOf("打开 Claude Desktop"))
	}
}
