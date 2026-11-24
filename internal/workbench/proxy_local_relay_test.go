package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sylvinus/agent-vm/internal/managedproxy"
)

func relayRequest(t *testing.T, s *Server, body string, trusted bool) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", s.origin+"/api/proxy/local-relay", strings.NewReader(body))
	r.Header.Set("Origin", s.origin)
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: s.cookieName(), Value: s.session})
	if trusted {
		r.AddCookie(&http.Cookie{Name: s.cookieName() + "-desktop", Value: trustedViewProof(s.token)})
	}
	w := httptest.NewRecorder()
	s.handler().ServeHTTP(w, r)
	return w
}

func waitProxyJob(t *testing.T, s *Server) Job {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if job := s.jobs.snapshot(); !job.Running {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("proxy job did not finish")
	return Job{}
}

func TestLocalRelayPermissionAndImportOrder(t *testing.T) {
	s := testServer(t)
	// Imports only save; applying is covered elsewhere.
	cfg, _ := loadConfig(s.configPath)
	cfg.ProxyRevision = ""
	if err := writePrivate(s.configPath, cfg); err != nil {
		t.Fatal(err)
	}
	var validated []string
	oldValidate, oldCore := validateProxyCore, proxyCorePath
	t.Cleanup(func() { validateProxyCore, proxyCorePath = oldValidate, oldCore })
	proxyCorePath = func() (string, error) { return "mihomo", nil }
	validateProxyCore = func(_ context.Context, profile string, p managedproxy.Store) error {
		validated = append(validated, p.Selected)
		_, err := p.Runtime(profile)
		return err
	}
	// Agents, phones and browsers without the desktop proof cannot allow it.
	if w := relayRequest(t, s, `{"port": 7890, "allow": true}`, false); w.Code != http.StatusForbidden {
		t.Fatalf("untrusted view allowed relay: %d", w.Code)
	}
	if w := relayRequest(t, s, `{"port": 0, "allow": true}`, true); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid port: %d", w.Code)
	}
	if w := relayRequest(t, s, `{"port": 7890, "allow": true}`, true); w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	if job := waitProxyJob(t, s); job.Error != "" {
		t.Fatal(job.Error)
	}
	p, _ := managedproxy.Load(s.profile)
	if !slices.Equal(p.LocalRelays, []int{7890}) {
		t.Fatalf("relay not saved: %v", p.LocalRelays)
	}
	// The first node is the relay; the import must not roll back the source.
	content := "proxies: [{name: relay, type: socks5, server: 127.0.0.1, port: 7890}, {name: via, type: http, server: remote.example, port: 8080, dialer-proxy: relay}]"
	body, _ := json.Marshal(proxyInput{Name: "relay first", Content: content})
	if w := request(s, "POST", "/api/proxy/import", s.origin, string(body), true); w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	if job := waitProxyJob(t, s); job.Error != "" {
		t.Fatal(job.Error)
	}
	p, _ = managedproxy.Load(s.profile)
	src := p.Sources[len(p.Sources)-1]
	if len(src.Nodes) != 2 || !p.LocalRelay(src.Nodes[0].ID) {
		t.Fatalf("relay source not imported: %+v", src)
	}
	// The saved selection stays; the new nodes are validated alongside it.
	if len(validated) != 1 || validated[0] != p.Selected {
		t.Fatalf("validated %v, selected %s", validated, p.Selected)
	}
	// With no valid selection, the first node that can exit is validated.
	p.Selected = ""
	if err := p.Save(s.profile); err != nil {
		t.Fatal(err)
	}
	validated = nil
	body, _ = json.Marshal(proxyInput{Name: "again", Content: content})
	request(s, "POST", "/api/proxy/import", s.origin, string(body), true)
	if job := waitProxyJob(t, s); job.Error != "" {
		t.Fatal(job.Error)
	}
	p, _ = managedproxy.Load(s.profile)
	again := p.Sources[len(p.Sources)-1]
	if len(validated) != 1 || validated[0] != again.Nodes[1].ID {
		t.Fatalf("validated %v instead of the remote node", validated)
	}
	// A source of the relay alone imports without an exit to validate.
	validated = nil
	body, _ = json.Marshal(proxyInput{Name: "relay only", Content: "proxies: [{name: relay, type: socks5, server: 127.0.0.1, port: 7890}]"})
	request(s, "POST", "/api/proxy/import", s.origin, string(body), true)
	if job := waitProxyJob(t, s); job.Error != "" {
		t.Fatal(job.Error)
	}
	if len(validated) != 0 {
		t.Fatalf("relay-only source validated with %v", validated)
	}
	w := request(s, "GET", "/api/proxy", s.origin, "", true)
	var view struct {
		LocalRelays []int `json:"local_relays"`
		Sources     []struct {
			Nodes []struct {
				Name  string `json:"name"`
				Relay bool   `json:"relay"`
			} `json:"nodes"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil || !slices.Equal(view.LocalRelays, []int{7890}) {
		t.Fatal("status lost relays", err, w.Body.String())
	}
	if node := view.Sources[1].Nodes[0]; !node.Relay || view.Sources[1].Nodes[1].Relay {
		t.Fatalf("relay flag: %+v", view.Sources[1].Nodes)
	}
}

func TestRevokingAppliedLocalRelayClosesRoute(t *testing.T) {
	s := testServer(t)
	p, _ := managedproxy.Load(s.profile)
	if err := p.AllowLocalRelay(7890); err != nil {
		t.Fatal(err)
	}
	if err := p.Import("relay", "", []byte("proxies: [{name: relay, type: socks5, server: 127.0.0.1, port: 7890}, {name: via, type: http, server: remote.example, port: 8080, dialer-proxy: relay}]"), "relay", 0, ""); err != nil {
		t.Fatal(err)
	}
	p.Selected = p.Sources[1].Nodes[1].ID
	if err := p.Save(s.profile); err != nil {
		t.Fatal(err)
	}
	if w := relayRequest(t, s, `{"port": 7890, "allow": false}`, true); w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	if job := waitProxyJob(t, s); job.Error != "" {
		t.Fatal(job.Error)
	}
	p, _ = managedproxy.Load(s.profile)
	if len(p.LocalRelays) != 0 || p.Revision != "" {
		t.Fatalf("revoked relay left the route applied: %v %q", p.LocalRelays, p.Revision)
	}
	if _, err := p.Route(); !errors.Is(err, managedproxy.ErrLocalProxyNode) {
		t.Fatalf("route through revoked relay: %v", err)
	}
	cfg, _ := loadConfig(s.configPath)
	if _, err := managedproxy.Applied(s.profile, cfg.ProxyURL, cfg.ProxyRevision); err == nil {
		t.Fatal("revoked relay route still applied")
	}
}
