package workbench

import (
	"context"
	"time"

	"github.com/sylvinus/agent-vm/internal/diagnostics"
	"github.com/sylvinus/agent-vm/internal/managedproxy"
)

// An applied core can exit or stop answering while the environment runs (it
// crashed, or the host ended it). The health check then closes networking,
// but nothing started the core again: the user had to re-apply the node.
// proxyCoreRestart re-applies it automatically, backing off for a core that
// keeps failing. Re-applying ends in the ordinary full check, so networking
// still reopens only after that check passes.
type proxyCoreRestart struct {
	// stopped: the user stopped the built-in proxy; leave it stopped until a
	// node is applied again.
	stopped bool
	// armed: the core has not answered since the first failed health check.
	armed bool
	next  time.Time // earliest automatic restart
	delay time.Duration
	last  time.Time // last automatic restart
}

const (
	proxyCoreRestartDelay    = 15 * time.Second
	proxyCoreRestartMaxDelay = 10 * time.Minute
	// A core that fails again this soon after an automatic restart keeps
	// the longer delay instead of starting over.
	proxyCoreRestartStable = 5 * time.Minute
)

var restartProxyCoreJob = (*Server).restartProxyCore

// restartUnresponsiveProxyCore runs on each health check that found the
// applied core not answering, after networking was closed. The core must keep
// failing for proxyCoreRestartDelay first: a busy host can miss one check.
func (s *Server) restartUnresponsiveProxyCore(stage string) {
	now := time.Now()
	s.mu.Lock()
	r := &s.coreRestart
	if r.stopped || s.networkPaused || s.closing || !s.ready {
		s.mu.Unlock()
		return
	}
	if !r.armed {
		r.armed = true
		if r.delay == 0 || now.Sub(r.last) >= proxyCoreRestartStable {
			r.delay = proxyCoreRestartDelay
		} else {
			r.delay = min(2*r.delay, proxyCoreRestartMaxDelay)
		}
		r.next = now.Add(r.delay)
	}
	due := !now.Before(r.next)
	if due {
		// If this restart fails too, the next attempt waits longer.
		r.last = now
		r.delay = min(2*r.delay, proxyCoreRestartMaxDelay)
		r.next = now.Add(r.delay)
	}
	s.mu.Unlock()
	if !due {
		return
	}
	diagnostics.Log("warn", "proxy", "内置代理持续没有响应（stage="+stage+"），正在自动重新应用当前节点")
	_, _ = s.submitJob("proxy-network", func() error { return restartProxyCoreJob(s) })
}

// proxyCoreAnswered ends a pending automatic restart once the core answers.
func (s *Server) proxyCoreAnswered() {
	s.mu.Lock()
	s.coreRestart.armed = false
	s.mu.Unlock()
}

// restartProxyCore re-applies the applied node. applyProxy closes networking
// first and reopens it only through the full check.
func (s *Server) restartProxyCore() error {
	cfg, err := loadConfig(s.configPath)
	if err != nil {
		return err
	}
	p, err := managedproxy.Applied(s.profile, cfg.ProxyURL, cfg.ProxyRevision)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(s.operationContext(), 2*time.Second)
	running := managedproxy.Running(ctx, s.profile, p)
	cancel()
	s.mu.Lock()
	skip := running || s.coreRestart.stopped || s.networkPaused || s.closing
	s.mu.Unlock()
	if skip || !s.vmRunning() {
		return nil
	}
	return s.applyProxy(p, cfg, nil, false)
}
