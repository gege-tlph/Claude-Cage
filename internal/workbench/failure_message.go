package workbench

import (
	"io"
	"strings"
	"sync"

	"github.com/sylvinus/agent-vm/internal/host"
	"github.com/sylvinus/agent-vm/internal/usererror"
)

const failureHelp = "在设置中导出诊断包，并附上出错时正在执行的操作。"

type failureRule struct {
	patterns     []string
	reason, next string
}

// failureRules name known diagnostics in process output. Every reason and
// next step needs an English entry in ui/src/locales/en.json
// (TestFailureMessagesHaveEnglish).
var failureRules = []failureRule{
	{[]string{strings.ToLower(host.HypervisorPlatformMissing), strings.ToLower(host.HypervisorNotReady)}, "Windows 虚拟化尚未就绪", "请在“启用或关闭 Windows 功能”中启用 Windows Hypervisor Platform（Windows 虚拟机监控程序平台），确认固件已开启 CPU 虚拟化，重启电脑后再继续。"},
	{[]string{"is still running, or lima cannot say"}, "无法确认环境已停止：环境进程状态异常（例如虚拟机管理进程已退出而虚拟机仍在运行）", "Cage lite 已保留环境。请重试停止；仍失败时在设置中导出诊断包，不要删除环境目录。"},
	{[]string{`qemu-img": executable file not found`, `qemu-img\": executable file not found`, `qemu-system-x86_64": executable file not found`, `qemu-system-x86_64\": executable file not found`, "lima needs 'qemu-"}, "找不到运行虚拟机所需的 QEMU 组件（如 qemu-img）", "请重新运行 Cage lite 安装程序以恢复随附的 QEMU；从 WSL 或脚本启动 Cage lite 时，确认环境变量 PATHEXT 包含 .EXE。"},
	{[]string{"base vm not found"}, "基础模板缺失，无法创建或重置环境", "请先完成环境安装；已有环境的磁盘应保留，不要删除或重置个人数据。"},
	{[]string{"base vm setup did not complete"}, "基础模板尚未安装完成", "请继续环境安装后重试；已有环境的磁盘应保留，不要删除或重置个人数据。"},
	{[]string{"desktop file service state=failed"}, "Claude Desktop 的文件服务已经退出", "请从 Cage lite 重新打开 Claude Desktop；若应用反复退出，请在设置中查看资源用量并导出诊断包。"},
	{[]string{"curl: (7)", "connection refused"}, "目标服务没有接受连接", "请确认环境及所选代理已启动；文件传递时请重新打开接收文件的应用，再重试。"},
	{[]string{"no space left on device", "not enough space on the disk"}, "磁盘空间不足，无法继续写入", "请释放环境所在磁盘的空间后继续；不要删除 Cage lite 的个人数据目录。"},
	{[]string{"dpkg was interrupted"}, "上次中断的软件安装未能自动补完，环境的软件包管理器仍停在未完成状态", "请重试一次；仍失败时在设置中导出诊断包。不要手动删除 dpkg 的状态或锁文件。"},
	{[]string{"could not get lock", "unable to acquire the dpkg", "another process using it"}, "环境中已有另一个软件安装或更新进程", "请等待当前安装结束后再继续；不要手动删除安装锁文件。"},
	{[]string{"temporary failure resolving", "could not resolve host", "curl: (6)", "something wicked happened resolving", "no address associated with hostname"}, "下载服务器的域名解析失败", "请在网络与代理设置中检查所选节点和 DNS，再继续安装。"},
	{[]string{"curl: (28)", "connection timed out", "operation timed out"}, "下载或环境连接等待超时", "请检查所选代理是否可用，再继续；已保存的下载会尝试续传。"},
	{[]string{"curl: (60)", "certificate verify failed", "certificate verification failed"}, "下载服务器的证书验证未通过", "请检查电脑时间和所选代理；仍失败时导出诊断包，不要关闭证书验证。"},
	{[]string{"curl: (35)", "unexpected eof while reading", "ssl_error_syscall"}, "与下载服务器的 TLS 加密握手未能完成，连接被中途断开", "这通常发生在所选代理或出口与该服务器之间。请在网络与代理设置中换一个节点后重试；仍失败时，可在“设置 → 网络与代理”中开启“安装与工具下载兼容模式（限制 curl 最高使用 TLS 1.2）”再试。不要关闭证书验证。"},
	{[]string{"407 proxy authentication", "proxy authentication required"}, "代理服务器拒绝了当前登录凭据", "请在网络与代理设置中更新代理账号或订阅，再应用节点。"},
	{[]string{"permission denied", "access is denied"}, "系统拒绝访问安装所需的文件或程序", "请检查应用及环境目录的权限；若安全软件拦截了 Cage lite，请先核实其拦截记录。"},
	// Lima refuses an instance whose socket paths exceed the system limit (#913).
	{[]string{"must be less than unix_path_max", "` too long: `", "vm name too long"}, limaPathTooLongReason, limaPathTooLongNext},
	{[]string{"which lima cannot mount"}, "目录路径含有空格，Lima 无法把它共享进环境", "操作日志中列出了这个目录；把它移到不含空格的路径后重试。若是 Cage lite 配置目录旁的内部传输目录，步骤见故障排查文档“用户目录含空格时环境无法启动”。"},
	{[]string{"is also your own lima's home", "set agent_vm_lima_home to a folder for agent-vm alone"}, "宿主机环境变量 LIMA_HOME 与 Cage lite 的虚拟机目录重合", "请取消设置 LIMA_HOME 或将其改为其他目录，完全退出并重新打开 Cage lite 后重试；若正在安装应用更新，请完全退出 Cage lite 后直接运行官方安装包原地升级，原有数据和虚拟机磁盘会保留。"},
	{[]string{"tried to start delayed item"}, "环境的软件包下载程序反复重试，未能完成安装", "请使用包含 APT 修复的 Cage lite 构建后继续安装；仍失败时导出诊断包。"},
}

// Only recognize known diagnostics. Unrecognized process output stays in the
// existing operation log and never becomes an arbitrary user-facing message.
func executionFailure(action string, err error, output string) error {
	if reason, next, ok := imageDownloadFailure(output); ok {
		return usererror.New(action, reason, next, err)
	}
	lower := strings.ToLower(output)
	for _, rule := range failureRules {
		for _, pattern := range rule.patterns {
			if strings.Contains(lower, pattern) {
				return usererror.New(action, rule.reason, rule.next, err)
			}
		}
	}
	return usererror.Wrap(action, err, "打开“操作日志”查看最后的错误；仍无法判断时，"+failureHelp)
}

// serviceHTTPFailure and fileServiceFailure: see desktop_failure.go
// (failure-text-audit group 4).
func serviceHTTPFailure(action string, status int) error { return serviceFailure(action, status) }

func fileServiceFailure(status int, data []byte) error { return fileTransferFailure(status, data) }

func browserOperation(path string) string {
	switch path {
	case "/navigate":
		return "打开网页"
	case "/reload":
		return "刷新网页"
	case "/audit":
		return "读取浏览器检测结果"
	case "/tabs":
		return "读取浏览器标签页"
	case "/paste-image":
		return "粘贴图片"
	case "/editor-restart":
		return "打开编辑器"
	case "/extensions":
		return "读取浏览器扩展"
	default:
		return "执行浏览器操作"
	}
}

type failureOutput struct {
	mu   sync.Mutex
	tail []byte
}

func (b *failureOutput) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(data)
	if len(data) > 8192 {
		data = data[len(data)-8192:]
	}
	b.tail = append(b.tail, data...)
	if len(b.tail) > 8192 {
		b.tail = b.tail[len(b.tail)-8192:]
	}
	return n, nil
}
func (b *failureOutput) String() string { b.mu.Lock(); defer b.mu.Unlock(); return string(b.tail) }
func (b *failureOutput) writer(dst io.Writer) io.Writer {
	if dst == nil {
		return b
	}
	return io.MultiWriter(dst, b)
}
