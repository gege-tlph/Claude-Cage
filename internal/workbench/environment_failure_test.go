package workbench

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/sylvinus/agent-vm/internal/host"
	"github.com/sylvinus/agent-vm/internal/usererror"
)

func TestEnvironmentFailureNamesWhereStartingGotStuck(t *testing.T) {
	cause := errors.New("exit status 1")
	for _, tc := range []struct {
		name, op, output, reason string
		tools                    []string
	}{
		{"ssh requirement", "up", "INFO[0005] [hostagent] Waiting for the essential requirement 1 of 2: `ssh`\nFATA[0605] failed to satisfy the essential requirement 1 of 2 `ssh`: hint: exit status 255",
			"原因是虚拟机启动后，SSH 连接超时", []string{"start", "diagnostics"}},
		{"stuck waiting, with the wait", "up", "[hostagent] Waiting for the essential requirement 1 of 2: \"ssh\"\n[hostagent] The essential requirement 1 of 2 is satisfied\n[hostagent] Waiting for the essential requirement 2 of 2: \"boot scripts must have finished\"\nError: did not receive an event with the `running` status: no boot progress for 10m0s",
			"原因是虚拟机启动后，开机脚本未就绪（10 分钟）", []string{"start", "diagnostics"}},
		{"first boot without progress", "up", "Error: did not receive an event with the `running` status: no boot progress for 10m0s",
			"原因是首次启动没有新进展（10 分钟）", []string{"start", "log", "diagnostics"}},
		{"hostagent slow", "up", "Error: hostagent (`C:\\x\\ha.pid`) did not start up in 1m0s (hint: see `C:\\x\\ha.stderr.log`)",
			"原因是环境后台启动超时（1 分钟）", []string{"start", "diagnostics"}},
		{"image locked", "up", "Error: QEMU 启动失败：qemu-system-x86_64: Failed to get \"write\" lock\nIs another process using the image [diffdisk]?",
			"原因是环境磁盘正被另一个进程使用", []string{"restart", "diagnostics"}},
		{"qemu other", "up", "Error: QEMU 启动失败：qemu-system-x86_64: -accel whpx: injection failed",
			"原因是虚拟机启动即退出", []string{"start", "diagnostics"}},
		{"kvm", "up", "Error: QEMU 启动失败：Could not access KVM kernel module: No such file or directory",
			"原因是 KVM 不可用", []string{"start"}},
		{"hypervisor", "up", strings.ToLower(host.HypervisorPlatformMissing),
			"原因是 Windows 虚拟化未就绪", []string{"start"}},
		{"stop state", "stop", "Error: hostagent is gone, but the VM is still running, or lima cannot say",
			"原因是无法确认虚拟机已停止", []string{"stop", "diagnostics"}},
		{"base missing", "up", "Error: base vm not found",
			"原因是环境安装未完成（基础模板缺失）", []string{"continue-install"}},
		{"container ssh", "up", "Error: 容器 SSH 服务 30 秒内未就绪，请稍后重试启动；原容器文件已保留",
			"原因是 Linux 容器启动后，SSH 连接超时（30 秒）", []string{"start", "diagnostics"}},
		{"container mount", "up", "Error: 容器的项目挂载尚未初始化，请先完整停止环境再启动；文件和登录状态会保留",
			"原因是 Linux 容器的项目文件夹未挂载", []string{"restart"}},
		{"unknown", "up", "starting\nlevel=error msg=\"browser display exited, status=1/FAILURE\"\nexit status 1",
			"原因未识别", []string{"start", "log", "diagnostics"}},
	} {
		f := environmentFailure(tc.op, "启动环境", cause, tc.output, "")
		if f == nil {
			t.Errorf("%s: no failure", tc.name)
			continue
		}
		if f.Reason != tc.reason || !reflect.DeepEqual(f.Tools, tc.tools) {
			t.Errorf("%s: %q %v, want %q %v", tc.name, f.Reason, f.Tools, tc.reason, tc.tools)
		}
		if !errors.Is(f, cause) {
			t.Errorf("%s: cause lost", tc.name)
		}
	}
	if f := environmentFailure("setup", "准备 Linux 环境", cause, "Error: base vm not found", ""); f != nil {
		t.Fatalf("setup keeps the install rules: %+v", f)
	}
	unknown := environmentFailure("up", "启动环境", cause, "level=error msg=\"browser display exited, status=1/FAILURE\"\nexit status 1", "")
	if unknown.Raw != "browser display exited, status=1/FAILURE" || !unknown.Unknown {
		t.Fatalf("last error line: %+v", unknown)
	}
}

func TestLastErrorLineDropsLogFraming(t *testing.T) {
	for output, want := range map[string]string{
		"ok\n{\"level\":\"error\",\"msg\":\"accept tcp failed\",\"error\":\"closed\"}\nexit status 1": "accept tcp failed: closed",
		"Error: 容器连接超时；容器文件保留\n":                                                                      "容器连接超时；容器文件保留",
		"just output\nexit status 2":                                                                  "exit status 2",
		"":                                                                                            "",
	} {
		if got := lastErrorLine(output); got != want {
			t.Errorf("%q: %q, want %q", output, got, want)
		}
	}
}

// An unrecognized failure says which stage it was stuck at; the job keeps it
// in three parts for the interface and joins it in Error.
func TestJobKeepsTheFailureAndItsStage(t *testing.T) {
	var m jobManager
	var operation sync.Mutex
	_, err := m.submit(&operation, "start", 0, nil, nil, func() error {
		m.execution().message("正在启动浏览器、编辑器与共享文件…")
		return usererror.Unrecognized("启动环境", "browser-display.service failed", "重试启动", errors.New("exit status 1"), usererror.ToolStart)
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return !m.snapshot().Running })
	job := m.snapshot()
	if job.Failure == nil || job.Failure.Reason != "卡在启动浏览器，原因未识别" || job.Failure.Level != usererror.LevelError {
		t.Fatalf("failure: %+v", job.Failure)
	}
	if job.Error != "启动环境失败，卡在启动浏览器，原因未识别（最后一条错误：browser-display.service failed）。解决建议：重试启动。" {
		t.Fatalf("error: %q", job.Error)
	}
	view := m.logView()
	last := view.Entries[len(view.Entries)-1]
	if last.State != "failed" || last.Level != usererror.LevelError || last.Message != job.Error {
		t.Fatalf("log entry: %+v", last)
	}
}
