package workbench

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/sylvinus/agent-vm/internal/managedproxy"
	"github.com/sylvinus/agent-vm/internal/usererror"
)

// subscriptionDownload is how fetchProxySource reports a failed download:
// a fixed message with the transport error behind it.
type subscriptionDownload struct{ cause error }

func (e subscriptionDownload) Error() string {
	return "订阅下载失败，请检查下载路径和服务地址"
}
func (e subscriptionDownload) Unwrap() error { return e.cause }

type timeoutError struct{}

func (timeoutError) Error() string   { return "Client.Timeout exceeded" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

// A failed subscription download names why, through which network, and what
// is still in use (failure-text-audit P11).
func TestSubscriptionFailureNamesWhy(t *testing.T) {
	core := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connectex: refused")}
	for _, tc := range []struct {
		name     string
		err      error
		viaHost  bool
		previous int
		reason   string
		tools    []string
	}{
		{"forbidden", errors.New("订阅服务器返回 HTTP 403"), false, 24,
			"原因是订阅服务器返回 HTTP 403，仍在用上次的 24 个节点", []string{"settings:network", "refresh"}},
		{"server busy", errors.New("订阅服务器返回 HTTP 503"), false, 0,
			"原因是订阅服务器返回 HTTP 503", []string{"refresh"}},
		{"core down", subscriptionDownload{fmt.Errorf("连接指定 SOCKS5 代理失败：%w", core)}, false, 3,
			"原因是连不上本机代理核心，仍在用上次的 3 个节点", []string{"reapply", "refresh-host"}},
		{"node cannot reach it", subscriptionDownload{errors.New("连接指定 SOCKS5 代理失败：未能取得可识别的错误原因。请检查代理；请求不会改为直连。")}, false, 24,
			"原因是经所选节点连不上订阅服务器，仍在用上次的 24 个节点", []string{"refresh-host", "refresh"}},
		{"timeout through the node", subscriptionDownload{timeoutError{}}, false, 0,
			"原因是经所选节点连接订阅服务器超时（30 秒）", []string{"refresh-host", "refresh"}},
		{"timeout through the host", subscriptionDownload{timeoutError{}}, true, 0,
			"原因是经电脑本机网络连接订阅服务器超时（30 秒）", []string{"refresh"}},
		{"dns", subscriptionDownload{&net.DNSError{Err: "no such host", Name: "sub.example"}}, true, 0,
			"原因是经电脑本机网络解析不了订阅服务器的域名", []string{"refresh"}},
		{"too big", errors.New("订阅下载中断或超过 8 MiB"), false, 0,
			"原因是订阅下载中断或超过 8 MiB", []string{"refresh"}},
	} {
		f := subscriptionFailure("更新订阅", tc.err, tc.viaHost, tc.previous, "src-1")
		if f == nil || f.Lead != "更新订阅失败" || f.Level != usererror.LevelWarn || f.Reason != tc.reason || !reflect.DeepEqual(f.Tools, tc.tools) || f.Target != "src-1" {
			t.Errorf("%s: %+v, want %q %v", tc.name, f, tc.reason, tc.tools)
		}
	}
	// Product rules keep their own message.
	if f := subscriptionFailure("更新订阅", errors.New("订阅地址重定向到了非 HTTPS 地址"), false, 0, ""); f != nil {
		t.Fatalf("a refused redirect became %+v", f)
	}
}

// No importable node: how many, and the share-link case (P13).
func TestImportFailureCountsNodes(t *testing.T) {
	share := errors.New(`没有可导入的节点，已跳过 2 个；第 1 个节点 ""：分享链接的协议暂不支持；请改用 Clash/Mihomo YAML；第 2 个节点 "b"：分享链接的协议暂不支持；请改用 Clash/Mihomo YAML`)
	f := importFailure("import", share)
	if f == nil || f.Error() != "导入订阅失败，原因是 2 个节点都没能导入（分享链接的协议不支持）。解决建议：改用 Clash/Mihomo YAML 导入。" || f.Level != usererror.LevelWarn {
		t.Fatalf("%+v", f)
	}
	mixed := errors.New(`没有可导入的节点，已跳过 2 个；第 1 个节点 "a"：缺少 server；第 2 个节点 "b"：分享链接的协议暂不支持；请改用 Clash/Mihomo YAML`)
	if f := importFailure("refresh", mixed); f == nil || f.Error() != "更新订阅失败，原因是 2 个节点都没能导入。解决建议：核对节点配置后重新导入。" {
		t.Fatalf("%+v", f)
	}
	if importFailure("import", errors.New("配置为空")) != nil {
		t.Fatal("an unrelated error matched")
	}
}

// A missing or unapplied node is a step to take, not a fault (P12); a core
// that does not come up is (P6).
func TestNodeAndCoreFailures(t *testing.T) {
	if f := proxyRequirementFailure(managedproxy.ErrNoNodeSelected); f == nil || f.Level != usererror.LevelInfo ||
		f.Error() != "启动环境前需要代理节点，原因是还没有选择节点。下一步：在网络与代理中导入订阅并选择节点。" || f.Tools[0] != "settings:network" {
		t.Fatalf("%+v", f)
	}
	if f := proxyRequirementFailure(errProxyNotApplied); f == nil || f.Tools[0] != "reapply" || f.Level != usererror.LevelInfo {
		t.Fatalf("%+v", f)
	}
	if f := coreFailure(errors.Join(managedproxy.ErrCoreNotReady, errors.New("socket"))); f == nil || f.Reason != "原因是代理核心 15 秒内没有就绪" || f.Tools[0] != "reapply" {
		t.Fatalf("%+v", f)
	}
	if f := coreFailure(managedproxy.ErrCoreExited); f == nil || f.Reason != "原因是代理核心启动后立即退出" {
		t.Fatalf("%+v", f)
	}
}

// The job names the operation a probe left open, the node it went through,
// and a retry for a failure without tools.
func TestJobDescribesTheFailure(t *testing.T) {
	s := testServer(t)
	selectTestNode(t, s, "澳大利亚 01")
	probe := usererror.Because("原因是经所选节点连不上任何检测地址（6 个出口检测服务和连通性检查都失败：连接超时）", "换一个节点", nil, usererror.ToolSettingsNetwork)
	for _, tc := range []struct {
		kind string
		err  error
		lead string
	}{
		{"proxy", probe, "应用代理节点失败"},
		{"start", networkCheckError{probe}, "联网检查未通过"},
		{"start", probe, "启动环境失败"},
		{"safety", probe, "联网检查未通过"},
	} {
		copied := *probe
		var err error = &copied
		if _, wrapped := tc.err.(networkCheckError); wrapped {
			err = networkCheckError{&copied}
		}
		f, _ := usererror.AsFailure(s.describeFailure(tc.kind, "", err))
		if f.Lead != tc.lead || f.Subject != "澳大利亚 01" {
			t.Errorf("%s: %+v, want %q", tc.kind, f, tc.lead)
		}
	}
	want := "应用代理节点失败，原因是经「澳大利亚 01」连不上任何检测地址（6 个出口检测服务和连通性检查都失败：连接超时）。解决建议：换一个节点。"
	copied := *probe
	if err := s.describeFailure("proxy", "", &copied); err.Error() != want {
		t.Fatalf("%q", err.Error())
	}
	// A start retries as a start, not by applying the node alone.
	startProbe := *probe
	startProbe.Tools = []string{usererror.ToolSettingsNetwork, usererror.ToolReapply, usererror.ToolDiagnostics}
	if f, _ := usererror.AsFailure(s.describeFailure("start", "", &startProbe)); !reflect.DeepEqual(f.Tools, []string{"settings:network", "start", "diagnostics"}) {
		t.Fatalf("start tools: %v", f.Tools)
	}
	bare := usererror.Because("原因是连接网络后", "重试", nil)
	if f, _ := usererror.AsFailure(s.describeFailure("start", "", bare)); !reflect.DeepEqual(f.Tools, []string{"start"}) {
		t.Fatalf("retry tool: %+v", f)
	}
	if f, _ := usererror.AsFailure(proxyActionFailure("import", usererror.Because("原因是x", "y", nil))); f.Lead != "导入订阅失败" {
		t.Fatalf("import lead: %+v", f)
	}
	if f, _ := usererror.AsFailure(proxyActionFailure("route", usererror.Because("原因是x", "y", nil))); f.Lead != "" || f.Tools[0] != "reapply" {
		t.Fatalf("route: %+v", f)
	}
}

// A request that fails with a structured failure also hands it over in a
// header, for pages that do not watch a job.
func TestRequestFailureHeader(t *testing.T) {
	w := httptest.NewRecorder()
	requestError(w, usererror.Fail("导入订阅", "原因是 2 个节点都没能导入", "改用 Clash/Mihomo YAML 导入", nil).WithLevel(usererror.LevelWarn))
	data, err := base64.RawURLEncoding.DecodeString(w.Header().Get("X-Cage-Failure"))
	var f usererror.Failure
	if err != nil || json.Unmarshal(data, &f) != nil || f.Lead != "导入订阅失败" || f.Level != "warn" {
		t.Fatalf("header %q: %+v %v", w.Header().Get("X-Cage-Failure"), f, err)
	}
	plain := httptest.NewRecorder()
	requestError(plain, errors.New("节点不存在"))
	if plain.Header().Get("X-Cage-Failure") != "" {
		t.Fatal("a plain error got a header")
	}
}

// selectTestNode saves a store whose selected node has name.
func selectTestNode(t *testing.T, s *Server, name string) {
	t.Helper()
	p, err := managedproxy.Load(s.profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Import("test", "", []byte(fmt.Sprintf(`{"proxies":[{"name":%q,"type":"socks5","server":"192.0.2.1","port":1080}]}`, name)), "", 0, ""); err != nil {
		t.Fatal(err)
	}
	p.Selected = p.Sources[len(p.Sources)-1].Nodes[0].ID
	if err := p.Save(s.profile); err != nil {
		t.Fatal(err)
	}
}
