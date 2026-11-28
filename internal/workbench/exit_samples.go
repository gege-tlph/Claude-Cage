package workbench

import (
	"fmt"
	"strings"

	"github.com/sylvinus/agent-vm/internal/diagnostics"
	"github.com/sylvinus/agent-vm/internal/usererror"
)

// All guest platforms return the same observation contract. Do not infer a
// transport failure from an unavailable sample or expose raw guest output.
func incompleteExitSamples(hostIP string, hostErr error, terminal networkObservation, terminalErr error, browser networkObservation, browserErr error) string {
	missing, details := exitSampleGaps(hostIP, hostErr, terminal, terminalErr, browser, browserErr)
	if len(missing) == 0 {
		return ""
	}
	// Each sample's own failure goes to the log; the check names only the
	// samples that are missing (failure-text-audit P9).
	diagnostics.Log("warn", "exit-samples", strings.Join(details, "; "))
	return joinNames(missing) + "没有取到出口"
}

// exitSampleGaps names the samples without an exit, and why each failed
// (「浏览器：检测通道返回 HTTP 503，未取得出口地址」), redacted.
func exitSampleGaps(hostIP string, hostErr error, terminal networkObservation, terminalErr error, browser networkObservation, browserErr error) (missing, details []string) {
	for _, sample := range []struct {
		name        string
		observation networkObservation
		err         error
	}{
		{"电脑本机", networkObservation{Status: 200, IP: hostIP}, hostErr},
		{"终端", terminal, terminalErr},
		{"浏览器", browser, browserErr},
	} {
		var reason string
		switch {
		case sample.err != nil:
			reason = diagnostics.Redact(usererror.Display("读取采样", sample.err, "").Error())
		case sample.observation.Status == 0:
			reason = "没有收到检测通道的响应"
		case sample.observation.Status != 200:
			if detail := diagnosticFailureDetail(sample.observation); detail != "" {
				reason = fmt.Sprintf("检测通道返回 HTTP %d（%s），未取得出口地址", sample.observation.Status, detail)
			} else {
				reason = fmt.Sprintf("检测通道返回 HTTP %d，未取得出口地址", sample.observation.Status)
			}
		case sample.observation.IP == "":
			reason = "检测通道没有返回出口地址"
		}
		if reason != "" {
			missing = append(missing, sample.name)
			details = append(details, sample.name+"："+reason)
		}
	}
	return missing, details
}

// joinNames: 「电脑本机」「电脑本机和浏览器」「电脑本机、终端和浏览器」.
func joinNames(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], "、") + "和" + names[len(names)-1]
}

// diagnosticFailureDetail maps fixed reason tokens emitted by the internal
// diagnostic endpoint to concise labels so users can distinguish internal gate
// 503 responses from an upstream HTTP 503. Arbitrary strings are ignored.
func diagnosticFailureDetail(obs networkObservation) string {
	switch obs.Reason {
	case "timeout":
		return "内部检测超时"
	case "tls_timeout":
		return "内部检测 TLS 握手超时"
	case "tls_handshake":
		return "内部检测 TLS 握手失败"
	case "proxy_connect":
		return "内部检测代理连接失败"
	case "probe_failed":
		return "内部检测未能经代理取得出口"
	default:
		if obs.Status == 503 && obs.Error == "proxy_unavailable" {
			return "内部检测未能经代理取得出口"
		}
		return ""
	}
}
