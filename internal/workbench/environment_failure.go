package workbench

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/pbnjay/memory"
	"github.com/sylvinus/agent-vm/internal/cage"
	"github.com/sylvinus/agent-vm/internal/diagnostics"
	"github.com/sylvinus/agent-vm/internal/host"
	"github.com/sylvinus/agent-vm/internal/usererror"
	"github.com/sylvinus/agent-vm/internal/vm"
)

// environmentFailure names why starting or stopping the environment failed,
// as notes/failure-text-audit-2026-11-28.md group 1 has it: where it got
// stuck, what was seen, one next step and the tools for it. It returns nil
// for setup, which keeps the install rules (failureRules) for now.
// failureFacts are what a failed start knows about its environment, for
// checking the computer when the output names no cause.
type failureFacts struct {
	// dir is the environment's Lima home, on the disk it lives on.
	dir string
	// memoryGiB is the environment's memory setting; 0 when unknown.
	memoryGiB int
	// orphaned reports a virtual machine left running without Lima (#508).
	orphaned func() bool
}

// The computer's state, read after a failure. Tests replace them.
var (
	hostFreeGiB         = host.FreeGiB
	hostFreeMemory      = memory.FreeMemory
	hostHypervisorReady = host.HypervisorReady
	hostVolume          = host.UnsupportedVolume
)

func environmentFailure(op, action string, cause error, output string, facts failureFacts) *usererror.Failure {
	dir := facts.dir
	var next string
	var retry []string
	switch op {
	case "up":
		next, retry = "重试启动", []string{usererror.ToolStart, usererror.ToolDiagnostics}
	case "down", "stop":
		next, retry = "重试停止", []string{usererror.ToolStop, usererror.ToolDiagnostics}
	default:
		return nil
	}
	lower := strings.ToLower(output)
	fail := func(reason, next string, tools ...string) *usererror.Failure {
		return usererror.Fail(action, reason, next, cause, tools...)
	}
	if f := qemuFailure(action, cause, output, facts.memoryGiB); f != nil {
		return f
	}
	if f := limaStartFailure(action, cause, output); f != nil {
		return f
	}
	switch {
	case strings.Contains(lower, strings.ToLower(host.HypervisorPlatformMissing)), strings.Contains(lower, strings.ToLower(host.HypervisorNotReady)):
		return hypervisorFailure(action, cause)
	// Lima reads the VM as stopped while its hostagent still runs (its PID
	// files lost): a start would leave the config naming what the running VM
	// does not have. A stop ends that hostagent, then the start goes on.
	case strings.Contains(output, vm.ErrHostAgentRunning.Error()):
		return usererror.Fail(action, "原因是上次的环境后台仍在运行", "重启环境", cause, usererror.ToolRestart, usererror.ToolDiagnostics)
	// Only `cage stop` says it (internal/cli/vms.go).
	case op != "up" && strings.Contains(lower, "is still running, or lima cannot say"):
		return fail("原因是无法确认虚拟机已停止", "重试停止，不要手动删除环境目录", usererror.ToolStop, usererror.ToolDiagnostics)
	case containsAny(lower, `qemu-img": executable file not found`, `qemu-img\": executable file not found`, `qemu-system-x86_64": executable file not found`, `qemu-system-x86_64\": executable file not found`, "lima needs 'qemu-"):
		return fail("原因是找不到随附的 QEMU", "重新安装 Cage lite", usererror.ToolDiagnostics)
	case containsAny(lower, "base vm not found", "base vm setup did not complete"):
		return fail("原因是环境安装未完成（基础模板缺失）", "继续安装", usererror.ToolContinueInstall)
	case containsAny(lower, "no space left on device", "not enough space on the disk"):
		return spaceFailure(action, cause, dir)
	case containsAny(lower, "must be less than unix_path_max", "` too long: `", "vm name too long"):
		return fail("原因是环境目录路径太长", "在环境与恢复中把环境目录迁到更短的路径", usererror.ToolSettingsProject)
	case strings.Contains(lower, "which lima cannot mount"):
		return fail("原因是目录路径含空格，无法共享进环境", "把该目录移到不含空格的路径后"+next, retry[0])
	case containsAny(lower, "is also your own lima's home", "set agent_vm_lima_home to a folder for agent-vm alone"):
		return fail("原因是系统环境变量 LIMA_HOME 指向了 Cage lite 的虚拟机目录", "取消 LIMA_HOME 后重新打开 Cage lite")
	case strings.Contains(output, "容器 SSH 服务 30 秒内未就绪"):
		return fail("原因是 Linux 容器启动后，SSH 连接超时（30 秒）", next, retry...)
	case strings.Contains(output, "容器连接超时"):
		return fail("原因是连接 Linux 容器超时", next, usererror.ToolStart, usererror.ToolLog)
	case strings.Contains(output, "容器的项目挂载尚未初始化"):
		return fail("原因是 Linux 容器的项目文件夹未挂载", "重启环境", usererror.ToolRestart)
	case strings.Contains(lower, "was left running without lima's host agent") && strings.Contains(lower, "could not stop vm"):
		return orphanFailure(action, cause)
	case containsAny(lower, "permission denied", "access is denied"):
		return deniedFailure(action, cause, output, dir)
	}
	// Nothing in the output names a cause: look at the computer itself
	// (notes/failure-text-audit-2026-11-28.md B1–B7), first match wins.
	if op == "up" {
		if f := checkedFailure(action, cause, facts); f != nil {
			return f
		}
	}
	return usererror.Unrecognized(action, lastErrorLine(output), next, cause, append(retry[:1:1], usererror.ToolLog, usererror.ToolDiagnostics)...)
}

// lowDiskGiB is the free space under which a failed start blames the disk.
const lowDiskGiB = 2

// checkedFailure reads the computer after a start the output does not
// explain: disk space, memory, a left-over virtual machine, Windows
// virtualization, and the file system the environment lives on.
func checkedFailure(action string, cause error, facts failureFacts) *usererror.Failure {
	if facts.dir != "" {
		if free, ok := hostFreeGiB(facts.dir); ok && free < lowDiskGiB {
			return diskFailure(action, cause, facts.dir)
		}
	}
	if free := hostFreeMemory(); facts.memoryGiB > 0 && free > 0 && free < uint64(facts.memoryGiB)<<30 {
		return memoryFailure(action, cause, facts.memoryGiB, free)
	}
	if facts.orphaned != nil && facts.orphaned() {
		return orphanFailure(action, cause)
	}
	if err := hostHypervisorReady(); err != nil {
		return hypervisorFailure(action, cause)
	}
	if facts.dir != "" {
		if kind := hostVolume(facts.dir); kind != "" {
			if kind == host.VolumeNetwork {
				kind = "网络驱动器"
			}
			return usererror.Fail(action, "原因是环境目录所在磁盘不支持（"+kind+"）", "在环境与恢复中把环境目录迁到 NTFS 磁盘", cause, usererror.ToolSettingsProject)
		}
	}
	return nil
}

func hypervisorFailure(action string, cause error) *usererror.Failure {
	return usererror.Fail(action, "原因是 Windows 虚拟化未就绪", "在固件中开启 CPU 虚拟化，在「启用或关闭 Windows 功能」中启用 Windows Hypervisor Platform，然后重启电脑", cause, usererror.ToolStart)
}

// orphanFailure: the virtual machine of the last run still runs without
// Lima, and the start could not stop it (#508). Stopping again, then
// starting, is one button.
func orphanFailure(action string, cause error) *usererror.Failure {
	return usererror.Fail(action, "原因是上次的虚拟机仍在运行，没能停止", "重启环境", cause, usererror.ToolRestart, usererror.ToolDiagnostics)
}

func memoryFailure(action string, cause error, needGiB int, free uint64) *usererror.Failure {
	reason := "原因是电脑可用内存不足"
	switch {
	case needGiB > 0 && free > 0:
		reason = fmt.Sprintf("原因是电脑可用内存不足（需要 %d GiB，可用 %.1f GiB）", needGiB, float64(free)/(1<<30))
	case free > 0:
		reason = fmt.Sprintf("原因是电脑可用内存不足（可用 %.1f GiB）", float64(free)/(1<<30))
	}
	return usererror.Fail(action, reason, "关闭其他程序，或在计算资源中调小内存", cause, usererror.ToolResources, usererror.ToolStart)
}

// deniedFailure tells a refused QEMU from an environment folder that cannot
// be written, by the line that was refused.
func deniedFailure(action string, cause error, output, dir string) *usererror.Failure {
	line := ""
	for _, candidate := range strings.Split(output, "\n") {
		if lower := strings.ToLower(candidate); strings.Contains(lower, "permission denied") || strings.Contains(lower, "access is denied") {
			line = candidate
		}
	}
	lower := strings.ToLower(line)
	if m := qemuProgram.FindString(lower); m != "" {
		return usererror.Fail(action, "原因是系统拒绝运行 QEMU（"+m+"）", "在安全软件中允许 Cage lite", cause, usererror.ToolDiagnostics)
	}
	if dir != "" && (strings.Contains(lower, strings.ToLower(dir)) || strings.Contains(lower, strings.ToLower(filepath.ToSlash(dir)))) {
		return usererror.Fail(action, "原因是系统拒绝写入环境目录（"+dir+"）", "检查环境目录的权限", cause, usererror.ToolSettingsProject)
	}
	return usererror.Fail(action, "原因是系统拒绝访问环境所需的文件或程序", "在安全软件中允许 Cage lite，并检查环境目录的权限", cause, usererror.ToolDiagnostics)
}

var qemuProgram = regexp.MustCompile(`qemu-(?:system-[a-z0-9_]+|img)(?:\.exe)?`)

func diskFailure(action string, cause error, dir string) *usererror.Failure {
	reason := "原因是环境所在磁盘空间不足"
	if free, ok := hostFreeGiB(dir); ok && dir != "" {
		reason = fmt.Sprintf("原因是环境所在磁盘空间不足（剩余 %d GiB）", free)
	}
	return usererror.Fail(action, reason, "释放磁盘空间后重试，不要删除 Cage lite 的数据目录", cause, usererror.ToolStart, usererror.ToolSettingsProject)
}

// spaceFailure: a write found no space. The words are the same for the
// computer's disk and for the environment's own virtual disk, so the
// computer is asked: with room left there, the virtual disk is full, and only
// a larger disk helps.
func spaceFailure(action string, cause error, dir string) *usererror.Failure {
	if free, ok := hostFreeGiB(dir); dir != "" && ok && free >= lowDiskGiB {
		return usererror.Fail(action, "原因是环境的虚拟磁盘已满", "在计算资源中调大磁盘后重试", cause, usererror.ToolResources, usererror.ToolStart)
	}
	return diskFailure(action, cause, dir)
}

// qemuFailure reads QEMU's own words, which runtime and vm pass on as
// 「QEMU 启动失败：…」.
func qemuFailure(action string, cause error, output string, memoryGiB int) *usererror.Failure {
	var message string
	for _, line := range strings.Split(output, "\n") {
		if _, rest, ok := strings.Cut(line, "QEMU 启动失败："); ok {
			message = strings.TrimSpace(rest)
		}
	}
	if message == "" {
		return nil
	}
	lower := strings.ToLower(message)
	fail := func(reason, next string, tools ...string) *usererror.Failure {
		return usererror.Fail(action, reason, next, cause, tools...)
	}
	switch {
	case containsAny(lower, `failed to get "write" lock`, "is another process using the image"):
		return fail("原因是环境磁盘正被另一个进程使用", "重启环境", usererror.ToolRestart, usererror.ToolDiagnostics)
	case containsAny(lower, "cannot set up guest memory", "cannot allocate memory"):
		return memoryFailure(action, cause, memoryGiB, hostFreeMemory())
	case containsAny(lower, "could not access kvm kernel module", "failed to initialize kvm"):
		return fail("原因是 KVM 不可用", "确认已开启 CPU 虚拟化，并允许当前用户使用 KVM", usererror.ToolStart)
	case containsAny(lower, "hv_error", "hvf error"):
		return fail("原因是 macOS 虚拟化框架不可用", "重启电脑后重试启动", usererror.ToolStart)
	}
	f := fail("原因是虚拟机启动即退出", "重试启动", usererror.ToolStart, usererror.ToolDiagnostics)
	f.Raw = lastErrorLine(message)
	return f
}

var (
	limaRequirementFailed  = regexp.MustCompile("failed to satisfy the essential requirement (\\d+) of \\d+ [`\"]([^`\"]+)[`\"]")
	limaRequirementWaiting = regexp.MustCompile("Waiting for the essential requirement (\\d+) of \\d+: [`\"]([^`\"]+)[`\"]")
	limaRequirementMet     = regexp.MustCompile(`The essential requirement (\d+) of \d+ is satisfied`)
	limaNoProgress         = regexp.MustCompile(`no boot progress for (\S+?)[:;,)]?(?:\s|$)`)
	limaHostagentSlow      = regexp.MustCompile(`hostagent \(.*?\) did not start up in (\S+?)[:;,)]?(?:\s|$)`)
)

// limaRequirements are what Lima waits for after the virtual machine boots,
// in the interface's words.
var limaRequirements = map[string]string{
	"ssh":                                 "SSH 连接",
	"user session is ready for ssh":       "用户会话",
	"sshfs binary to be installed":        "文件共享组件",
	"fuse to allow_other as user":         "文件共享组件",
	"boot scripts must have finished":     "开机脚本",
	"systemd must be available":           "系统服务",
	"containerd binaries to be installed": "容器组件",
}

// limaStartFailure reads where Lima's start got stuck: the requirement it
// waited for, and how long nothing happened.
func limaStartFailure(action string, cause error, output string) *usererror.Failure {
	fail := func(reason string, tools ...string) *usererror.Failure {
		if len(tools) == 0 {
			tools = []string{usererror.ToolStart, usererror.ToolDiagnostics}
		}
		return usererror.Fail(action, reason, "重试启动", cause, tools...)
	}
	wait := ""
	if m := limaNoProgress.FindAllStringSubmatch(output, -1); len(m) > 0 {
		wait = limaDuration(m[len(m)-1][1])
	}
	in := func(text string) string {
		if wait == "" {
			return text
		}
		return text + "（" + wait + "）"
	}
	if m := limaHostagentSlow.FindAllStringSubmatch(output, -1); len(m) > 0 {
		wait = limaDuration(m[len(m)-1][1])
		return fail(in("原因是环境后台启动超时"))
	}
	stuck := ""
	if m := limaRequirementFailed.FindAllStringSubmatch(output, -1); len(m) > 0 {
		stuck = m[len(m)-1][2]
	} else if strings.Contains(output, "did not receive an event with the `running` status") || strings.Contains(output, `did not receive an event with the "running" status`) {
		met := map[string]bool{}
		for _, m := range limaRequirementMet.FindAllStringSubmatch(output, -1) {
			met[m[1]] = true
		}
		for _, m := range limaRequirementWaiting.FindAllStringSubmatch(output, -1) {
			if !met[m[1]] {
				stuck = m[2]
			}
		}
		if stuck == "" {
			if wait != "" {
				return fail(in("原因是首次启动没有新进展"), usererror.ToolStart, usererror.ToolLog, usererror.ToolDiagnostics)
			}
			return fail("原因是虚拟机没有按时进入运行状态", usererror.ToolStart, usererror.ToolLog, usererror.ToolDiagnostics)
		}
	}
	if stuck == "" {
		return nil
	}
	name, known := limaRequirements[stuck]
	if !known {
		name = stuck
	}
	if stuck == "ssh" {
		return fail(in("原因是虚拟机启动后，SSH 连接超时"))
	}
	return fail(in("原因是虚拟机启动后，" + name + "未就绪"))
}

// limaDuration turns Go's "10m0s" into 「10 分钟」.
func limaDuration(text string) string {
	d, err := time.ParseDuration(text)
	if err != nil {
		return ""
	}
	return usererror.Duration(d)
}

var (
	logfmtMessage = regexp.MustCompile(`\bmsg="((?:[^"\\]|\\.)*)"`)
	errorWords    = []string{"error", "fatal", "failed", "failure", "denied", "timed out", "timeout", "错误", "失败", "未就绪", "超时"}
)

// lastErrorLine is the last line of output that reads as an error, with its
// log framing removed and private values redacted.
func lastErrorLine(output string) string {
	lines := strings.Split(output, "\n")
	fallback := ""
	for i := len(lines) - 1; i >= 0; i-- {
		line := plainLogLine(lines[i])
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "exit status") {
			if fallback == "" {
				fallback = line
			}
			continue
		}
		if containsAny(lower, errorWords...) {
			return diagnostics.Redact(line)
		}
	}
	return diagnostics.Redact(fallback)
}

// plainLogLine drops a log line's framing: JSON records and logfmt keep their
// message, an "Error: " lead goes.
func plainLogLine(line string) string {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "{") {
		var record struct {
			Message string `json:"msg"`
			Error   string `json:"error"`
		}
		if json.Unmarshal([]byte(line), &record) == nil && record.Message != "" {
			line = record.Message
			if record.Error != "" {
				line += ": " + record.Error
			}
		}
	} else if m := logfmtMessage.FindStringSubmatch(line); m != nil {
		line = strings.ReplaceAll(m[1], `\"`, `"`)
	}
	line = strings.TrimPrefix(line, "Error: ")
	return strings.TrimSpace(line)
}

func containsAny(text string, patterns ...string) bool {
	for _, pattern := range patterns {
		if strings.Contains(text, pattern) {
			return true
		}
	}
	return false
}

// failureFacts gathers what environmentFailure checks after a failure.
func (s *Server) failureFacts() failureFacts {
	limaHome, _ := s.commandLimaHome()
	facts := failureFacts{dir: limaHome, orphaned: s.vmOrphaned}
	if a, err := cage.LoadResourceAllocation(s.configPath); err == nil {
		// The budget the start asked for: automatic memory follows what the
		// computer had free (#1115).
		if memoryGiB, _, err := a.Limits(host.WindowsAvailableMemory()); err == nil {
			facts.memoryGiB = memoryGiB
		}
	}
	return facts
}

// browserNotReady is a start whose virtual machine runs but whose browser
// never answered (A17).
func browserNotReady(waited time.Duration) *usererror.Failure {
	reason := "卡在启动浏览器，原因是浏览器连接未就绪"
	if text := usererror.Duration(waited); text != "" {
		reason += "（" + text + "）"
	}
	return usererror.Fail("启动环境", reason, "重试启动", nil, usererror.ToolStart, usererror.ToolLog)
}

var limaForcedKill = regexp.MustCompile(`QEMU did not exit in \S+?,? forcibly killing QEMU`)

// noteForcedShutdown adds one line to the simple log when Lima had to kill
// QEMU after the shutdown wait (C2): the stop still succeeded. The wording,
// without the wait, is the user's (2026-11-28).
func (s *Server) noteForcedShutdown(output string) {
	if limaForcedKill.MatchString(output) {
		s.setMessage("虚拟机关闭时间超出预期，已强制结束")
	}
}
