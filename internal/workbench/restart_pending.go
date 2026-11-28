package workbench

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/docker/go-units"
	"github.com/sylvinus/agent-vm/internal/cage"
	"go.yaml.in/yaml/v3"
)

// appliedSettings are the saved settings a running environment reads only
// when it starts. One saved while it runs stays pending until the next start:
// the status names it and the workbench offers a restart.
type appliedSettings struct {
	BalancedDisplay  bool
	ProjectFileCache bool
	Resources        cage.ResourceAllocation
	resourcesKnown   bool
	// diskGiB is the size the VM's disk has now (its lima.yaml), 0 unknown.
	diskGiB int
}

func (s *Server) savedRestartSettings() appliedSettings {
	allocation, err := cage.LoadResourceAllocation(s.configPath)
	disk := 0
	var configured struct {
		Disk string `yaml:"disk"`
	}
	if b, readErr := os.ReadFile(filepath.Join(s.instanceDir(), "lima.yaml")); readErr == nil && yaml.Unmarshal(b, &configured) == nil {
		if bytes, sizeErr := units.RAMInBytes(configured.Disk); sizeErr == nil && bytes > 0 {
			disk = int((bytes + (1 << 30) - 1) >> 30)
		}
	}
	s.mu.Lock()
	p := s.preferences
	s.mu.Unlock()
	return appliedSettings{BalancedDisplay: p.BalancedDisplay, ProjectFileCache: p.ProjectFileCache, Resources: allocation, resourcesKnown: err == nil, diskGiB: disk}
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
		inMemory, inCPUs, inErr := in.Resources.Limits(0)
		memory, cpus, err := now.Resources.Limits(0)
		if inErr == nil && err == nil {
			if inMemory != memory {
				pending = append(pending, "memory")
			}
			if inCPUs != cpus {
				pending = append(pending, "cpus")
			}
		}
	}
	// The disk only grows, to a set size larger than it is now. Automatic,
	// or the present size written by the form on another change, is none.
	if now.resourcesKnown && now.diskGiB > 0 && !now.Resources.Automatic && now.Resources.DiskGiB > now.diskGiB {
		pending = append(pending, "disk")
	}
	return pending
}

// environmentSettingsChanged reports whether saving v changes what the
// environment was started with, so applying it restarts the environment.
// Preferences such as balanced display apply at the next start instead
// (restart_pending).
func environmentSettingsChanged(old cage.Config, oldPrefs Preferences, v savedProfile) bool {
	return !old.SameSettings(v.Config) || oldPrefs.AutoTimezone != v.Preferences.AutoTimezone || oldPrefs.AutoLanguage != v.Preferences.AutoLanguage
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
		_, _, diskBefore := s.configuredInstanceResources()
		if err := s.stopEnvironment(); err != nil {
			return err
		}
		if err := s.prepareEnvironment(); err != nil {
			return err
		}
		if err := s.startEnvironment(true); err != nil {
			return err
		}
		// A larger saved disk grew at this start; extend the guest's
		// partition as applying resources does.
		if _, _, diskAfter := s.configuredInstanceResources(); diskBefore > 0 && diskAfter > diskBefore {
			growCtx, cancel := context.WithTimeout(s.operationContext(), 60*time.Second)
			defer cancel()
			if _, err := s.guestControl(growCtx, cage.GuestDiskGrowScript, "disk-grow"); err != nil {
				return fmt.Errorf("虚拟磁盘已扩大，但客机自动扩展分区失败：%w", err)
			}
		}
		return nil
	})
}
