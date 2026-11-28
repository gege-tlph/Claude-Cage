package cage

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/sylvinus/agent-vm/internal/usererror"
)

func sixFailures(reason string) []probeFailure {
	failures := make([]probeFailure, 6)
	for i := range failures {
		failures[i] = probeFailure{service: "服务", reason: reason}
	}
	return failures
}

// Each way the exit probe fails reads as one cause with one next step; the
// lead is the caller's, the node is named later (failure-text-audit P1–P5).
func TestExitProbeFailuresReadAsOneCause(t *testing.T) {
	for _, tc := range []struct {
		name   string
		f      *usererror.Failure
		reason string
		tools  []string
	}{
		{"unreachable", probeUnreachable(sixFailures("连接在返回完整结果前中断"), ErrProxyConnectFailed),
			"原因是经所选节点连不上任何检测地址（6 个出口检测服务和连通性检查都失败：连接在返回完整结果前中断）", []string{"settings:network", "reapply", "diagnostics"}},
		{"tls timeout", probeTLS(sixFailures("等待响应超时"), true, ErrTLSHandshakeFailed),
			"原因是经所选节点与 6 个出口检测服务的 TLS 握手都超时（每个 7 秒）", []string{"split-retry", "settings:network", "diagnostics"}},
		{"tls eof", probeTLS(sixFailures("连接在返回完整结果前中断"), false, ErrTLSHandshakeFailed),
			"原因是经所选节点与出口检测服务的 TLS 握手都中断（6 个都在握手时断开）", []string{"split-retry", "settings:network", "diagnostics"}},
		{"blocked", probeBlocked(sixFailures("等待响应超时"), ErrExitProbeBlocked),
			"原因是所选节点能上网，但 6 个出口检测服务都没有响应（每个 7 秒）", []string{"settings:network", "reapply"}},
		{"web page", probeSchemeMismatch(sixFailures("明文"), ErrTLSHandshakeFailed),
			"原因是所选节点返回了网页而不是代理数据（6 个出口检测服务都收到明文 HTTP）", []string{"settings:network", "diagnostics"}},
		{"core", probeCoreUnreachable(sixFailures("目标服务拒绝了连接"), ErrProxyConnectFailed),
			"原因是连不上本机代理核心", []string{"reapply", "log", "diagnostics"}},
		{"connect refused by core", probeCoreUnreachable(sixFailures("代理拒绝 CONNECT 请求（HTTP 502）；请求不会改为直连"), ErrProxyConnectFailed),
			"原因是代理核心没能经所选节点建立连接（6 个出口检测服务都失败：代理拒绝 CONNECT 请求（HTTP 502））", []string{"settings:network", "reapply", "diagnostics"}},
		{"timed out", probeTimedOut(sixFailures("等待响应超时"), ErrExitProbeTimeout),
			"原因是经所选节点访问 6 个出口检测服务都超时（每个 7 秒）", []string{"settings:network", "reapply", "diagnostics"}},
		{"one way, mixed services", probeMixed(sixFailures("检测服务返回 HTTP 503"), nil),
			"原因是 6 个出口检测服务都没有返回出口（检测服务返回 HTTP 503）", []string{"settings:network", "reapply", "diagnostics"}},
	} {
		if tc.f.Lead != "" || tc.f.Reason != tc.reason || !reflect.DeepEqual(tc.f.Tools, tc.tools) || tc.f.Level != usererror.LevelError {
			t.Errorf("%s: %+v, want %q %v", tc.name, tc.f, tc.reason, tc.tools)
		}
	}
	if f := probeUnreachable(sixFailures("x"), ErrProxyConnectFailed); !errors.Is(f, ErrProxyConnectFailed) {
		t.Fatal("the sentinel was lost")
	}
	if probeWait() != int(exitProbeRequestTimeout/time.Second) {
		t.Fatal("wait")
	}
}

// With ClientHello splitting already on, a TLS failure suggests another node.
func TestSplitAlreadyOnSuggestsAnotherNode(t *testing.T) {
	err := splitAlreadyOn(probeTLS(sixFailures("x"), true, ErrTLSHandshakeFailed))
	f, _ := usererror.AsFailure(err)
	if f.Next != "换一个节点" || !reflect.DeepEqual(f.Tools, []string{"settings:network", "reapply"}) {
		t.Fatalf("%+v", f)
	}
	other := probeBlocked(sixFailures("x"), nil)
	if splitAlreadyOn(other); other.Next != "换一个节点" || other.Tools[1] != "reapply" {
		t.Fatalf("a non-TLS failure changed: %+v", other)
	}
}

func TestShortProbeReasonKeepsTheCause(t *testing.T) {
	for in, want := range map[string]string{
		"通过所选代理查询出口 IP失败：等待响应超时。请检查网络与代理，再重新检测。": "等待响应超时",
		"查询出口 IP 失败：检测服务返回 HTTP 503":             "检测服务返回 HTTP 503",
		"连接在返回完整结果前中断":                           "连接在返回完整结果前中断",
	} {
		if got := shortProbeReason(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

// The network check's failures name the item, what was seen, and its tools;
// evidence that names the cause is the reason itself (P7–P10).
func TestCheckFailureNamesTheItem(t *testing.T) {
	for _, tc := range []struct {
		check        SafetyCheck
		reason, next string
		tools        []string
	}{
		{SafetyCheck{ID: "proxy_transport", State: CheckUnknown, Evidence: "环境未能完成代理出口查询（检测通道 HTTP 503），尚不能确认代理连接可用", Remedy: "x"},
			"原因是代理连接未能确认（检测通道 HTTP 503）", "重新应用节点", []string{"reapply", "checklist", "diagnostics"}},
		{SafetyCheck{ID: "dns_transport", State: CheckUnknown, Evidence: "经所选节点用不了加密 DNS（1.1.1.1、8.8.8.8）", Remedy: "检查 DNS 配置"},
			"原因是经所选节点用不了加密 DNS（1.1.1.1、8.8.8.8）", "在加密 DNS 中添加备用端点", []string{"settings:dns", "recheck"}},
		{SafetyCheck{ID: "exit_consistency", State: CheckFailed, Evidence: "出口位于中国大陆", Remedy: "x"},
			"原因是出口位于中国大陆", "换一个境外节点", []string{"settings:network"}},
		{SafetyCheck{ID: "exit_consistency", State: CheckFailed, Evidence: "出口超出 IP 变化规则允许的国家或地区", Remedy: "x"},
			"原因是出口超出 IP 变化规则允许的国家或地区", "换一个符合 IP 变化规则的节点", []string{"settings:network"}},
		{SafetyCheck{ID: "exit_consistency", State: CheckUnknown, Evidence: "电脑本机和浏览器没有取到出口", Remedy: "x"},
			"原因是出口 IP 与地区未能确认（电脑本机和浏览器没有取到出口）", "重新检测", []string{"recheck", "checklist", "diagnostics"}},
		{SafetyCheck{ID: "browser_settings", Title: "浏览器时区与语言", State: CheckFailed, Evidence: "浏览器语言与设置不一致。第二句。", Remedy: "更新环境组件，再重新检测"},
			"原因是浏览器时区与语言未通过（浏览器语言与设置不一致）", "更新环境组件，再重新检测", []string{"recheck", "checklist", "diagnostics"}},
	} {
		f := checkFailure(tc.check)
		if f.Lead != "联网检查未通过" || f.Reason != tc.reason || f.Next != tc.next || !reflect.DeepEqual(f.Tools, tc.tools) {
			t.Errorf("%s: %+v, want %q / %q %v", tc.check.ID, f, tc.reason, tc.next, tc.tools)
		}
	}
	if f := missingEvidence("dns_transport"); f.Error() != "联网检查未通过，原因是DNS 解析的结果缺失或已过期。解决建议：重新检测。" {
		t.Fatalf("%q", f.Error())
	}
}
