package workbench

import (
	"errors"
	"reflect"
	"testing"

	"github.com/sylvinus/agent-vm/internal/vm"
)

// 「Lima cannot say」 is a stop's answer; a start that shows it is not a
// stop failure.
func TestCannotSayIsAStopFailureOnly(t *testing.T) {
	healthyHost(t)
	output := "Error: VM 'work' is still running, or Lima cannot say. See 'cage list'."
	stop := environmentFailure("down", "停止环境", errors.New("exit status 1"), output, failureFacts{})
	if stop == nil || stop.Lead != "停止环境失败" || stop.Reason != "原因是无法确认虚拟机已停止" || !reflect.DeepEqual(stop.Tools, []string{"stop", "diagnostics"}) {
		t.Fatalf("stop: %+v", stop)
	}
	start := environmentFailure("up", "启动环境", errors.New("exit status 1"), output, failureFacts{})
	if start == nil || start.Lead != "启动环境失败" || start.Reason == "原因是无法确认虚拟机已停止" {
		t.Fatalf("start: %+v", start)
	}
}

// A start refused because the last run's hostagent still runs offers the
// restart, which stops that hostagent before starting.
func TestHostagentStillRunningOffersRestart(t *testing.T) {
	healthyHost(t)
	f := environmentFailure("up", "启动环境", errors.New("exit status 1"), "Error: "+vm.ErrHostAgentRunning.Error(), failureFacts{})
	if f == nil || f.Error() != "启动环境失败，原因是上次的环境后台仍在运行。解决建议：重启环境。" || !reflect.DeepEqual(f.Tools, []string{"restart", "diagnostics"}) {
		t.Fatalf("%+v", f)
	}
}
