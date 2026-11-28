package workbench

import (
	"errors"
	"reflect"
	"testing"

	"github.com/sylvinus/agent-vm/internal/vm"
)

// A start refused because the last run's hostagent still runs offers the
// restart, which stops that hostagent before starting.
func TestHostagentStillRunningOffersRestart(t *testing.T) {
	healthyHost(t)
	f := environmentFailure("up", "启动环境", errors.New("exit status 1"), "Error: "+vm.ErrHostAgentRunning.Error(), failureFacts{})
	if f == nil || f.Error() != "启动环境失败，原因是上次的环境后台仍在运行。解决建议：重启环境。" || !reflect.DeepEqual(f.Tools, []string{"restart", "diagnostics"}) {
		t.Fatalf("%+v", f)
	}
}
