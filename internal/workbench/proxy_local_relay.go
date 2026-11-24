package workbench

import (
	"net/http"

	"github.com/sylvinus/agent-vm/internal/managedproxy"
)

// proxyLocalRelay allows or revokes a 127.0.0.1 port as the first hop of a
// chain (#595). Only the Cage window on this computer can change it; agents
// and remote clients cannot widen the host boundary.
func (s *Server) proxyLocalRelay(w http.ResponseWriter, r *http.Request) {
	if !s.hasTrustedView(r) {
		http.Error(w, "请在这台电脑的 Cage 窗口中设置本机中转", http.StatusForbidden)
		return
	}
	var in struct {
		Port  int  `json:"port"`
		Allow bool `json:"allow"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Port < 1 || in.Port > 65535 {
		http.Error(w, "端口应为 1–65535", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	pauseVersion := s.safety.pauseVersion
	s.mu.Unlock()
	s.launchRequest(w, r, "proxy", func() error { return s.changeLocalRelay(in.Port, in.Allow, &pauseVersion) })
}

func (s *Server) changeLocalRelay(port int, allow bool, resumeVersion *uint64) error {
	p, err := managedproxy.Load(s.profile)
	if err != nil {
		return err
	}
	if allow {
		// Allowing changes no running route: nodes on the port are imported
		// or applied afterwards, through the usual checks.
		if err := p.AllowLocalRelay(port); err != nil {
			return err
		}
		return p.Save(s.profile)
	}
	cfg, err := loadConfig(s.configPath)
	if err != nil {
		return err
	}
	var relays []string
	for _, src := range p.Sources {
		for _, n := range src.Nodes {
			if p.LocalRelay(n.ID) {
				relays = append(relays, n.ID)
			}
		}
	}
	_, before := p.Route()
	p.RevokeLocalRelay(port)
	affected := false
	for _, id := range relays {
		affected = affected || !p.LocalRelay(id)
	}
	applied := cfg.ProxyRevision != "" && cfg.ProxyRevision == p.Revision && cfg.ProxyURL == p.Endpoint()
	if !affected || !applied {
		return p.Save(s.profile)
	}
	if _, after := p.Route(); after != nil {
		if before == nil {
			// The applied route went through the port. Networking stays
			// closed until the user applies another route.
			if err := s.closeProxyGate("本机中转已撤销，外网保持关闭"); err != nil {
				return err
			}
			if err := managedproxy.Stop(s.profile); err != nil {
				return err
			}
			p.Revision = ""
		}
		return p.Save(s.profile)
	}
	// The running core still lists the revoked nodes; until it is restarted
	// without them, its checks fail closed.
	if err := p.Save(s.profile); err != nil {
		return err
	}
	return s.applyProxy(p, cfg, resumeVersion, false)
}
