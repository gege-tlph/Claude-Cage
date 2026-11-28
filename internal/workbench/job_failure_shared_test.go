package workbench

import (
	"reflect"
	"testing"

	"github.com/sylvinus/agent-vm/internal/usererror"
)

// One failure value returned by two jobs in turn (a package-level failure,
// such as Windows' missing network card) keeps each job's own lead and tools.
func TestDescribeFailureDoesNotChangeASharedFailure(t *testing.T) {
	shared := usererror.Because("原因是找不到可用的 IPv4 出口网卡", "重新应用节点", nil, usererror.ToolReapply)
	s := &Server{}
	proxy, _ := usererror.AsFailure(s.describeFailure("proxy", "", shared))
	start, _ := usererror.AsFailure(s.describeFailure("start", "", shared))
	if proxy.Lead != "应用代理节点失败" || !reflect.DeepEqual(proxy.Tools, []string{usererror.ToolReapply}) {
		t.Fatalf("proxy: %+v", proxy)
	}
	if start.Lead != "启动环境失败" || !reflect.DeepEqual(start.Tools, []string{usererror.ToolStart}) || start.Next != "重试启动" {
		t.Fatalf("start: %+v", start)
	}
	if shared.Lead != "" || shared.Tools[0] != usererror.ToolReapply || shared.Next != "重新应用节点" {
		t.Fatalf("the shared failure changed: %+v", shared)
	}
	action, _ := usererror.AsFailure(proxyActionFailure("restart", shared))
	if action.Lead != "重启代理核心失败" || shared.Lead != "" {
		t.Fatalf("proxy action: %+v, shared: %+v", action, shared)
	}
}
