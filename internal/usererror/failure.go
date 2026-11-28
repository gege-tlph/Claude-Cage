package usererror

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Failure levels: what the user can still do after it.
const (
	// LevelError: the operation failed; the environment cannot be used, or
	// its state is unknown.
	LevelError = "error"
	// LevelWarn: the operation did not finish, but the environment works.
	LevelWarn = "warn"
	// LevelInfo: nothing failed; the user has a step to take.
	LevelInfo = "info"
)

// Tools a failure can offer as buttons. The interface draws each with its
// icon and runs it; the backend only names them.
const (
	ToolStart = "start"
	ToolStop  = "stop"
	// ToolRestart stops the environment, waits, then starts it: one button
	// that carries the flow through.
	ToolRestart         = "restart"
	ToolContinueInstall = "continue-install"
	ToolDiagnostics     = "diagnostics"
	ToolLog             = "log"
	ToolSettingsNetwork = "settings:network"
	ToolSettingsProject = "settings:project"
	ToolResources       = "settings:resources"
	// Proxy and network (group 2): each carries the flow through, see
	// ui/src/failure-card.jsx.
	ToolReapply     = "reapply"
	ToolRecheck     = "recheck"
	ToolChecklist   = "checklist"
	ToolRefresh     = "refresh"
	ToolRefreshHost = "refresh-host"
	ToolSplitRetry  = "split-retry"
	ToolSettingsDNS = "settings:dns"
	// Install (group 3): turn on the install TLS 1.2 mode, then continue.
	ToolTLS12Retry = "tls12-retry"
	// Claude Desktop (group 4).
	ToolOpenDesktop    = "open-desktop"
	ToolReopenDesktop  = "reopen-desktop"
	ToolInstallDesktop = "install-desktop"
	ToolUpdateDesktop  = "update-desktop"
)

// SelectedNode stands for the node a reason went through until the
// workbench names it (Subject): 「经所选节点…」 becomes 「经「澳大利亚 01」…」.
const SelectedNode = "所选节点"

// rawLimit bounds the last error line a failure quotes.
const rawLimit = 120

// Failure is a failure the interface shows in three lines:
//
//	{Lead}，{Reason}（最后一条错误：{Raw}）
//	解决建议：{Next}            (下一步： for LevelInfo)
//	[Tools…]
//
// Reason starts with 原因是 for a known cause, or reads 原因未识别 (the job
// adds where it got stuck). Every text matches the interface's own words.
type Failure struct {
	Level  string   `json:"level"`
	Lead   string   `json:"lead"`
	Reason string   `json:"reason"`
	Raw    string   `json:"raw,omitempty"`
	Next   string   `json:"next,omitempty"`
	Tools  []string `json:"tools,omitempty"`
	// Unknown marks a reason that was not recognized; the job fills in the
	// stage it was stuck at.
	Unknown bool `json:"unknown,omitempty"`
	// Subject is the node SelectedNode in Reason stands for, shown quoted.
	Subject string `json:"subject,omitempty"`
	// Target is what a tool acts on, such as the subscription to update.
	Target string `json:"target,omitempty"`
	cause  error
}

// Fail builds an error-level failure of action: 「{action}失败，{reason}」.
// An empty action leaves the lead to the caller, as Because does.
func Fail(action, reason, next string, cause error, tools ...string) *Failure {
	lead := ""
	if action != "" {
		lead = LeadOf(action)
	}
	return &Failure{Level: LevelError, Lead: lead, Reason: reason, Next: next, Tools: tools, cause: cause}
}

// Because is a failure whose operation the caller names later (Lead empty):
// the exit probe does not know whether a start or a node switch ran it.
func Because(reason, next string, cause error, tools ...string) *Failure {
	return &Failure{Level: LevelError, Reason: reason, Next: next, Tools: tools, cause: cause}
}

// Notice is a nothing-failed failure: the user has a step to take
// (LevelInfo, 「下一步」). lead is the whole first part, such as
// 「启动环境前需要代理节点」.
func Notice(lead, reason, next string, cause error, tools ...string) *Failure {
	return &Failure{Level: LevelInfo, Lead: lead, Reason: reason, Next: next, Tools: tools, cause: cause}
}

// LeadOf is 「{action}失败」, with the space the typography wants after a
// Latin name: 「打开 Claude Desktop 失败」.
func LeadOf(action string) string {
	if last := action[len(action)-1]; last < 0x80 && (last >= '0' && last <= '9' || last >= 'A' && last <= 'Z' || last >= 'a' && last <= 'z') {
		return action + " 失败"
	}
	return action + "失败"
}

// Unrecognized is the failure of action whose cause was not recognized; raw
// is the last error line of its output, shortened.
func Unrecognized(action, raw, next string, cause error, tools ...string) *Failure {
	f := Fail(action, "原因未识别", next, cause, tools...)
	f.Raw, f.Unknown = shorten(raw), true
	return f
}

// WithLevel sets the level and returns f.
func (f *Failure) WithLevel(level string) *Failure {
	f.Level = level
	return f
}

// StuckAt names the stage an unrecognized failure was stuck at: 「卡在启动浏览器，原因未识别」.
func (f *Failure) StuckAt(stage string) {
	stage = strings.TrimSpace(stage)
	if f.Unknown && stage != "" && !strings.HasPrefix(f.Reason, "卡在") {
		f.Reason = "卡在" + stage + "，" + f.Reason
	}
}

// NextLabel is 解决建议 for a failure, 下一步 for a notice.
func (f *Failure) NextLabel() string {
	if f.Level == LevelInfo {
		return "下一步"
	}
	return "解决建议"
}

// NamedReason is Reason with SelectedNode replaced by the quoted Subject.
func (f *Failure) NamedReason() string {
	if f.Subject == "" {
		return f.Reason
	}
	return strings.Replace(f.Reason, SelectedNode, "「"+f.Subject+"」", 1)
}

func (f *Failure) Error() string {
	var b strings.Builder
	b.WriteString(f.Lead)
	if reason := f.NamedReason(); reason != "" {
		if f.Lead != "" {
			b.WriteString("，")
		}
		b.WriteString(reason)
	}
	if f.Raw != "" {
		b.WriteString("（最后一条错误：" + f.Raw + "）")
	}
	b.WriteString("。")
	if f.Next != "" {
		b.WriteString(f.NextLabel() + "：" + strings.TrimRight(f.Next, "。") + "。")
	}
	return b.String()
}

func (f *Failure) Unwrap() error { return f.cause }

// AsFailure finds a Failure in err's chain.
func AsFailure(err error) (*Failure, bool) {
	var f *Failure
	ok := errors.As(err, &f)
	return f, ok
}

// Duration is a wait as the interface writes it: 「10 分钟」「30 秒」「1 小时 5 分钟」.
func Duration(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d <= 0:
		return ""
	case d < time.Minute:
		return fmt.Sprintf("%d 秒", int(d.Seconds()))
	case d < time.Hour:
		if s := int(d.Seconds()) % 60; s != 0 {
			return fmt.Sprintf("%d 分 %d 秒", int(d.Minutes()), s)
		}
		return fmt.Sprintf("%d 分钟", int(d.Minutes()))
	}
	if m := int(d.Minutes()) % 60; m != 0 {
		return fmt.Sprintf("%d 小时 %d 分钟", int(d.Hours()), m)
	}
	return fmt.Sprintf("%d 小时", int(d.Hours()))
}

// shorten keeps the first rawLimit characters of one line.
func shorten(line string) string {
	line = strings.TrimSpace(line)
	if utf8.RuneCountInString(line) <= rawLimit {
		return line
	}
	runes := []rune(line)
	return string(runes[:rawLimit]) + "…"
}
