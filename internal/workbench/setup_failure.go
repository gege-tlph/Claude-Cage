package workbench

import (
	"fmt"
	"strings"

	"github.com/sylvinus/agent-vm/internal/diagnostics"
	"github.com/sylvinus/agent-vm/internal/usererror"
)

// Install failures (notes/failure-text-audit-2026-11-28.md, group 3). The
// operation keeps its name — 「启动环境」 on the workbench, 「安装」 in first
// run (describeFailure) — and the install step goes into the reason:
// 「安装失败，原因是下载 Debian 13 镜像时 2 个下载地址都失败（…）」. The lead
// is left empty here.

// installStage is the install step of a progress label: 「正在下载 Debian 13
// 镜像」 → 「下载 Debian 13 镜像」.
func installStage(label string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimRight(label, "…."), "正在"))
}

// spaced ends a step named after a Latin word with a space, for the 「时」
// after it: 「安装 Node.js 24 时」.
func spaced(stage string) string {
	if last := stage[len(stage)-1]; last < 0x80 && last != ' ' {
		return stage + " "
	}
	return stage
}

// installFailure names why the install step stage failed. It falls back to
// the start rules (environment_failure.go), then to 「卡在{stage}，原因未识别」.
func installFailure(stage string, cause error, output string, facts failureFacts) *usererror.Failure {
	if stage == "" {
		stage = "准备环境"
	}
	stage = spaced(stage)
	if f := offlineDataFailure(stage, cause, output); f != nil {
		return f
	}
	if f := imageFailure(stage, cause, output); f != nil {
		return f
	}
	if f := aptFailure(stage, cause, output); f != nil {
		return f
	}
	if f := downloadFailure(stage, cause, output); f != nil {
		return f
	}
	f := environmentFailure("up", "", cause, output, facts)
	if f.Unknown {
		f.StuckAt(stage)
	}
	return f
}

var imageCauseShort = map[string]string{
	"eof": "连接被中途断开", "timeout": "连接超时", "tls": "TLS 或证书错误", "dns": "域名解析失败",
	"refused": "连接被拒绝", "proxy": "连不上电脑的系统代理", "proxy-config": "读不出电脑的系统代理设置",
	"checksum": "校验不符", "other": "其他错误",
}

// imageFailure: every download address of the system image failed (I1, I2).
func imageFailure(stage string, cause error, output string) *usererror.Failure {
	var tokens []string
	for _, line := range strings.Split(output, "\n") {
		if m := imageDownloadLine.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			tokens = strings.Fields(m[1])
		}
	}
	if len(tokens) == 0 {
		return nil
	}
	var parts []string
	seen := map[string]bool{}
	kept, hostProxy, hostProxyConfig, checksum := false, false, false, false
	for _, token := range tokens {
		fields := strings.Split(token, ":")
		why, known := imageCauseShort[fields[1]]
		if !known {
			why = "HTTP " + strings.TrimPrefix(fields[1], "http-")
		}
		seen[why] = true
		parts = append(parts, imageMirrorNames[fields[0]]+"："+why)
		kept = kept || fields[2] == "kept"
		hostProxy = hostProxy || fields[1] == "proxy"
		hostProxyConfig = hostProxyConfig || fields[1] == "proxy-config"
		checksum = checksum || fields[1] == "checksum"
	}
	detail := strings.Join(parts, "；")
	if len(seen) == 1 {
		for why := range seen {
			detail = why
		}
	}
	reason := fmt.Sprintf("原因是%s时 %d 个下载地址都失败（%s）", stage, len(tokens), detail)
	next, tools := "稍后继续安装", []string{usererror.ToolStart, usererror.ToolLog}
	switch {
	case hostProxyConfig:
		// The image comes through the computer's own proxy settings, not a
		// Cage lite node (the tooltip of 网络代理 says so).
		reason = fmt.Sprintf("原因是%s时读不出电脑的系统代理设置（PAC）", stage)
		next = "检查系统代理与 PAC 设置后继续安装"
	case hostProxy:
		reason = fmt.Sprintf("原因是%s时连不上电脑的系统代理（%d 个下载地址都失败）", stage, len(tokens))
		next = "打开系统代理软件，或清除 HTTPS_PROXY 后继续安装"
	case checksum && len(seen) == 1:
		reason = fmt.Sprintf("原因是%s时下载的镜像校验不符（已丢弃）", stage)
		next, tools = "继续安装，会重新下载", []string{usererror.ToolStart, usererror.ToolSettingsNetwork}
	case kept:
		next = "稍后继续安装，已下载的部分会续传"
	}
	return usererror.Because(reason, next, cause, tools...)
}

// aptFailure: an APT call of the setup script failed (I3). The command, the
// packages and the output tail stay in the log.
func aptFailure(stage string, cause error, log string) *usererror.Failure {
	ev := parseAPTFailure(log)
	if ev == nil {
		return nil
	}
	tail := strings.Join(ev.tail, "\n")
	lower := strings.ToLower(tail)
	retry := []string{usererror.ToolStart, usererror.ToolLog}
	network := []string{usererror.ToolSettingsNetwork, usererror.ToolStart}
	var f *usererror.Failure
	switch {
	case strings.Contains(lower, "dpkg was interrupted"):
		f = usererror.Because("原因是"+stage+"时，上次中断的安装没能补完（dpkg）", "继续安装，不要手动删除 dpkg 的锁文件", cause, retry...)
	case containsAny(lower, "temporary failure resolving", "could not resolve", "name or service not known", "something wicked happened resolving", "no address associated with hostname"):
		// The source's host stays in the log: it may be a private mirror.
		f = usererror.Because("原因是"+stage+"时解析不了软件源域名", "换一个节点后继续安装", cause, network...)
	case containsAny(lower, "unexpected eof while reading", "error reading from server"):
		next := "继续安装"
		if ev.partialBytes > 0 {
			next = "继续安装，APT 缓存里 " + aptPartialSize(ev.partialBytes) + " 未完成的软件包会复用"
		}
		f = usererror.Because("原因是"+stage+"时下载连接中断", next, cause, retry...)
	case containsAny(lower, "connection timed out", "timed out", "could not connect to", "unable to connect to", "connection failed", "connection refused"):
		f = usererror.Because("原因是"+stage+"时连接软件源超时", "换一个节点后继续安装", cause, network...)
	case containsAny(lower, "proxy authentication required", "407 proxy"):
		f = usererror.Because("原因是"+stage+"时代理拒绝了登录凭据（407）", "在网络代理中更新订阅后继续安装", cause, network...)
	case containsAny(lower, aptNotFoundNeedle, "has no installation candidate"):
		reason := "原因是" + stage + "时软件源里找不到所需的软件包"
		if m := aptMissingPackage.FindStringSubmatch(tail); m != nil && m[1]+m[2] != "" {
			reason = "原因是" + stage + "时软件源里找不到 " + m[1] + m[2]
		}
		f = usererror.Because(reason, "稍后继续安装", cause, retry...)
	case strings.Contains(lower, "no space left on device"):
		f = usererror.Because("原因是"+stage+"时环境磁盘空间不足", "在环境与恢复中调大磁盘后继续安装", cause, usererror.ToolSettingsProject, usererror.ToolStart)
	case containsAny(lower, "could not get lock", "unable to acquire the dpkg", "another process using it"):
		f = usererror.Because("原因是"+stage+"时另一个安装进程占着软件包管理器", "稍后继续安装，不要手动删除安装锁文件", cause, retry...)
	default:
		last := ""
		if len(ev.tail) > 0 {
			last = ev.tail[len(ev.tail)-1]
		}
		f = usererror.Unrecognized("", diagnostics.Redact(last), "继续安装", cause, usererror.ToolStart, usererror.ToolLog, usererror.ToolDiagnostics)
		f.StuckAt(stage)
	}
	detail := "command=" + clip(ev.command, aptCommandBytes) + " exit=" + ev.exit
	if tail != "" {
		detail += " tail=" + strings.ReplaceAll(tail, "\n", " | ")
	}
	diagnostics.Log("error", "package-install", diagnostics.Redact("reason="+f.Reason+" "+detail))
	return f
}

// downloadFailure: a curl download of the setup failed (I4).
func downloadFailure(stage string, cause error, output string) *usererror.Failure {
	lower := strings.ToLower(output)
	network := []string{usererror.ToolSettingsNetwork, usererror.ToolStart}
	switch {
	case containsAny(lower, "curl: (6)", "could not resolve host"):
		return usererror.Because("原因是"+stage+"时域名解析失败", "换一个节点后继续安装", cause, network...)
	case containsAny(lower, "curl: (28)", "operation timed out"):
		return usererror.Because("原因是"+stage+"时连接超时", "换一个节点后继续安装", cause, network...)
	case containsAny(lower, "curl: (35)", "unexpected eof while reading", "ssl_error_syscall"):
		return usererror.Because("原因是"+stage+"时 TLS 握手中断", "开启「安装与工具下载兼容模式」后继续安装", cause, usererror.ToolTLS12Retry, usererror.ToolSettingsNetwork)
	case containsAny(lower, "curl: (60)", "certificate verify failed", "certificate verification failed"):
		return usererror.Because("原因是"+stage+"时证书验证未通过", "检查电脑时间后继续安装，不要关闭证书验证", cause, usererror.ToolStart)
	case containsAny(lower, "curl: (7)"):
		return usererror.Because("原因是"+stage+"时连接被拒绝", "换一个节点后继续安装", cause, network...)
	case strings.Contains(lower, "tried to start delayed item"):
		return usererror.Because("原因是"+stage+"时软件包下载反复重试没有完成", "继续安装", cause, usererror.ToolStart, usererror.ToolLog)
	}
	return nil
}
