package cage

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/sylvinus/agent-vm/internal/diagnostics"
	"github.com/sylvinus/agent-vm/internal/usererror"
)

// The network check's failures in three lines (notes/failure-text-audit-
// 2026-11-28.md, group 2, P7–P10): 「联网检查未通过，原因是{检查项}{未通过 /
// 未能确认}（{证据}）」, the item's own suggestion, and the tools for it.
// 「外网保持关闭」 is gone: the status bar shows 「外网已关闭」 throughout.

const checkLead = "联网检查未通过"

// evidenceLimit bounds the evidence a reason quotes.
const evidenceLimit = 60

// causeEvidence is evidence that already names the cause, so it is the
// reason itself: 「原因是出口位于中国大陆」, not 「原因是出口 IP 与地区未通过（出口位于中国大陆）」.
var causeEvidence = []string{"出口位于", "出口命中", "出口超出", "出口已偏离", "经所选节点"}

func checkFailure(check SafetyCheck) *usererror.Failure {
	state := "未通过"
	if check.State == CheckUnknown {
		state = "未能确认"
	}
	evidence := firstClause(diagnostics.Redact(check.Evidence))
	if check.ID == "dns_transport" && strings.HasPrefix(check.Evidence, "经所选节点用不了加密 DNS") {
		// Built from the configured DoH endpoints, public resolvers the
		// redaction would turn into [ip].
		evidence = firstClause(check.Evidence)
	}
	direct := false
	for _, prefix := range causeEvidence {
		direct = direct || strings.HasPrefix(evidence, prefix)
	}
	reason := "原因是" + evidence
	if !direct {
		reason = "原因是" + safetyCheckTitle(check) + state
		if detail := evidenceDetail(evidence); detail != "" {
			reason += "（" + detail + "）"
		}
	}
	next := strings.TrimRight(diagnostics.Redact(check.Remedy), "。；;")
	if next == "" {
		next = "重新检测"
	}
	tools := []string{usererror.ToolRecheck, usererror.ToolChecklist, usererror.ToolDiagnostics}
	switch {
	case check.ID == "proxy_transport":
		next, tools = "重新应用节点", []string{usererror.ToolReapply, usererror.ToolChecklist, usererror.ToolDiagnostics}
	case check.ID == "dns_transport" && strings.HasPrefix(evidence, "经所选节点"):
		next, tools = "在加密 DNS 中添加备用端点", []string{usererror.ToolSettingsDNS, usererror.ToolRecheck}
	case check.ID == "exit_consistency" && check.State == CheckFailed:
		next, tools = "换一个符合 IP 变化规则的节点", []string{usererror.ToolSettingsNetwork}
		if strings.Contains(evidence, "中国大陆") {
			next = "换一个境外节点"
		}
	case check.ID == "exit_consistency":
		next = "重新检测"
	}
	return &usererror.Failure{Level: usererror.LevelError, Lead: checkLead, Reason: reason, Next: next, Tools: tools}
}

// missingEvidence: an item's record is missing, stale or incomplete.
func missingEvidence(id string) *usererror.Failure {
	return &usererror.Failure{Level: usererror.LevelError, Lead: checkLead, Reason: "原因是" + safetyCheckTitle(SafetyCheck{ID: id}) + "的结果缺失或已过期",
		Next: "重新检测", Tools: []string{usererror.ToolRecheck, usererror.ToolChecklist}}
}

// evidenceInner is the value a long sentence of evidence carries in
// parentheses: 「环境未能完成代理出口查询（检测通道 HTTP 503），尚不能确认…」.
var evidenceInner = regexp.MustCompile(`^[^（]{4,}（([^（）]+)）`)

// evidenceDetail keeps evidence short in a reason: its value in parentheses
// when the sentence is long.
func evidenceDetail(evidence string) string {
	if m := evidenceInner.FindStringSubmatch(evidence); m != nil && utf8.RuneCountInString(evidence) > 24 {
		return m[1]
	}
	return evidence
}

// firstClause keeps evidence to its first sentence, at most evidenceLimit
// characters.
func firstClause(text string) string {
	text = strings.TrimSpace(text)
	if before, _, ok := strings.Cut(text, "。"); ok {
		text = before
	}
	if before, _, ok := strings.Cut(text, "；"); ok {
		text = before
	}
	text = strings.TrimRight(text, "。；;，, ")
	if utf8.RuneCountInString(text) > evidenceLimit {
		text = string([]rune(text)[:evidenceLimit]) + "…"
	}
	return text
}
