package workbench

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sylvinus/agent-vm/internal/host"
	"github.com/sylvinus/agent-vm/internal/usererror"
)

// healthyHost makes the computer look fine to the checks after a failure;
// each test then breaks one thing.
func healthyHost(t *testing.T) {
	t.Helper()
	free, mem, hv, vol := hostFreeGiB, hostFreeMemory, hostHypervisorReady, hostVolume
	t.Cleanup(func() { hostFreeGiB, hostFreeMemory, hostHypervisorReady, hostVolume = free, mem, hv, vol })
	hostFreeGiB = func(string) (int, bool) { return 100, true }
	hostFreeMemory = func() uint64 { return 32 << 30 }
	hostHypervisorReady = func() error { return nil }
	hostVolume = func(string) string { return "" }
}

// After a start the output does not explain, the computer is checked in
// order (failure-text-audit B1–B7); the first finding names the cause.
func TestUnexplainedStartChecksTheComputer(t *testing.T) {
	cause := errors.New("exit status 1")
	output := "starting\nexit status 1"
	for _, tc := range []struct {
		name   string
		breaks func()
		facts  failureFacts
		reason string
		tools  []string
	}{
		{"nothing found", func() {}, failureFacts{dir: `C:\env`, memoryGiB: 8}, "原因未识别", []string{"start", "log", "diagnostics"}},
		{"disk", func() { hostFreeGiB = func(string) (int, bool) { return 1, true } }, failureFacts{dir: `C:\env`},
			"原因是环境所在磁盘空间不足（剩余 1 GiB）", []string{"start", "settings:project"}},
		{"memory", func() { hostFreeMemory = func() uint64 { return 5 << 30 } }, failureFacts{dir: `C:\env`, memoryGiB: 8},
			"原因是电脑可用内存不足（需要 8 GiB，可用 5.0 GiB）", []string{"settings:resources", "start"}},
		{"memory setting unknown", func() { hostFreeMemory = func() uint64 { return 1 << 30 } }, failureFacts{dir: `C:\env`},
			"原因未识别", []string{"start", "log", "diagnostics"}},
		{"left-over virtual machine", func() {}, failureFacts{dir: `C:\env`, orphaned: func() bool { return true }},
			"原因是上次的虚拟机仍在运行，没能停止", []string{"restart", "diagnostics"}},
		{"virtualization", func() { hostHypervisorReady = func() error { return errors.New(host.HypervisorNotReady) } }, failureFacts{dir: `C:\env`},
			"原因是 Windows 虚拟化未就绪", []string{"start"}},
		{"exFAT", func() { hostVolume = func(string) string { return "exFAT" } }, failureFacts{dir: `E:\env`},
			"原因是环境目录所在磁盘不支持（exFAT）", []string{"settings:project"}},
		{"network drive", func() { hostVolume = func(string) string { return host.VolumeNetwork } }, failureFacts{dir: `Z:\env`},
			"原因是环境目录所在磁盘不支持（网络驱动器）", []string{"settings:project"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			healthyHost(t)
			tc.breaks()
			f := environmentFailure("up", "启动环境", cause, output, tc.facts)
			if f == nil || f.Reason != tc.reason || !reflect.DeepEqual(f.Tools, tc.tools) {
				t.Fatalf("%+v, want %q %v", f, tc.reason, tc.tools)
			}
		})
	}
	// A stop is not checked like a start.
	healthyHost(t)
	hostFreeGiB = func(string) (int, bool) { return 1, true }
	if f := environmentFailure("stop", "停止环境", cause, output, failureFacts{dir: `C:\env`}); !f.Unknown {
		t.Fatalf("stop: %+v", f)
	}
}

// The start stops a virtual machine left running without Lima itself; only
// when that stop fails does the start fail, with one button to stop and
// start again (A15).
func TestLeftOverVirtualMachineThatWouldNotStop(t *testing.T) {
	healthyHost(t)
	output := "Warning: VM 'claude-cage-x' was left running without Lima's host agent, cut off: stopping it.\nError: could not stop VM 'claude-cage-x'. See 'agent-vm list'."
	f := environmentFailure("up", "启动环境", errors.New("exit status 1"), output, failureFacts{})
	if f == nil || f.Reason != "原因是上次的虚拟机仍在运行，没能停止" || f.Next != "重启环境" || f.Tools[0] != usererror.ToolRestart {
		t.Fatalf("%+v", f)
	}
}

// A refusal names what was refused: QEMU, or the environment folder (B5).
func TestAccessDeniedNamesWhatWasRefused(t *testing.T) {
	healthyHost(t)
	cause := errors.New("exit status 1")
	for output, want := range map[string]string{
		`Error: fork/exec C:\Program Files\Cage lite\qemu\qemu-system-x86_64.exe: Access is denied.`: "原因是系统拒绝运行 QEMU（qemu-system-x86_64.exe）",
		`Error: open C:\CageData\lima\claude-cage-x\diffdisk: Access is denied.`:                     `原因是系统拒绝写入环境目录（C:\CageData\lima）`,
		`Error: open somewhere: permission denied`:                                                   "原因是系统拒绝访问环境所需的文件或程序",
	} {
		f := environmentFailure("up", "启动环境", cause, output, failureFacts{dir: `C:\CageData\lima`})
		if f == nil || f.Reason != want {
			t.Errorf("%q: %+v, want %q", output, f, want)
		}
	}
}

// QEMU's own memory error now says how much the environment needs (B2).
func TestQEMUMemoryErrorSaysWhatIsNeeded(t *testing.T) {
	healthyHost(t)
	hostFreeMemory = func() uint64 { return 3 << 29 }
	f := environmentFailure("up", "启动环境", errors.New("exit status 1"), "Error: QEMU 启动失败：qemu-system-x86_64: cannot set up guest memory 'pc.ram': Cannot allocate memory", failureFacts{memoryGiB: 4})
	if f == nil || f.Reason != "原因是电脑可用内存不足（需要 4 GiB，可用 1.5 GiB）" {
		t.Fatalf("%+v", f)
	}
}

// A browser that never answers after the start says where and how long (A17).
func TestBrowserNotReadySaysWhereAndHowLong(t *testing.T) {
	f := browserNotReady(9 * time.Second)
	if f.Error() != "启动环境失败，卡在启动浏览器，原因是浏览器连接未就绪（9 秒）。解决建议：重试启动。" || !reflect.DeepEqual(f.Tools, []string{"start", "log"}) {
		t.Fatalf("%q %v", f.Error(), f.Tools)
	}
}

// A stop that had to kill QEMU still succeeds, and leaves one line in the
// simple log (C2).
func TestForcedShutdownLeavesOneLogLine(t *testing.T) {
	s := &Server{}
	var lines []string
	m := &s.jobs
	done := make(chan struct{})
	var operation sync.Mutex
	_, err := m.submit(&operation, "stop", 0, nil, nil, func() error {
		defer close(done)
		s.noteForcedShutdown(`{"level":"warning","msg":"QEMU did not exit in 3m0s, forcibly killing QEMU","time":"2026-11-28T10:00:00+08:00"}`)
		s.noteForcedShutdown("Stopping VM… done")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	<-done
	waitFor(t, func() bool { return !m.snapshot().Running })
	for _, entry := range m.logView().Entries {
		if strings.Contains(entry.Message, "强制结束") {
			lines = append(lines, entry.Message)
		}
	}
	if !reflect.DeepEqual(lines, []string{"虚拟机未在 3 分钟内关机，已强制结束"}) || m.snapshot().Error != "" {
		t.Fatalf("lines %q, error %q", lines, m.snapshot().Error)
	}
}
