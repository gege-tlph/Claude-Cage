package cage

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sylvinus/agent-vm/internal/diagnostics"
	"github.com/sylvinus/agent-vm/internal/managedproxy"
)

// stallingTLSServices accepts TCP and never answers the ClientHello, which the
// probe reports as a TLS handshake timeout. onAccept runs for every connection,
// standing in for the core logging while it dials the node.
func stallingTLSServices(t *testing.T, onAccept func()) []exitProbeService {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	t.Cleanup(func() { close(done); ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			onAccept()
			go func(conn net.Conn) {
				defer conn.Close()
				<-done
			}(c)
		}
	}()
	return []exitProbeService{{id: "a", name: "主检测服务", endpoint: "https://" + ln.Addr().String()}, {id: "b", name: "备用检测服务", endpoint: "https://" + ln.Addr().String()}}
}

func useDiagnosticStore(t *testing.T) *diagnostics.Store {
	t.Helper()
	store := diagnostics.New(t.TempDir())
	previous := diagnostics.Default()
	diagnostics.SetDefault(store)
	t.Cleanup(func() { diagnostics.SetDefault(previous) })
	return store
}

func TestExitProbeReportsCoreRealityAuthFailure(t *testing.T) {
	for _, tc := range []struct {
		name     string
		selected bool
		markers  []string
		want     string
		unwanted []string
	}{
		{"relay on applied route", true, []string{managedproxy.RealityHopRelay}, "原因是代理链前置节点的", []string{"出口节点"}},
		{"exit on applied route", true, []string{managedproxy.RealityHopExit}, "原因是代理链出口节点的", []string{"前置节点"}},
		{"single node", true, []string{managedproxy.RealityHopSingle}, "原因是所选节点的", []string{"前置节点", "出口节点"}},
		{"conflicting hops", true, []string{managedproxy.RealityHopRelay, managedproxy.RealityHopExit}, "原因是代理节点的", []string{"前置节点", "出口节点"}},
		{"other route", false, []string{managedproxy.RealityHopRelay}, "原因是代理节点的", []string{"前置节点", "出口节点"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := useDiagnosticStore(t)
			services := stallingTLSServices(t, func() {
				_ = store.Record("warn", "proxy", "[TCP] dial CAGE --> ipv4.icanhazip.com:443 error: 203.0.113.5:443 connect error: REALITY authentication failed")
				for _, hop := range tc.markers {
					_ = store.Record("warn", "proxy", managedproxy.RealityHopMarker+hop)
				}
			})
			var checks atomic.Int32
			reachable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				checks.Add(1)
				w.WriteHeader(http.StatusNoContent)
			}))
			defer reachable.Close()
			ctx := context.Background()
			if tc.selected {
				ctx = context.WithValue(ctx, selectedRouteProbe{}, true)
			}
			ip, err := probeExitIPServices(ctx, &http.Client{Timeout: 150 * time.Millisecond}, services, time.Minute, reachable.URL)
			if ip != "" || !errors.Is(err, ErrRealityAuthFailed) || errors.Is(err, ErrTLSHandshakeFailed) || errors.Is(err, ErrProxyConnectFailed) {
				t.Fatalf("expected ErrRealityAuthFailed only, got ip=%q err=%v", ip, err)
			}
			message := err.Error()
			for _, want := range []string{"REALITY 认证没有通过", tc.want, "public-key", "short-id", "servername"} {
				if !strings.Contains(message, want) {
					t.Fatalf("missing %q: %s", want, message)
				}
			}
			for _, unwanted := range append([]string{"TLS", "client-fingerprint", "外网保持关闭", "203.0.113.5"}, tc.unwanted...) {
				if strings.Contains(message, unwanted) {
					t.Fatalf("unexpected %q: %s", unwanted, message)
				}
			}
			if checks.Load() != 0 {
				t.Fatalf("REALITY failure must not run the connectivity check, got %d", checks.Load())
			}
			if reason := diagnosticProbeReason(err); reason != "reality_auth" {
				t.Fatalf("diagnostic reason = %q", reason)
			}
		})
	}
}

// Through the applied route, a REALITY failure no hop of the route accounts
// for belongs to another node the core dialed meanwhile (a speed test, another
// account's exit): the probe keeps its own classification.
func TestExitProbeIgnoresRealityFailureOffTheRoute(t *testing.T) {
	store := useDiagnosticStore(t)
	services := stallingTLSServices(t, func() {
		_ = store.Record("warn", "proxy", "[TCP] dial other --> www.gstatic.com:443 error: 198.51.100.9:443 connect error: REALITY authentication failed")
	})
	ip, err := probeExitIPServices(context.WithValue(context.Background(), selectedRouteProbe{}, true), &http.Client{Timeout: 150 * time.Millisecond}, services, time.Minute, "")
	if ip != "" || errors.Is(err, ErrRealityAuthFailed) || !errors.Is(err, ErrTLSHandshakeFailed) {
		t.Fatalf("a failure off the route must not be this probe's: ip=%q err=%v", ip, err)
	}
}

func TestExitProbeIgnoresRealityFailureBeforeTheProbe(t *testing.T) {
	store := useDiagnosticStore(t)
	_ = store.Record("warn", "proxy", "[ip] connect error: REALITY authentication failed")
	_ = store.Record("warn", "proxy", managedproxy.RealityHopMarker+managedproxy.RealityHopRelay)
	_ = store.Record("warn", "exit-probe", "REALITY authentication failed")
	time.Sleep(20 * time.Millisecond)
	services := stallingTLSServices(t, func() {
		_ = store.Record("warn", "exit-probe", "error=REALITY authentication failed")
	})
	ip, err := probeExitIPServices(context.WithValue(context.Background(), selectedRouteProbe{}, true), &http.Client{Timeout: 150 * time.Millisecond}, services, time.Minute, "")
	if ip != "" || errors.Is(err, ErrRealityAuthFailed) || !errors.Is(err, ErrTLSHandshakeFailed) || !strings.Contains(err.Error(), "TLS 握手都超时") {
		t.Fatalf("an earlier or non-core REALITY record must not change the classification: ip=%q err=%v", ip, err)
	}
}
