package workbench

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/sylvinus/agent-vm/internal/usererror"
)

// Claude Desktop and file transfer failures (notes/failure-text-audit-
// 2026-11-28.md, group 4). The leads come from the job (打开 Claude Desktop,
// 安装 Claude Desktop…) or the request (传递文件, 打开网页…); the interface
// never shows 文件服务 or 密钥环.

// desktopStartFailure reads what the guest's start script printed
// (internal/cage claude-desktop-start.sh) before it failed (F1).
func desktopStartFailure(cause error, output string) *usererror.Failure {
	retry := []string{usererror.ToolOpenDesktop, usererror.ToolDiagnostics}
	switch {
	case strings.Contains(output, "请先安装 Claude Desktop"):
		return usererror.Notice("打开 Claude Desktop 前需要先安装", "", "安装 Claude Desktop", cause, usererror.ToolInstallDesktop)
	case strings.Contains(output, "请先保存并关闭现有 Claude Desktop"):
		return usererror.Notice("Claude Desktop 需要重新打开", "原因是现在的窗口不是用 Cage 账户打开的", "保存工作后重新打开", cause, usererror.ToolReopenDesktop)
	case strings.Contains(output, "图形会话未能启动"):
		return usererror.Because("原因是环境的桌面会话没有启动", "重启环境", cause, usererror.ToolRestart, usererror.ToolDiagnostics)
	case strings.Contains(output, "等待凭据解锁通道超时"):
		return usererror.Because("原因是 15 秒内没有等到登录解锁", "重试打开", cause, retry...)
	case strings.Contains(output, "密钥环未能解锁"):
		// 「登录数据未改动」: a user fears losing the login here.
		return usererror.Because("原因是 Claude Desktop 启动后退出，登录状态没有解锁（登录数据未改动）", "重试打开", cause, retry...)
	case strings.Contains(output, "Claude Desktop 启动超时"):
		return usererror.Because("原因是 Claude Desktop 30 秒内没有完成启动", "重试打开", cause, usererror.ToolOpenDesktop, usererror.ToolLog)
	case errors.Is(cause, context.DeadlineExceeded):
		return usererror.Because("原因是打开 Claude Desktop 超时（55 秒）", "重试打开", cause, usererror.ToolOpenDesktop, usererror.ToolLog)
	}
	return usererror.Unrecognized("", lastErrorLine(output), "重试打开", cause, usererror.ToolOpenDesktop, usererror.ToolLog, usererror.ToolDiagnostics)
}

// desktopInstallFailure: installing or updating Claude Desktop failed (F2),
// for the same reasons an install does; retry is the job's own button.
func desktopInstallFailure(update bool, cause error, output string) error {
	if errors.Is(cause, context.Canceled) {
		return cause
	}
	stage, tool := "下载并安装 Claude Desktop", usererror.ToolInstallDesktop
	if update {
		stage, tool = "下载并更新 Claude Desktop", usererror.ToolUpdateDesktop
	}
	stage = spaced(stage)
	f := aptFailure(stage, cause, output)
	if f == nil {
		f = downloadFailure(stage, cause, output)
	}
	if f == nil && containsAny(strings.ToLower(output), "no space left on device") {
		f = usererror.Because("原因是"+stage+"时环境磁盘空间不足", "在环境与恢复中调大磁盘后重试", cause, usererror.ToolSettingsProject)
	}
	if f == nil {
		f = usererror.Unrecognized("", lastErrorLine(output), "重试", cause, tool, usererror.ToolLog, usererror.ToolDiagnostics)
		f.StuckAt(stage)
	}
	for i, name := range f.Tools {
		if name == usererror.ToolStart {
			f.Tools[i] = tool
		}
	}
	f.Next = strings.ReplaceAll(f.Next, "继续安装", "重试")
	return f
}

// desktopFilesStopped: Claude Desktop's file transfer has quit (F5).
func desktopFilesStopped(output string) *usererror.Failure {
	if !strings.Contains(output, "desktop file service state=failed") {
		return nil
	}
	return usererror.Fail("传递文件", "原因是 Claude Desktop 的文件传递已停止", "重新打开 Claude Desktop", nil, usererror.ToolReopenDesktop)
}

// fileRefusals are what the guest's file transfer answers when the user has
// a step to take (F3): the reason, then the step.
var fileRefusals = map[string][2]string{
	"上传窗口已关闭，请重新选择文件":         {"原因是上传窗口已关闭", "在应用里重新点上传，再选择文件"},
	"原文件窗口已关闭":                {"原因是原文件窗口已关闭", "在接收文件的应用中重新打开文件窗口"},
	"请先完成已打开的文件窗口":            {"原因是另一个文件窗口还没处理", "先完成或关闭那个窗口，再选择文件"},
	"每个文件不能超过 2 GB":           {"原因是单个文件超过 2 GB", "缩小文件后重新上传"},
	"项目文件夹尚未连接，请重新打开项目":       {"原因是项目文件夹未连接", "在 Cage lite 中重新打开该项目"},
	"目标没有接收文件，请点击支持附件的输入框后重试": {"原因是目标没有接收文件", "点一下应用里支持附件的输入框，再选择文件"},
	"上传已取消":       {"原因是上传已取消", "需要时重新选择文件"},
	"当前窗口没有请求文件夹": {"原因是当前窗口只接收文件，不接收文件夹", "按窗口提示重新选择"},
	"文件夹正在交给应用":   {"原因是上一个文件夹还在传给应用", "等它完成后再选择"},
	"文件位置无法读取":    {"原因是接收文件的应用读不了这个位置", "确认项目文件夹已连接，再重新选择"},
	"文件请求过大":      {"原因是一次传递的文件太多", "减少一次传递的文件数量"},
	"请选择项目文件夹":    {"原因是当前操作需要一个已连接的项目文件夹", "在 Cage lite 中打开项目，再重新选择"},
}

// fileTransferFailure: the guest refused a file transfer (F3), or answered
// with an HTTP status (F4).
func fileTransferFailure(status int, data []byte) error {
	answer := strings.TrimSpace(string(data))
	if answer == "下载服务不可用" {
		return usererror.Fail("传递文件", "原因是环境里的下载服务没有就绪", "重新打开应用", nil)
	}
	if refusal, ok := fileRefusals[answer]; ok {
		f := usererror.Fail("传递文件", refusal[0], refusal[1], nil).WithLevel(usererror.LevelInfo)
		return f
	}
	return serviceFailure("传递文件", status)
}

// serviceFailure: a service inside the environment answered with an error
// status (F4).
func serviceFailure(action string, status int) error {
	code := fmt.Sprintf("HTTP %d", status)
	switch status {
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		return usererror.Fail(action, "原因是环境里的服务没有响应（"+code+"）", "重启环境", nil, usererror.ToolRestart)
	case http.StatusGatewayTimeout:
		return usererror.Fail(action, "原因是等环境里的服务响应超时（"+code+"）", "稍后重试", nil)
	case http.StatusUnauthorized, http.StatusForbidden:
		return usererror.Fail(action, "原因是环境拒绝了这次连接（"+code+"）", "重新连接环境", nil)
	case http.StatusNotFound, http.StatusGone:
		return usererror.Fail(action, "原因是目标窗口或文件已不存在", "重新打开窗口或重新选择文件", nil).WithLevel(usererror.LevelInfo)
	case http.StatusConflict:
		return usererror.Fail(action, "原因是另一个操作还没结束", "稍后重试", nil).WithLevel(usererror.LevelInfo)
	case http.StatusRequestEntityTooLarge:
		return usererror.Fail(action, "原因是文件超过允许的大小", "分批传递", nil).WithLevel(usererror.LevelInfo)
	case http.StatusTooManyRequests:
		return usererror.Fail(action, "原因是请求太频繁", "稍后重试", nil).WithLevel(usererror.LevelInfo)
	}
	f := usererror.Unrecognized(action, code, "重试", nil, usererror.ToolDiagnostics)
	return f
}
