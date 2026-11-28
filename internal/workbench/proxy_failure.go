package workbench

import (
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/sylvinus/agent-vm/internal/managedproxy"
	"github.com/sylvinus/agent-vm/internal/usererror"
)

// Proxy and exit failures the workbench names (notes/failure-text-audit-
// 2026-11-28.md, group 2). The exit probe and the network check build their
// own (internal/cage probe_failure.go, safety_failure.go); here are the
// node manager's: subscriptions, imports, the core, a missing node.

// proxyActionLeads name the operation of a proxy action as its button does.
// Applying a node is the job's own name, 「应用代理节点」.
var proxyActionLeads = map[string]string{
	"import":       "导入订阅",
	"import-local": "导入配置",
	"refresh":      "更新订阅",
	"edit":         "保存订阅",
	"edit-node":    "保存地址",
	"delete":       "删除订阅",
	"restart":      "重启代理核心",
	"stop":         "停止代理核心",
}

// proxyActionFailure gives a failed proxy action its operation, and the
// retry tool it lacks.
func proxyActionFailure(action string, err error) error {
	if err == nil {
		return nil
	}
	err = usererror.Own(knownFailure("proxy", err))
	f, ok := usererror.AsFailure(err)
	if !ok {
		return err
	}
	if lead := proxyActionLeads[action]; lead != "" && f.Lead == "" {
		f.Lead = lead + "失败"
	}
	if len(f.Tools) == 0 && (action == "route" || action == "select") {
		f.Tools = []string{usererror.ToolReapply}
	}
	return err
}

// proxyRequirementFailure: the environment needs a node that is not chosen,
// or not applied (P12). Nothing failed; the user has a step to take.
func proxyRequirementFailure(err error) *usererror.Failure {
	switch {
	case errors.Is(err, managedproxy.ErrNoNodeSelected):
		return usererror.Notice("启动环境前需要代理节点", "原因是还没有选择节点", "在网络与代理中导入订阅并选择节点", err, usererror.ToolSettingsNetwork)
	case errors.Is(err, managedproxy.ErrNodeNotApplied), err.Error() == errProxyNotApplied.Error():
		return usererror.Notice("启动环境前需要应用节点", "原因是所选节点尚未应用，或配置已变化", "重新应用节点", err, usererror.ToolReapply)
	}
	return nil
}

// relayFailure: an auto 前置 (#1063) the backend refuses, or a saved one it
// can no longer use. Outside a proxy action the environment was starting.
func relayFailure(kind string, err error) *usererror.Failure {
	switch {
	case errors.Is(err, managedproxy.ErrRelaySourceReused):
		return usererror.Because("原因是两个自动前置用了同一个订阅", "给其中一个前置换订阅或改成自选节点", err, usererror.ToolSettingsNetwork)
	case errors.Is(err, managedproxy.ErrRelayPoolOverlap):
		return usererror.Because("原因是两个自动前置有相同的节点", "调整其中一个前置的范围", err, usererror.ToolSettingsNetwork)
	case errors.Is(err, managedproxy.ErrRelayTooMany):
		return usererror.Because("原因是自动前置的节点超过 300 个", "改用自选节点缩小范围", err, usererror.ToolSettingsNetwork)
	case errors.Is(err, managedproxy.ErrRelayRefreshEmptied), errors.Is(err, managedproxy.ErrRelayRefreshNoNodes):
		return usererror.Because("原因是更新后自动前置没有可用节点", "在网络与代理中调整前置后再更新", err, usererror.ToolSettingsNetwork).WithLevel(usererror.LevelWarn)
	case errors.Is(err, errSourceInUse):
		return usererror.Because("原因是当前代理链正在用这个订阅", "先换前置或出口并应用", err)
	}
	reason := ""
	switch {
	case errors.Is(err, managedproxy.ErrRelaySourceGone):
		reason = "原因是自动前置用的订阅已被删除"
	case errors.Is(err, managedproxy.ErrRelayNodeGone):
		reason = "原因是自动前置的自选节点已被移除"
	case errors.Is(err, managedproxy.ErrRelayNoNodes):
		reason = "原因是自动前置没有可用节点"
	default:
		return nil
	}
	if kind == "proxy" || kind == "proxy-update" {
		return usererror.Because(reason, "在网络与代理中重新选择前置", err, usererror.ToolSettingsNetwork)
	}
	return usererror.Notice("启动环境前需要重新选择前置", reason, "在网络与代理中重新选择前置", err, usererror.ToolSettingsNetwork)
}

// errSourceInUse: a subscription an auto 前置 or the chain uses cannot be
// deleted.
var errSourceInUse = errors.New("该来源正用于当前代理链或自动切换中转组，请先选择并应用其他路由")

// errProxyNotApplied is the start's own check that the saved node is the one
// the core runs (runtime.go).
var errProxyNotApplied = errors.New("内置代理配置未应用，请重新选择节点")

// coreFailure: the bundled core did not come up (P6).
func coreFailure(err error) *usererror.Failure {
	switch {
	case errors.Is(err, managedproxy.ErrCoreNotReady):
		return usererror.Because("原因是代理核心 15 秒内没有就绪", "重新应用节点", err, usererror.ToolReapply, usererror.ToolLog, usererror.ToolDiagnostics)
	case errors.Is(err, managedproxy.ErrCoreExited):
		return usererror.Because("原因是代理核心启动后立即退出", "重新应用节点", err, usererror.ToolReapply, usererror.ToolLog, usererror.ToolDiagnostics)
	}
	return nil
}

var subscriptionHTTPStatus = regexp.MustCompile(`订阅服务器返回 HTTP (\d+)`)

// subscriptionFailure names why downloading a subscription failed (P11): the
// subscription went through the selected node, or the computer's own network
// (viaHost). previous is how many nodes the source still has; source is its
// ID, for the retry tools.
func subscriptionFailure(lead string, err error, viaHost bool, previous int, source string) *usererror.Failure {
	message := err.Error()
	if !strings.HasPrefix(message, "订阅下载") && !subscriptionHTTPStatus.MatchString(message) {
		// Refused redirects, private or infrastructure addresses: product
		// rules whose own message already says what to do.
		return nil
	}
	path, next, tools := "经所选节点", "通过主机网络更新", []string{usererror.ToolRefreshHost, usererror.ToolRefresh}
	if viaHost {
		path, next, tools = "经电脑本机网络", "稍后重试", []string{usererror.ToolRefresh}
	}
	var reason string
	var dial *net.OpError
	var dns *net.DNSError
	var timeout net.Error
	var authority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	switch {
	case subscriptionHTTPStatus.MatchString(message):
		code := subscriptionHTTPStatus.FindStringSubmatch(message)[1]
		reason = "原因是订阅服务器返回 HTTP " + code
		next, tools = "到服务商处重新复制订阅地址", []string{usererror.ToolSettingsNetwork, usererror.ToolRefresh}
		if code == "429" || code[0] == '5' {
			next, tools = "稍后重试", []string{usererror.ToolRefresh}
		}
	case strings.HasPrefix(message, "订阅下载中断或超过 8 MiB"):
		reason, next, tools = "原因是订阅下载中断或超过 8 MiB", "稍后重试", []string{usererror.ToolRefresh}
	case !viaHost && errors.As(err, &dial) && dial.Op == "dial":
		reason, next, tools = "原因是连不上本机代理核心", "重新应用节点", []string{usererror.ToolReapply, usererror.ToolRefreshHost}
	case errors.As(err, &dns):
		reason = "原因是" + path + "解析不了订阅服务器的域名"
	case errors.As(err, &timeout) && timeout.Timeout(), errors.Is(err, syscall.ETIMEDOUT):
		reason = "原因是" + path + "连接订阅服务器超时（30 秒）"
	case errors.As(err, &authority), errors.As(err, &hostname):
		reason, next, tools = "原因是订阅服务器的证书验证未通过", "到服务商处确认订阅地址", []string{usererror.ToolSettingsNetwork}
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, syscall.ECONNRESET):
		reason = "原因是订阅服务器中途断开了连接"
	case !viaHost && strings.Contains(causeText(err), "SOCKS5"):
		// The core answered the CONNECT with a failure: the node could not
		// reach the subscription server.
		reason = "原因是经所选节点连不上订阅服务器"
	default:
		why := usererror.Reason(errors.Unwrap(err))
		if why == "" {
			why = "原因未识别"
		}
		reason = fmt.Sprintf("原因是%s下载订阅失败（%s）", path, why)
	}
	if previous > 0 {
		reason += fmt.Sprintf("，仍在用上次的 %d 个节点", previous)
	}
	f := usererror.Fail(lead, reason, next, err, tools...).WithLevel(usererror.LevelWarn)
	f.Target = source
	return f
}

// causeText is the text behind a download error's fixed message.
func causeText(err error) string {
	if cause := errors.Unwrap(err); cause != nil {
		return cause.Error()
	}
	return ""
}

var noImportableNodes = regexp.MustCompile(`^没有可导入的节点，已跳过 (\d+) 个；`)

// importFailure: no node of the content could be imported (P13).
func importFailure(action string, err error) *usererror.Failure {
	m := noImportableNodes.FindStringSubmatch(err.Error())
	if m == nil {
		return nil
	}
	lead := proxyActionLeads[action]
	if lead == "" {
		lead = "导入订阅"
	}
	reason := "原因是 " + m[1] + " 个节点都没能导入"
	next := "核对节点配置后重新导入"
	n, _ := strconv.Atoi(m[1])
	switch text := err.Error(); {
	case n > 0 && strings.Count(text, "分享链接的协议暂不支持") >= n:
		reason += "（分享链接的协议不支持）"
		next = "改用 Clash/Mihomo YAML 导入"
	case n > 0 && strings.Count(text, "MASQUE 分享链接缺少 ECDSA 密钥")+strings.Count(text, "MASQUE 协议，无法导入") >= n:
		// The bundled core cannot run MASQUE from a share link or YAML
		// without its keys: another file of the same nodes will not help.
		reason += "（MASQUE 协议不支持）"
		next = "改用其他协议的节点"
	}
	return usererror.Fail(lead, reason, next, err).WithLevel(usererror.LevelWarn)
}
