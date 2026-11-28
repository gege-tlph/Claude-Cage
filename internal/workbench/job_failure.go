package workbench

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/sylvinus/agent-vm/internal/cage"
	"github.com/sylvinus/agent-vm/internal/managedproxy"
	"github.com/sylvinus/agent-vm/internal/usererror"
)

// describeFailure completes a job's failure where the code that failed could
// not: the operation (usererror.Because leaves it empty) and the node a
// reason went through (usererror.SelectedNode). Known errors that are still
// plain text become failures first (proxy_failure.go, setup_failure.go,
// desktop_failure.go).
func (s *Server) describeFailure(kind, initiator string, err error) error {
	if err == nil {
		return nil
	}
	err = knownFailure(kind, err)
	f, ok := usererror.AsFailure(err)
	if !ok {
		return err
	}
	if f.Lead == "" {
		var check networkCheckError
		switch {
		case kind == "proxy" || kind == "proxy-update":
			f.Lead = usererror.LeadOf(jobName(kind))
		case errors.As(err, &check), kind == "safety", kind == "network-recovery":
			f.Lead = "联网检查未通过"
		default:
			f.Lead = usererror.LeadOf(jobName(kind))
		}
	}
	// A start carries on as a start: applying the node again alone would end
	// at 「节点已应用」 with the environment still stopped.
	if kind == "start" {
		for i, tool := range f.Tools {
			if tool == usererror.ToolReapply {
				f.Tools[i] = usererror.ToolStart
			}
		}
	}
	// First run calls a start 「安装」, carried on by 「继续安装」 (group 3).
	if initiator == jobInitiatorSetup && (kind == "start" || kind == "setup") {
		if f.Lead == usererror.LeadOf(jobName(kind)) {
			f.Lead = "安装失败"
		}
		for i, tool := range f.Tools {
			if tool == usererror.ToolStart {
				f.Tools[i] = usererror.ToolContinueInstall
			}
		}
	}
	if len(f.Tools) == 0 {
		if tool := retryTools[kind]; tool != "" {
			f.Tools = []string{tool}
		}
	}
	if f.Subject == "" && strings.Contains(f.Reason, usererror.SelectedNode) {
		f.Subject = s.exitNodeName()
	}
	return err
}

// retryTools carry a job's flow through again, for a failure that names no
// tool of its own.
var retryTools = map[string]string{
	"start": usererror.ToolStart, "stop": usererror.ToolStop, "setup": usererror.ToolContinueInstall,
	"safety": usererror.ToolRecheck, "network-recovery": usererror.ToolRecheck, "proxy-update": usererror.ToolRefresh,
	"claude-desktop-start": usererror.ToolOpenDesktop, "claude-desktop-install": usererror.ToolInstallDesktop, "claude-desktop-update": usererror.ToolUpdateDesktop,
}

// knownFailure turns known plain errors into failures, by the job kind.
func knownFailure(kind string, err error) error {
	if _, ok := usererror.AsFailure(err); ok {
		return err
	}
	if f := proxyRequirementFailure(err); f != nil {
		return f
	}
	if f := coreFailure(err); f != nil {
		return f
	}
	return err
}

// failureHeader hands a request's failure to the interface in three parts
// (X-Cage-Failure, base64url JSON) next to the joined text in the body, so
// a page that is not watching a job can draw it the same way.
func failureHeader(w http.ResponseWriter, err error) {
	f, ok := usererror.AsFailure(err)
	if !ok {
		return
	}
	if data, e := json.Marshal(f); e == nil {
		w.Header().Set("X-Cage-Failure", base64.RawURLEncoding.EncodeToString(data))
	}
}

// exitNodeName is the name of the applied exit node, "" without one.
func (s *Server) exitNodeName() string {
	p, err := managedproxy.Load(s.profile)
	if err != nil {
		return ""
	}
	if node, ok := p.Find(p.Selected); ok {
		return node.Name
	}
	return ""
}

// dohUnavailableEvidence is the DNS check's evidence when the fixed DoH query
// through the node failed: which endpoints, and how (P7).
func dohUnavailableEvidence(cfg cage.Config, observation networkObservation) string {
	var hosts []string
	for _, endpoint := range cfg.DNSEndpoints() {
		hosts = append(hosts, cage.DNSHost(endpoint))
	}
	evidence := "经所选节点用不了加密 DNS（" + strings.Join(hosts, "、")
	if detail := diagnosticFailureDetail(observation); detail != "" {
		evidence += "，" + detail
	}
	return evidence + "）"
}
