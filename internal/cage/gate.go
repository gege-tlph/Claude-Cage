package cage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
)

const (
	GateClosed        = "closed"
	GateChecking      = "checking"
	GateOpen          = "open"
	GateLease         = 90 * time.Second
	DiagnosticHost    = "198.18.0.254"
	DiagnosticAddress = "198.18.0.254:80"
)

var ErrGateClosed = errors.New("Cage lite 外网未放行，请在工作台完成关键检查")

type GateCounters struct {
	TCPDenied     uint64 `json:"tcp_denied"`
	UDPDenied     uint64 `json:"udp_denied"`
	IPv6Denied    uint64 `json:"ipv6_denied"`
	PrivateDenied uint64 `json:"private_denied"`
	DNSDenied     uint64 `json:"dns_denied"`
	ProbeTCP      uint64 `json:"probe_tcp"`
	ProbeDNS      uint64 `json:"probe_dns"`
	FaultTCP      uint64 `json:"fault_tcp"`
	FaultDNS      uint64 `json:"fault_dns"`
}

type GateStatus struct {
	Epoch          uint64        `json:"epoch"`
	PolicyVerified bool          `json:"policy_verified"`
	Policy         string        `json:"policy"`
	RunID          string        `json:"run_id"`
	CheckID        string        `json:"check_id,omitempty"`
	CheckStartedAt time.Time     `json:"check_started_at,omitempty"`
	Configuration  string        `json:"configuration"`
	Mode           string        `json:"mode"`
	Fault          bool          `json:"fault"`
	Until          time.Time     `json:"until"`
	Reason         string        `json:"reason,omitempty"`
	Counters       GateCounters  `json:"counters"`
	Connections    int           `json:"connections"`
	Report         *SafetyReport `json:"report,omitempty"`
}

// EgressGate is owned by the hostagent, not by the guest or browser UI.
// Checking permits only the bounded synthetic diagnostic service. Public TCP
// and arbitrary DNS remain closed until the complete report is accepted.
type EgressGate struct {
	config                            Config
	mu                                sync.Mutex
	raw, diagnostic, failedDiagnostic *Egress
	state                             GateStatus
	epoch                             uint64
	epochContext                      context.Context
	cancel                            context.CancelFunc
	timer                             *time.Timer
	connections                       map[*gateConn]struct{}
	probeRequests                     int
	// window mirrors epoch, mode and Until for the reads and writes of open
	// connections, which must not wait on mu. It is replaced, under mu, in
	// the same critical section as every change of those fields.
	window atomic.Pointer[gateWindow]
}

// gateWindow is the authorization a connection checks: validLocked without mu.
type gateWindow struct {
	epoch uint64
	open  bool // mode is not GateClosed
	until time.Time
}

func (w *gateWindow) valid(epoch uint64) bool {
	return w != nil && w.epoch == epoch && w.open && time.Now().Before(w.until)
}

// Caller holds mu.
func (g *EgressGate) publishLocked() {
	g.window.Store(&gateWindow{epoch: g.epoch, open: g.state.Mode != GateClosed, until: g.state.Until})
}

func NewEgressGate(cfg Config) (*EgressGate, error) {
	raw, err := NewEgress(cfg)
	if err != nil {
		return nil, err
	}
	diagnostic, err := NewEgress(cfg)
	if err != nil {
		return nil, err
	}
	failed, err := NewEgress(cfg)
	if err != nil {
		return nil, err
	}
	failed.connect = unavailableProxy{}
	diagnostic.client.Transport.(*http.Transport).DisableKeepAlives = true
	failed.client.Transport.(*http.Transport).DisableKeepAlives = true
	ctx, cancel := context.WithCancel(context.Background())
	var id [16]byte
	if _, err = rand.Read(id[:]); err != nil {
		cancel()
		return nil, err
	}
	g := &EgressGate{config: cfg, raw: raw, diagnostic: diagnostic, failedDiagnostic: failed,
		epochContext: ctx, cancel: cancel, connections: make(map[*gateConn]struct{}),
		state: GateStatus{Policy: SafetyPolicyVersion, RunID: hex.EncodeToString(id[:]), Configuration: cfg.Fingerprint(), Mode: GateClosed, Reason: "等待关键检查"}}
	// DoH pools must obey revocation too; only this private transport can dial
	// the configured resolver hostname without accepting a guest-supplied host.
	raw.client.Transport.(*http.Transport).DialContext = g.dialDNS
	g.publishLocked() // g is not shared yet
	return g, nil
}

// SyncProxyRevision revokes every old connection/report while preserving the
// VM. A new proxy generation may carry its node's exit policy. Transports and
// guest settings stay identical. Completing a previously empty anchor is also
// allowed; both changes revoke authorization and require a fresh safety report.
func (g *EgressGate) SyncProxyRevision(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	g.mu.Lock()
	before, after := g.config, cfg
	oldPolicy, newPolicy := before.ExitPolicy.Normalized(), after.ExitPolicy.Normalized()
	initialBinding := oldPolicy.NeedsBinding() && !newPolicy.NeedsBinding() &&
		oldPolicy.Mode == newPolicy.Mode && oldPolicy.Scope == newPolicy.Scope &&
		(oldPolicy.IP == "" || oldPolicy.IP == newPolicy.IP) &&
		(oldPolicy.CountryCode == "" || oldPolicy.CountryCode == newPolicy.CountryCode) &&
		(oldPolicy.Region == "" || oldPolicy.Region == newPolicy.Region) &&
		(oldPolicy.City == "" || oldPolicy.City == newPolicy.City)
	if before.ProxyRevision != after.ProxyRevision || initialBinding {
		before.ExitPolicy, after.ExitPolicy = ExitPolicy{}, ExitPolicy{}
	}
	before.ProxyRevision, after.ProxyRevision = "", ""
	if !reflect.DeepEqual(before, after) || cfg.ProxyRevision == "" {
		g.mu.Unlock()
		return errors.New("出口地址或环境设置变化需要重启环境")
	}
	connections := g.transitionLocked(GateClosed, false, "代理节点已变更，等待重新检查")
	g.config = cfg
	g.state.Configuration = cfg.Fingerprint()
	g.state.Report = nil
	g.mu.Unlock()
	closeGateConnections(connections)
	return nil
}

func (g *EgressGate) Status() GateStatus {
	// The policy reads this machine's addresses: never while holding mu.
	verified := g.raw.Allows("tcp", netip.MustParseAddrPort("1.1.1.1:443")) &&
		!g.raw.Allows("udp", netip.MustParseAddrPort("1.1.1.1:3478")) &&
		!g.raw.Allows("icmp", netip.MustParseAddrPort("1.1.1.1:1")) &&
		!g.raw.Allows("tcp", netip.MustParseAddrPort("[2606:4700:4700::1111]:443")) &&
		!g.raw.Allows("tcp", netip.MustParseAddrPort("127.0.0.1:17777"))
	g.mu.Lock()
	defer g.mu.Unlock()
	s := g.state
	s.Epoch = g.epoch
	s.PolicyVerified = verified
	if s.Mode != GateClosed && !time.Now().Before(s.Until) {
		s.Mode = GateClosed
		s.Reason = "检查或监测授权已到期"
	}
	s.Connections = len(g.connections)
	if s.Report != nil {
		r := *s.Report
		r.Checks = append([]SafetyCheck(nil), r.Checks...)
		s.Report = &r
	}
	return s
}

func (g *EgressGate) validLocked(epoch uint64) bool {
	return g.epoch == epoch && g.state.Mode != GateClosed && time.Now().Before(g.state.Until)
}

func (g *EgressGate) armLocked(duration time.Duration) {
	g.armUntilLocked(time.Now().Add(duration))
}

func (g *EgressGate) armUntilLocked(until time.Time) {
	if g.timer != nil {
		g.timer.Stop()
	}
	g.state.Until = until
	g.publishLocked()
	epoch := g.epoch
	g.timer = time.AfterFunc(time.Until(until), func() {
		g.mu.Lock()
		if epoch != g.epoch || time.Now().Before(g.state.Until) {
			g.mu.Unlock()
			return
		}
		connections := g.transitionLocked(GateClosed, false, "检查或监测授权已到期")
		g.mu.Unlock()
		closeGateConnections(connections)
	})
}

// Caller holds mu; connection closure happens after unlocking. The epoch is
// invalidated before any connection can finish registering after a revoke.
func (g *EgressGate) transitionLocked(mode string, fault bool, reason string) []*gateConn {
	g.cancel()
	if g.timer != nil {
		g.timer.Stop()
	}
	g.epoch++
	g.epochContext, g.cancel = context.WithCancel(context.Background())
	g.state.Mode, g.state.Fault, g.state.Reason = mode, fault, reason
	g.state.Until = time.Time{}
	if mode == GateClosed {
		g.state.CheckID = ""
	}
	g.publishLocked()
	connections := make([]*gateConn, 0, len(g.connections))
	for c := range g.connections {
		connections = append(connections, c)
	}
	g.connections = make(map[*gateConn]struct{})
	return connections
}

func closeGateConnections(connections []*gateConn) {
	for _, c := range connections {
		_ = c.Close()
	}
}

func (g *EgressGate) Close(reason string) {
	g.mu.Lock()
	connections := g.transitionLocked(GateClosed, false, reason)
	g.mu.Unlock()
	closeGateConnections(connections)
}

func (g *EgressGate) BeginCheck() error {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return err
	}
	g.mu.Lock()
	connections := g.transitionLocked(GateChecking, false, "关键检查进行中")
	g.state.CheckID = hex.EncodeToString(id[:])
	g.state.CheckStartedAt = time.Now()
	g.probeRequests = 0
	g.armLocked(3 * time.Minute)
	g.mu.Unlock()
	closeGateConnections(connections)
	return nil
}

func (g *EgressGate) SetFault(fault bool) error {
	g.mu.Lock()
	if g.state.Mode != GateChecking || !g.validLocked(g.epoch) {
		g.mu.Unlock()
		return ErrGateClosed
	}
	until := g.state.Until
	connections := g.transitionLocked(GateChecking, fault, "关键检查进行中")
	g.armUntilLocked(until)
	g.mu.Unlock()
	closeGateConnections(connections)
	return nil
}

func (g *EgressGate) Grant(report SafetyReport) error {
	g.mu.Lock()
	if g.state.Mode != GateChecking || !g.validLocked(g.epoch) || report.CheckID != g.state.CheckID || report.CreatedAt.Before(g.state.CheckStartedAt) {
		g.mu.Unlock()
		return errors.New("检测会话已失效，请重新检测")
	}
	if err := report.Authorizes(g.state.RunID, g.state.Configuration, time.Now()); err != nil {
		g.mu.Unlock()
		return err
	}
	connections := g.transitionLocked(GateOpen, false, "")
	g.state.CheckID = ""
	report.Checks = append([]SafetyCheck(nil), report.Checks...)
	g.state.Report = &report
	g.armReportLeaseLocked(report)
	g.mu.Unlock()
	closeGateConnections(connections)
	return nil
}

func (g *EgressGate) Renew(reportID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state.Mode != GateOpen || !g.validLocked(g.epoch) || g.state.Report == nil || g.state.Report.ID != reportID {
		return ErrGateClosed
	}
	if err := g.state.Report.Authorizes(g.state.RunID, g.state.Configuration, time.Now()); err != nil {
		return err
	}
	g.armReportLeaseLocked(*g.state.Report)
	return nil
}

func (g *EgressGate) armReportLeaseLocked(report SafetyReport) {
	until := time.Now().Add(GateLease)
	if expiry := report.CreatedAt.Add(SafetyReportLifetime); expiry.Before(until) {
		until = expiry
	}
	g.armUntilLocked(until)
}

func (g *EgressGate) Allows(network string, dst netip.AddrPort) bool {
	// The policy depends only on dst and this machine's addresses, which it
	// reads for every flow. Decide it before taking mu, so that lookup never
	// holds up the gate or the reads and writes of open connections.
	diagnostic := network == "tcp" && dst.String() == DiagnosticAddress
	public := g.raw.Allows(network, dst)
	lan := !public && directLAN(g.config.DirectLAN, network, dst, directLANOffHost)
	// What a refusal counts as private. Only TCP can be public, and then the
	// same policy has just answered it.
	private := !public && !g.raw.public.Allows("tcp", dst)
	g.mu.Lock()
	defer g.mu.Unlock()
	active := g.validLocked(g.epoch)
	if active && g.state.Mode == GateChecking && diagnostic {
		return true
	}
	if active && g.state.Mode == GateOpen && (public || lan) {
		return true
	}
	switch network {
	case "tcp":
		g.state.Counters.TCPDenied++
	case "udp":
		g.state.Counters.UDPDenied++
	}
	if dst.Addr().Is6() {
		g.state.Counters.IPv6Denied++
	}
	if private {
		g.state.Counters.PrivateDenied++
	}
	return false
}

func (g *EgressGate) operation(ctx context.Context, diagnostic bool) (context.Context, func(), uint64, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.validLocked(g.epoch) || (!diagnostic && g.state.Mode != GateOpen) || (diagnostic && g.state.Mode != GateChecking) {
		return nil, nil, 0, ErrGateClosed
	}
	child, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(g.epochContext, cancel)
	return child, func() { stop(); cancel() }, g.epoch, nil
}

func (g *EgressGate) track(c net.Conn, epoch uint64) (net.Conn, error) {
	g.mu.Lock()
	if !g.validLocked(epoch) || len(g.connections) >= 1024 {
		g.mu.Unlock()
		_ = c.SetDeadline(time.Now())
		_ = c.Close()
		return nil, ErrGateClosed
	}
	wrapped := &gateConn{Conn: c, gate: g, epoch: epoch}
	g.connections[wrapped] = struct{}{}
	g.mu.Unlock()
	return wrapped, nil
}

func (g *EgressGate) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	dst, err := netip.ParseAddrPort(addr)
	if err != nil || (network != "tcp" && network != "tcp4" && network != "tcp6") {
		return nil, ErrGateClosed
	}
	if addr == DiagnosticAddress {
		return g.dialDiagnostic(ctx)
	}
	if !g.raw.Allows("tcp", dst) {
		if directLAN(g.config.DirectLAN, "tcp", dst, directLANOffHost) {
			return g.dialLAN(ctx, dst)
		}
		return nil, ErrGateClosed
	}
	return g.dialDNS(ctx, network, addr)
}

// dialLAN connects to the configured LAN directly, not through the proxy.
// Like proxied connections it needs open networking and ends with it.
func (g *EgressGate) dialLAN(ctx context.Context, dst netip.AddrPort) (net.Conn, error) {
	ctx, done, epoch, err := g.operation(ctx, false)
	if err != nil {
		return nil, err
	}
	defer done()
	c, err := directLANDial(ctx, "tcp4", dst.String())
	if err != nil {
		return nil, err
	}
	return g.track(c, epoch)
}

// Shared guarded dial; the resolver is the only caller allowed to supply names.
func (g *EgressGate) dialDNS(ctx context.Context, network, addr string) (net.Conn, error) {
	ctx, done, epoch, err := g.operation(ctx, false)
	if err != nil {
		return nil, err
	}
	defer done()
	c, err := g.raw.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	return g.track(c, epoch)
}

func (g *EgressGate) ExchangeDNS(ctx context.Context, query *dns.Msg) (*dns.Msg, error) {
	ctx, done, epoch, err := g.operation(ctx, false)
	if err != nil {
		g.mu.Lock()
		g.state.Counters.DNSDenied++
		g.mu.Unlock()
		return nil, err
	}
	defer done()
	r, err := g.raw.ExchangeDNS(ctx, query)
	g.mu.Lock()
	valid := g.validLocked(epoch)
	g.mu.Unlock()
	if !valid {
		return nil, ErrGateClosed
	}
	return r, err
}

type gateConn struct {
	net.Conn
	gate  *EgressGate
	epoch uint64
	once  sync.Once
}

// valid runs on every Read and Write of every guest connection: it reads the
// published window instead of waiting on mu.
func (c *gateConn) valid() bool {
	return c.gate.window.Load().valid(c.epoch)
}
func (c *gateConn) Read(b []byte) (int, error) {
	if !c.valid() {
		_ = c.Close()
		return 0, ErrGateClosed
	}
	return c.Conn.Read(b)
}
func (c *gateConn) Write(b []byte) (int, error) {
	if !c.valid() {
		_ = c.Close()
		return 0, ErrGateClosed
	}
	return c.Conn.Write(b)
}

func (c *gateConn) CloseWrite() error {
	return closeWrite(c.Conn)
}

func (c *gateConn) Close() error {
	var err error
	c.once.Do(func() {
		_ = c.Conn.SetDeadline(time.Now())
		err = c.Conn.Close()
		c.gate.mu.Lock()
		delete(c.gate.connections, c)
		c.gate.mu.Unlock()
	})
	return err
}

type unavailableProxy struct{}

func (unavailableProxy) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, errors.New("proxy outage drill")
}
