package workbench

import (
	"net/http"

	"github.com/sylvinus/agent-vm/internal/cage"
)

// appliedSettings are the saved settings a running environment reads only
// when it starts. One saved while it runs stays pending until the next start:
// the status names it and the workbench offers a restart.
type appliedSettings struct {
	BalancedDisplay  bool
	ProjectFileCache bool
	Resources        cage.ResourceAllocation
	resourcesKnown   bool
}

func (s *Server) savedRestartSettings() appliedSettings {
	allocation, err := cage.LoadResourceAllocation(s.configPath)
	s.mu.Lock()
	p := s.preferences
	s.mu.Unlock()
	return appliedSettings{BalancedDisplay: p.BalancedDisplay, ProjectFileCache: p.ProjectFileCache, Resources: allocation, resourcesKnown: err == nil}
}

// markApplied records the settings an environment start is about to use.
func (s *Server) markApplied() {
	now := s.savedRestartSettings()
	s.mu.Lock()
	s.applied = &now
	s.mu.Unlock()
}

// restartPendingLocked lists, by id, the saved settings the running
// environment does not use yet. An environment that was already running
// when this workbench started is taken to use the saved ones.
func (s *Server) restartPendingLocked(running bool, now appliedSettings) []string {
	pending := []string{}
	if !running {
		s.applied = nil
		return pending
	}
	if s.applied == nil {
		s.applied = &now
	}
	in := *s.applied
	if in.BalancedDisplay != now.BalancedDisplay {
		pending = append(pending, "balanced_display")
	}
	if in.ProjectFileCache != now.ProjectFileCache {
		pending = append(pending, "project_file_cache")
	}
	if in.resourcesKnown && now.resourcesKnown {
		inMemory, inCPUs, inErr := in.Resources.Limits()
		memory, cpus, err := now.Resources.Limits()
		if inErr == nil && err == nil {
			if inMemory != memory {
				pending = append(pending, "memory")
			}
			if inCPUs != cpus {
				pending = append(pending, "cpus")
			}
		}
		// Automatic keeps the disk's present size; a set size only grows it.
		disk := func(a cage.ResourceAllocation) int {
			if a.Automatic {
				return 0
			}
			return a.DiskGiB
		}
		if disk(now.Resources) > disk(in.Resources) {
			pending = append(pending, "disk")
		}
	}
	return pending
}

// restartEnvironment stops and starts the running environment in one job, so
// saved settings that apply at start take effect. Another job, such as a stop
// already under way, refuses it.
func (s *Server) restartEnvironment(w http.ResponseWriter, r *http.Request) {
	if err := s.requireStorageReady(); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if !s.vmRunning() {
		http.Error(w, "环境未运行", http.StatusConflict)
		return
	}
	s.launchRequest(w, r, "restart", func() error {
		if err := s.stopEnvironment(); err != nil {
			return err
		}
		if err := s.prepareEnvironment(); err != nil {
			return err
		}
		return s.startEnvironment(true)
	})
}
