package workbench

import (
	"context"
	"testing"
	"time"

	"github.com/sylvinus/agent-vm/internal/cage"
	"github.com/sylvinus/agent-vm/internal/managedproxy"
)

func TestUnresponsiveProxyCoreIsReappliedAfterItKeepsFailing(t *testing.T) {
	s, gate := appliedProxyHealthServer(t)
	s.mu.Lock()
	s.ready = true
	s.mu.Unlock()
	var restarts int
	old := restartProxyCoreJob
	restartProxyCoreJob = func(*Server) error { restarts++; return nil }
	t.Cleanup(func() { restartProxyCoreJob = old })
	dead := func(context.Context, string, managedproxy.Store) string { return "readiness.configs" }
	healthy := func(context.Context, string, managedproxy.Store) string { return "" }
	check := func(stage func(context.Context, string, managedproxy.Store) string) {
		t.Helper()
		s.checkProxyHealthWith(stage)
		if job := awaitJobIdle(t, &s.jobs); job.Error != "" {
			t.Fatal(job)
		}
	}
	due := func() {
		s.mu.Lock()
		s.coreRestart.next = time.Now().Add(-time.Second)
		s.mu.Unlock()
	}

	// The first failure closes networking and starts the wait.
	check(dead)
	if gate.Status().Mode != cage.GateClosed || restarts != 0 {
		t.Fatalf("first failure: gate=%+v restarts=%d", gate.Status(), restarts)
	}
	if !s.coreRestart.next.After(time.Now().Add(10 * time.Second)) {
		t.Fatalf("no wait before the first restart: %+v", s.coreRestart)
	}
	// A check that passes in between ends it.
	check(healthy)
	if s.coreRestart.armed {
		t.Fatal("answering core kept the restart pending")
	}

	// Still failing once the wait is over: re-applied once, next wait longer.
	check(dead)
	due()
	check(dead)
	if restarts != 1 || s.coreRestart.delay != 2*proxyCoreRestartDelay {
		t.Fatalf("restarts=%d state=%+v", restarts, s.coreRestart)
	}
	check(dead)
	if restarts != 1 {
		t.Fatalf("restarted again without waiting: %d", restarts)
	}

	// A core that fails again soon after a restart keeps the longer wait.
	check(healthy)
	check(dead)
	if s.coreRestart.delay != 4*proxyCoreRestartDelay {
		t.Fatalf("crash loop did not back off: %+v", s.coreRestart)
	}

	// Paused networking or a proxy the user stopped stays down.
	due()
	s.mu.Lock()
	s.networkPaused = true
	s.mu.Unlock()
	check(dead)
	s.mu.Lock()
	s.networkPaused = false
	s.coreRestart.stopped = true
	s.mu.Unlock()
	check(dead)
	if restarts != 1 {
		t.Fatalf("restarted while paused or stopped: %d", restarts)
	}

	// Failures other than an unresponsive core are left to the user.
	s.mu.Lock()
	s.coreRestart = proxyCoreRestart{}
	s.mu.Unlock()
	check(func(context.Context, string, managedproxy.Store) string { return "route" })
	if s.coreRestart.armed {
		t.Fatal("a route error armed a core restart")
	}
}
