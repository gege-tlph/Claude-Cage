package cage

import (
	"fmt"
	"strings"
	"time"

	"github.com/sylvinus/agent-vm/internal/managedproxy"
	"github.com/sylvinus/agent-vm/internal/usererror"
)

// The exit probe's failures in three lines (notes/failure-text-audit-
// 2026-11-28.md, group 2, P1–P5). The probe does not know which operation ran
// it, so the lead is left to the caller (usererror.Because), and the node is
// usererror.SelectedNode until the workbench names it.

// probeTally is how many services failed, and the way most of them failed.
func probeTally(failures []probeFailure) (n int, how string) {
	count := map[string]int{}
	for _, failure := range failures {
		if failure.service == "" {
			continue
		}
		n++
		count[failure.reason]++
		if count[failure.reason] > count[how] {
			how = failure.reason
		}
	}
	return n, how
}

func probeWait() int { return int(exitProbeRequestTimeout / time.Second) }

// P1: neither the echo services nor the connectivity check answered.
func probeUnreachable(failures []probeFailure, cause error) *usererror.Failure {
	n, how := probeTally(failures)
	return usererror.Because(fmt.Sprintf("原因是经所选节点连不上任何检测地址（%d 个出口检测服务和连通性检查都失败：%s）", n, how),
		"换一个节点", cause, usererror.ToolSettingsNetwork, usererror.ToolReapply, usererror.ToolDiagnostics)
}

// P2: every service failed in the TLS handshake, the node being reachable.
func probeTLS(failures []probeFailure, timedOut bool, cause error) *usererror.Failure {
	n, _ := probeTally(failures)
	reason := fmt.Sprintf("原因是经所选节点与 %d 个出口检测服务的 TLS 握手都超时（每个 %d 秒）", n, probeWait())
	if !timedOut {
		reason = fmt.Sprintf("原因是经所选节点与出口检测服务的 TLS 握手都中断（%d 个都在握手时断开）", n)
	}
	return usererror.Because(reason, "开启「TLS ClientHello 分段传输兼容模式」后重试", cause,
		usererror.ToolSplitRetry, usererror.ToolSettingsNetwork, usererror.ToolDiagnostics)
}

// P3: the node reaches the internet, but no echo service answers through it.
func probeBlocked(failures []probeFailure, cause error) *usererror.Failure {
	n, _ := probeTally(failures)
	return usererror.Because(fmt.Sprintf("原因是所选节点能上网，但 %d 个出口检测服务都没有响应（每个 %d 秒）", n, probeWait()),
		"换一个节点", cause, usererror.ToolSettingsNetwork, usererror.ToolReapply)
}

// P4: the node answered the proxy request with a web page.
func probeSchemeMismatch(failures []probeFailure, cause error) *usererror.Failure {
	n, _ := probeTally(failures)
	return usererror.Because(fmt.Sprintf("原因是所选节点返回了网页而不是代理数据（%d 个出口检测服务都收到明文 HTTP）", n),
		"核对节点的密码、协议和端口后重新导入", cause, usererror.ToolSettingsNetwork, usererror.ToolDiagnostics)
}

// P5: the proxy step failed for every service: the local core could not be
// reached, or it refused the CONNECT because the node could not.
func probeCoreUnreachable(failures []probeFailure, cause error) *usererror.Failure {
	n, how := probeTally(failures)
	if strings.HasPrefix(how, "代理拒绝 CONNECT 请求") {
		return usererror.Because(fmt.Sprintf("原因是代理核心没能经所选节点建立连接（%d 个出口检测服务都失败：%s）", n, strings.TrimSuffix(how, "；请求不会改为直连")),
			"换一个节点", cause, usererror.ToolSettingsNetwork, usererror.ToolReapply, usererror.ToolDiagnostics)
	}
	return usererror.Because("原因是连不上本机代理核心", "重新应用节点", cause,
		usererror.ToolReapply, usererror.ToolLog, usererror.ToolDiagnostics)
}

// probeTimedOut: every service timed out, and whether the node reaches the
// internet could not be told.
func probeTimedOut(failures []probeFailure, cause error) *usererror.Failure {
	n, _ := probeTally(failures)
	return usererror.Because(fmt.Sprintf("原因是经所选节点访问 %d 个出口检测服务都超时（每个 %d 秒）", n, probeWait()),
		"换一个节点", cause, usererror.ToolSettingsNetwork, usererror.ToolReapply, usererror.ToolDiagnostics)
}

// probeMixed: no service returned an exit; one way of failing is named in
// the parentheses, several are listed.
func probeMixed(failures []probeFailure, cause error) *usererror.Failure {
	n, how := probeTally(failures)
	detail := summarizeProbeFailures(failures)
	if count := map[string]bool{}; true {
		for _, failure := range failures {
			if failure.service != "" {
				count[failure.reason] = true
			}
		}
		if len(count) == 1 {
			detail = how
		}
	}
	return usererror.Because(fmt.Sprintf("原因是 %d 个出口检测服务都没有返回出口（%s）", n, detail),
		"换一个节点", cause, usererror.ToolSettingsNetwork, usererror.ToolReapply, usererror.ToolDiagnostics)
}

// P6: the core logged a failed REALITY handshake with a node while the probe
// ran (#809). The tunnel never came up, so the services' timeouts follow from
// it and ClientHello splitting would not help. The hop is named only when the
// probe went through the applied route and the core's records agree.
func probeReality(hop string, cause error) *usererror.Failure {
	reason := "原因是代理节点的 REALITY 认证没有通过（代理核心日志：REALITY authentication failed）"
	switch hop {
	case managedproxy.RealityHopSingle:
		reason = "原因是所选节点的 REALITY 认证没有通过（代理核心日志：REALITY authentication failed）"
	case managedproxy.RealityHopRelay:
		reason = "原因是代理链前置节点的 REALITY 认证没有通过（代理核心日志：REALITY authentication failed）"
	case managedproxy.RealityHopExit:
		reason = "原因是代理链出口节点的 REALITY 认证没有通过（代理核心日志：REALITY authentication failed）"
	}
	return usererror.Because(reason, "核对节点的 public-key、short-id 和 servername 后重新导入", cause,
		usererror.ToolSettingsNetwork, usererror.ToolDiagnostics)
}

// splitAlreadyOn: with ClientHello splitting on already, a TLS failure is
// the node's, so the suggestion is another node.
func splitAlreadyOn(err error) error {
	f, ok := usererror.AsFailure(err)
	if !ok || len(f.Tools) == 0 || f.Tools[0] != usererror.ToolSplitRetry {
		return err
	}
	f.Next, f.Tools = "换一个节点", []string{usererror.ToolSettingsNetwork, usererror.ToolReapply}
	return err
}

// shortProbeReason keeps a service's failure to its cause: the operation and
// the advice an inner usererror added go.
func shortProbeReason(text string) string {
	text = strings.TrimRight(text, "。；;")
	if _, rest, ok := strings.Cut(text, "失败："); ok && !strings.Contains(rest, "失败：") {
		text = rest
	}
	if before, _, ok := strings.Cut(text, "。"); ok {
		text = before
	}
	return text
}
