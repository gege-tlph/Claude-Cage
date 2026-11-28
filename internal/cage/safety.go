package cage

import (
	"errors"
	"time"

	"github.com/sylvinus/agent-vm/internal/exitpolicy"
)

// SafetyPolicyVersion changes when the mandatory evidence contract changes.
// Reports authorize one running VM with one applied configuration.
const SafetyPolicyVersion = "cage-safety/3"
const SafetyReportLifetime = 24 * time.Hour

type CheckState = exitpolicy.State

const (
	CheckPassed  = exitpolicy.Passed
	CheckFailed  = exitpolicy.Failed
	CheckUnknown = exitpolicy.Unknown
)

type SafetyCheck struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	State     CheckState `json:"state"`
	Critical  bool       `json:"critical"`
	Evidence  string     `json:"evidence"`
	Remedy    string     `json:"remedy,omitempty"`
	CheckedAt time.Time  `json:"checked_at"`
}

type SafetyReport struct {
	Policy        string        `json:"policy"`
	ID            string        `json:"id"`
	RunID         string        `json:"run_id"`
	CheckID       string        `json:"check_id"`
	Configuration string        `json:"configuration"`
	CreatedAt     time.Time     `json:"created_at"`
	Checks        []SafetyCheck `json:"checks"`
}

var requiredSafetyChecks = [...]string{
	"egress_policy", "proxy_transport", "dns_transport", "guest_settings",
	"browser_settings", "filesystem_boundary", "management_boundary",
	"fail_closed", "udp_policy", "ipv6_policy", "exit_consistency",
}

// Authorizes rejects missing, duplicate, unknown, stale or mismatched evidence.
// The required IDs are defined here, never supplied by the frontend or a provider.
func (r SafetyReport) Authorizes(runID, configuration string, now time.Time) error {
	if r.Policy != SafetyPolicyVersion || r.ID == "" || r.CheckID == "" || runID == "" || configuration == "" ||
		r.RunID != runID || r.Configuration != configuration {
		return errors.New("检测报告不属于当前环境配置，请重新检测")
	}
	if r.CreatedAt.IsZero() || r.CreatedAt.After(now.Add(time.Minute)) || !now.Before(r.CreatedAt.Add(SafetyReportLifetime)) {
		return errors.New("检测报告已过期，请重新检测")
	}
	seen := make(map[string]SafetyCheck, len(r.Checks))
	for _, check := range r.Checks {
		if _, duplicate := seen[check.ID]; duplicate {
			return errors.New("检测报告包含重复项目")
		}
		if check.State != CheckPassed && check.State != CheckFailed && check.State != CheckUnknown {
			return errors.New("检测状态无效")
		}
		seen[check.ID] = check
		if check.Critical && check.State != CheckPassed {
			return checkFailure(check)
		}
	}
	for _, id := range requiredSafetyChecks {
		check, found := seen[id]
		if !found || !check.Critical || check.State != CheckPassed || check.Evidence == "" ||
			check.CheckedAt.Before(r.CreatedAt) || check.CheckedAt.After(now.Add(time.Minute)) {
			return missingEvidence(id)
		}
	}
	return nil
}

func safetyCheckTitle(check SafetyCheck) string {
	if check.Title != "" && check.Title != check.ID {
		return check.Title
	}
	titles := map[string]string{
		"egress_policy": "环境外出站约束", "proxy_transport": "代理连接", "dns_transport": "DNS 解析", "guest_settings": "系统时区与语言", "browser_settings": "浏览器时区与语言", "filesystem_boundary": "项目目录共享", "management_boundary": "宿主管理接口隔离", "fail_closed": "代理故障断网", "udp_policy": "UDP 出站约束", "ipv6_policy": "IPv6 出站约束", "exit_consistency": "出口 IP 与地区",
	}
	if title := titles[check.ID]; title != "" {
		return title
	}
	return "必需检测项目"
}
