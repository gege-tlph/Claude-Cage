package workbench

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/sylvinus/agent-vm/internal/cage"
	"github.com/sylvinus/agent-vm/internal/diagnostics"
	"github.com/sylvinus/agent-vm/internal/managedproxy"
)

// Ferry is a sing-box panel an organisation runs for its exits. In the
// "从 Ferry 配置" first run the employee's own subscription is imported and
// applied first, then Ferry is reached through that route only, so a panel
// the host network cannot reach still works:
//
//	POST <server>/cage/v1/login  {"username","password"}
//	  -> {"user","sub_url","exits":[{"name","region"}]}
const (
	ferryMaxReply = 1 << 20
	ferryMaxExits = 300
)

var ferryRegionCode = regexp.MustCompile(`^[A-Z]{2}$`)

// ferryClientFor gives the client Ferry requests use: the relay hop of the
// applied route (see ferryHopClient), never the host network. Tests replace it.
var ferryClientFor = func(s *Server) (*http.Client, error) {
	client, err := s.ferryHopClient()
	if err != nil {
		return nil, err
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("Ferry 地址发生跳转，已停止")
	}
	return client, nil
}

// ferryImport saves the exits as a source, as POST /api/proxy/import does.
// Tests replace it.
var ferryImport = func(s *Server, input proxyInput, pauseVersion *uint64) error {
	return s.ferrySaveExits(input, pauseVersion)
}

// ferrySaveExits imports the exits, or on a later sign-in updates the source
// imported before in place: its node IDs, and so the chain, stay.
func (s *Server) ferrySaveExits(input proxyInput, pauseVersion *uint64) error {
	p, err := managedproxy.Load(s.profile)
	if err != nil {
		return err
	}
	if i := ferryKnownSource(p, loadFerryState(s.profile), input); i >= 0 {
		src := p.Sources[i]
		if src.URL != input.URL {
			if err := s.changeProxy("edit", proxyInput{ID: src.ID, Name: src.Name, URL: input.URL, Interval: src.Interval}, pauseVersion); err != nil {
				return err
			}
		}
		err := s.changeProxy("refresh", proxyInput{ID: src.ID}, pauseVersion)
		s.ferryRememberSource(src.ID)
		return err
	}
	if err := s.changeProxy("import", input, pauseVersion); err != nil {
		return err
	}
	if p, err := managedproxy.Load(s.profile); err == nil {
		for i := len(p.Sources) - 1; i >= 0; i-- {
			if p.Sources[i].Name == input.Name && p.Sources[i].URL == input.URL {
				s.ferryRememberSource(p.Sources[i].ID)
				break
			}
		}
	}
	return nil
}

// ferryKnownSource is the source a sign-in updates in place, or -1: the one
// ferry.json names, else one with Ferry's name and this very subscription. A
// source of the user's that only shares the name keeps its subscription.
func ferryKnownSource(p managedproxy.Store, state ferryState, input proxyInput) int {
	if i := ferrySourceIndex(p, state); i >= 0 && p.Sources[i].URL != "" {
		return i
	}
	for i, src := range p.Sources {
		if src.Name == input.Name && src.URL != "" && src.URL == input.URL {
			return i
		}
	}
	return -1
}

func (s *Server) ferryRememberSource(id string) {
	state := loadFerryState(s.profile)
	if state.Source == id {
		return
	}
	state.Source = id
	if err := saveFerryState(s.profile, state); err != nil {
		diagnostics.Log("warn", "ferry", "Ferry 状态未能保存："+err.Error())
	}
}

// ferryFollowExit handles an update of the Ferry source that drops the exit
// the route uses: the organisation renamed it or moved the user to another
// exit. Instead of the update being refused while the old exit may stop
// working, the route moves to Ferry's new exit (same region, then same
// protocol, then Ferry's order) and keeps its relay hops, fixed or auto. A
// route whose exit is not Ferry's is the user's choice and is left alone.
func (s *Server) ferryFollowExitOn(action string, p managedproxy.Store, index int, in proxyInput, data []byte, usage string) (managedproxy.Store, bool) {
	if action != "refresh" {
		return p, false
	}
	return s.ferryFollowExit(p, index, in, data, usage)
}

func (s *Server) ferryFollowExit(p managedproxy.Store, index int, in proxyInput, data []byte, usage string) (managedproxy.Store, bool) {
	if index < 0 || p.Selected == "" {
		return p, false
	}
	src := p.Sources[index]
	if state := loadFerryState(s.profile); state.Source == "" || state.Source != src.ID {
		return p, false
	}
	var old managedproxy.Node
	found := false
	for _, n := range src.Nodes {
		if n.ID == p.Selected {
			old, found = n, true
		}
	}
	if !found {
		return p, false
	}
	next := p
	next.Sources = append([]managedproxy.Source(nil), p.Sources...)
	chain := append([]string(nil), p.Chain...)
	hops := copyRouteHops(p.RouteHops)
	next.Selected, next.Chain, next.RouteHops = "", nil, nil
	if err := next.Import(in.Name, in.URL, data, in.ID, in.Interval, usage); err != nil {
		return p, false
	}
	if _, kept := next.Find(old.ID); kept {
		return p, false
	}
	// An auto 前置's chosen nodes lose the ones this update dropped, as a
	// plain update of the source does; the new exit is none of them.
	gone := map[string]bool{}
	for _, n := range src.Nodes {
		gone[n.ID] = true
	}
	for _, n := range next.Sources[index].Nodes {
		delete(gone, n.ID)
	}
	pooled := map[string]bool{}
	for _, hop := range hops {
		if hop.Relay == nil || len(hop.Relay.NodeIDs) == 0 {
			continue
		}
		kept := make([]string, 0, len(hop.Relay.NodeIDs))
		for _, id := range hop.Relay.NodeIDs {
			if !gone[id] {
				kept = append(kept, id)
				pooled[id] = true
			}
		}
		if len(kept) == 0 {
			return p, false
		}
		hop.Relay.NodeIDs = kept
	}
	blocked := next.BlockedNodes()
	region := flagRegion(old.Name)
	exit, best := "", -1
	for _, n := range next.Sources[index].Nodes {
		if blocked[n.ID] != "" || pooled[n.ID] {
			continue
		}
		score := 0
		if region != "" && flagRegion(n.Name) == region {
			score += 2
		}
		if n.Type == old.Type {
			score++
		}
		if score > best {
			exit, best = n.ID, score
		}
	}
	if exit == "" {
		return p, false
	}
	if len(hops) > 0 {
		hops[len(hops)-1].NodeID = exit
		next.Selected, next.RouteHops = exit, hops
	} else {
		if len(chain) > 0 {
			chain[len(chain)-1] = exit
		}
		next.Selected, next.Chain = exit, chain
	}
	if _, err := next.Route(); err != nil {
		return p, false
	}
	diagnostics.Log("info", "ferry", "Ferry 的出口已变更，代理链改用新出口")
	return next, true
}

// copyRouteHops copies hops deep enough to change a hop's node or a group's
// chosen nodes without touching the store they came from.
func copyRouteHops(hops []managedproxy.RouteHop) []managedproxy.RouteHop {
	out := make([]managedproxy.RouteHop, len(hops))
	for i, h := range hops {
		out[i].NodeID = h.NodeID
		if h.Relay != nil {
			relay := *h.Relay
			relay.NodeIDs = append([]string(nil), h.Relay.NodeIDs...)
			out[i].Relay = &relay
		}
	}
	return out
}

// flagRegion reads the flag a Ferry node name starts with ("🇦🇺 悉尼") as
// its region code.
func flagRegion(name string) string {
	r := []rune(strings.TrimSpace(name))
	const a, z = 0x1F1E6, 0x1F1FF
	if len(r) >= 2 && r[0] >= a && r[0] <= z && r[1] >= a && r[1] <= z {
		return string([]rune{'A' + r[0] - a, 'A' + r[1] - a})
	}
	return ""
}

// ferryState is what the profile keeps of Ferry (ferry.json): the address and
// user last signed in with, their exits, and the source the exits were
// imported as.
type ferryState struct {
	Server string      `json:"server,omitempty"`
	User   string      `json:"user,omitempty"`
	Exits  []ferryExit `json:"exits,omitempty"`
	Source string      `json:"source,omitempty"`
}

func loadFerryState(profile string) ferryState {
	var state ferryState
	if profile == "" {
		return state
	}
	if raw, err := os.ReadFile(filepath.Join(profile, "ferry.json")); err == nil && len(raw) < 64<<10 {
		_ = json.Unmarshal(raw, &state)
	}
	return state
}

func saveFerryState(profile string, state ferryState) error {
	if profile == "" {
		return nil
	}
	return writePrivate(filepath.Join(profile, "ferry.json"), state)
}

// ferryStatus is GET /api/ferry: the address and user to prefill, the exits
// of the last sign-in and the source they were imported as.
func (s *Server) ferryStatus(w http.ResponseWriter, r *http.Request) {
	state := loadFerryState(s.profile)
	exits := state.Exits
	if exits == nil {
		exits = []ferryExit{}
	}
	jsonReply(w, map[string]any{"server": state.Server, "user": state.User, "exits": exits, "source": state.Source})
}

type ferryExit struct {
	Name   string `json:"name"`
	Region string `json:"region"`
}

type ferryLoginReply struct {
	User   string      `json:"user"`
	SubURL string      `json:"sub_url"`
	Exits  []ferryExit `json:"exits"`
}

// ferryError is a Ferry answer shown to the user as it is; status is what
// the workbench API answers with.
type ferryError struct {
	status  int
	message string
}

func (e *ferryError) Error() string { return e.message }

// ferryEndpoint joins an API path onto the server address the user entered.
func ferryEndpoint(server, path string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(server))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.Opaque != "" {
		return "", &ferryError{http.StatusBadRequest, "Ferry 地址需要完整的 HTTPS 网址"}
	}
	return u.JoinPath(path).String(), nil
}

func (s *Server) ferryLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Server   string `json:"server"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in, 64<<10) {
		return
	}
	reply, err := s.ferrySignIn(r.Context(), in.Server, in.Username, in.Password)
	if err != nil {
		var fe *ferryError
		if errors.As(err, &fe) {
			http.Error(w, fe.message, fe.status)
			return
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	// The subscription link is a credential: like every saved source it stays
	// in the backend (proxy status shows only its host). The exits are
	// imported here as a source of their own, through the applied route.
	input := proxyInput{Name: ferrySourceName, URL: reply.SubURL, Interval: ferryRefreshHours}
	if err := checkProxyInput("import", input); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if _, err := proxyCorePath(); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	s.mu.Lock()
	pauseVersion := s.safety.pauseVersion
	s.mu.Unlock()
	job, err := s.submitJobWithInitiator(requestInitiator(r), "proxy", func() error { return ferryImport(s, input, &pauseVersion) })
	switch {
	case errors.Is(err, errJobsClosed):
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	case errors.Is(err, errJobBusy):
		http.Error(w, err.Error(), http.StatusConflict)
		return
	case err != nil:
		requestError(w, err)
		return
	}
	jsonReply(w, map[string]any{"job": job, "user": reply.User, "source": ferrySourceName, "exits": reply.Exits})
}

// The source the exits are imported as, and how often it is refreshed
// (Ferry answers with Profile-Update-Interval: 12).
const (
	ferrySourceName   = "Ferry"
	ferryRefreshHours = 12
)

// ferrySignIn signs in and remembers the address, the user and their exits.
func (s *Server) ferrySignIn(ctx context.Context, server, username, password string) (ferryLoginReply, error) {
	login, err := ferryEndpoint(server, "cage/v1/login")
	if err != nil {
		return ferryLoginReply{}, err
	}
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return ferryLoginReply{}, &ferryError{http.StatusBadRequest, "请填写 Ferry 用户名和密码"}
	}
	client, err := ferryClientFor(s)
	if err != nil {
		return ferryLoginReply{}, &ferryError{http.StatusConflict, err.Error()}
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	reply, err := ferryPostLogin(ctx, client, login, username, password)
	if err != nil {
		return ferryLoginReply{}, err
	}
	state := loadFerryState(s.profile)
	state.Server, state.User, state.Exits = strings.TrimSpace(server), username, reply.Exits
	if err := saveFerryState(s.profile, state); err != nil {
		diagnostics.Log("warn", "ferry", "Ferry 状态未能保存："+err.Error())
	}
	return reply, nil
}

func ferryPostLogin(ctx context.Context, client *http.Client, endpoint, username, password string) (ferryLoginReply, error) {
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	status, raw, err := ferryPost(ctx, client, endpoint, body)
	if err != nil {
		return ferryLoginReply{}, err
	}
	if status != http.StatusOK {
		return ferryLoginReply{}, ferryStatusError(status, raw)
	}
	var reply ferryLoginReply
	if err := json.Unmarshal(raw, &reply); err != nil {
		return ferryLoginReply{}, errors.New("Ferry 的回答无法识别")
	}
	if err := reply.check(); err != nil {
		return ferryLoginReply{}, err
	}
	return reply, nil
}

func (r ferryLoginReply) check() error {
	if !subscriptionURL(r.SubURL) {
		return errors.New("Ferry 返回的订阅链接无效")
	}
	if len(r.Exits) == 0 {
		return &ferryError{http.StatusConflict, "Ferry 账号没有可用出口"}
	}
	if len(r.Exits) > ferryMaxExits {
		return errors.New("Ferry 返回的出口过多")
	}
	for _, exit := range r.Exits {
		if strings.TrimSpace(exit.Name) == "" || len(exit.Name) > 240 || !utf8.ValidString(exit.Name) || exit.Region != "" && !ferryRegionCode.MatchString(exit.Region) {
			return errors.New("Ferry 返回的出口无效")
		}
	}
	return nil
}

// ferryStatusError turns Ferry's {"error":code} into words.
func ferryStatusError(status int, raw []byte) error {
	var answer struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &answer)
	switch {
	case status == http.StatusUnauthorized || answer.Error == "bad_login":
		return &ferryError{http.StatusUnprocessableEntity, "Ferry 用户名或密码错误"}
	case answer.Error == "disabled":
		return &ferryError{http.StatusForbidden, "Ferry 账号已停用"}
	case answer.Error == "expired":
		return &ferryError{http.StatusForbidden, "Ferry 账号已过期"}
	case status == http.StatusTooManyRequests:
		return &ferryError{http.StatusTooManyRequests, "Ferry 登录尝试过多，请稍后再试"}
	}
	return fmt.Errorf("Ferry 返回 HTTP %d", status)
}

func ferryPost(ctx context.Context, client *http.Client, endpoint string, body []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, errors.New("连接 Ferry 超时")
		}
		return 0, nil, errors.New("无法连接 Ferry")
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, ferryMaxReply+1))
	if err != nil || len(raw) > ferryMaxReply {
		return 0, nil, errors.New("Ferry 的回答中断或过大")
	}
	return res.StatusCode, raw, nil
}

func sourceHasNode(src managedproxy.Source, id string) bool {
	for _, n := range src.Nodes {
		if n.ID == id {
			return true
		}
	}
	return false
}

// ferrySourceIndex is where the Ferry source is in p, or -1.
func ferrySourceIndex(p managedproxy.Store, state ferryState) int {
	if state.Source == "" {
		return -1
	}
	for i, src := range p.Sources {
		if src.ID == state.Source {
			return i
		}
	}
	return -1
}

// Everything Cage says to Ferry — the sign-in and the exits' subscription —
// goes through the relay hop alone: the applied route without
// its exit, the way the first run reached Ferry. An exit that stopped working
// (Ferry withdrew it, or the user's own exit) then cannot cut Cage off from
// Ferry and the replacement it offers. A route of one node is used as it is.
func (s *Server) ferryHopClient() (*http.Client, error) {
	cfg, err := loadConfig(s.configPath)
	if err != nil {
		return nil, err
	}
	p, err := managedproxy.Applied(s.profile, cfg.ProxyURL, cfg.ProxyRevision)
	if err != nil {
		return nil, err
	}
	active := func(group string) string {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return readRelayGroupActive(ctx, s.profile, group)
	}
	if cfg.ProxyURL, err = ferryHopEndpoint(p, cfg.ProxyURL, active); err != nil {
		return nil, err
	}
	e, err := cage.NewEgress(cfg)
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: &http.Transport{Proxy: nil, DialContext: e.DialContext}, Timeout: 30 * time.Second, CheckRedirect: managedproxy.SubscriptionRedirect}, nil
}

// ferryHopEndpoint is the core endpoint of the route without its exit: the
// credentials of the hop before the exit, or the route's own for one node.
// For an auto 前置 there that is the node it uses now (active names it from
// the core's group), or its first candidate while the core does not say.
func ferryHopEndpoint(p managedproxy.Store, route string, active func(group string) string) (string, error) {
	if len(p.RouteHops) >= 2 {
		hops, err := p.ResolveRoute()
		if err != nil {
			return "", err
		}
		hop := hops[len(hops)-2]
		if hop.Relay == nil {
			return p.NodeEndpoint(hop.NodeID)
		}
		if id := active(hop.GroupName); slices.Contains(hop.Candidates, id) {
			return p.NodeEndpoint(id)
		}
		return p.NodeEndpoint(hop.Candidates[0]) // ResolveRoute leaves a group one at least
	}
	if len(p.Chain) < 2 {
		return route, nil
	}
	return p.NodeEndpoint(p.Chain[len(p.Chain)-2])
}

// ferrySourceClient is the client for an update of the Ferry source.
func (s *Server) ferrySourceClient(action string, p managedproxy.Store, index int, host bool) (*http.Client, bool) {
	if action != "refresh" || host || index < 0 || ferrySourceIndex(p, loadFerryState(s.profile)) != index {
		return nil, false
	}
	client, err := s.ferryHopClient()
	if err != nil {
		return nil, false
	}
	return client, true
}

// The Ferry exit in use is probed through its whole route every few minutes.
// After two failed probes in a row the Ferry source is updated at once
// (through the relay, see ferrySourceClient), so an exit Ferry withdrew — the
// user moved to another department, the node disabled — is replaced within
// minutes instead of at the next 12-hour update. Tests replace these.
var (
	ferryWatchEvery = 5 * time.Minute
	ferryWatchRetry = 30 * time.Second
	// While an expired account's exit still works, the subscription is
	// updated at most this often: Ferry may keep sending the old expiry.
	ferryExpiredUpdateEvery = time.Hour
	ferryProbe              = func(ctx context.Context, profile, nodeID string) error {
		_, _, err := measureProxyDelay(ctx, profile, nodeID)
		return err
	}
	ferryCoreReady = func(ctx context.Context, s *Server, p managedproxy.Store) bool {
		cfg, err := loadConfig(s.configPath)
		if err != nil {
			return false
		}
		if _, err := managedproxy.Applied(s.profile, cfg.ProxyURL, cfg.ProxyRevision); err != nil {
			return false
		}
		return managedproxy.Running(ctx, s.profile, p)
	}
	ferryUpdateNow = func(s *Server, sourceID string) bool {
		_, err := s.submitJob("proxy-update", func() error { return s.changeProxy("refresh", proxyInput{ID: sourceID}, nil) })
		return err == nil
	}
)

type ferryWatchState struct {
	mu       sync.Mutex
	next     time.Time
	failures int
	running  bool
	// The last update for an expired account whose exit worked.
	expiredUpdate time.Time
}

// ferryWatchExit starts a probe when one is due. The network monitor calls
// it every round; the probe runs on its own so the round is not held up.
func (s *Server) ferryWatchExit() {
	w := &s.ferryWatch
	w.mu.Lock()
	if w.running || time.Now().Before(w.next) {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.mu.Unlock()
	go func() {
		wait := s.ferryWatchOnce()
		w.mu.Lock()
		w.running, w.next = false, time.Now().Add(wait)
		w.mu.Unlock()
	}()
}

// ferryWatchOnce probes the Ferry exit once and says when to probe again.
func (s *Server) ferryWatchOnce() time.Duration {
	w := &s.ferryWatch
	failed := func(n int) {
		w.mu.Lock()
		w.failures = n
		w.mu.Unlock()
	}
	p, err := managedproxy.Load(s.profile)
	if err != nil {
		return ferryWatchEvery
	}
	index := ferrySourceIndex(p, loadFerryState(s.profile))
	if index < 0 || !sourceHasNode(p.Sources[index], p.Selected) {
		failed(0)
		return ferryWatchEvery
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if !ferryCoreReady(ctx, s, p) {
		// Look again soon: the core may be about to start.
		failed(0)
		return ferryWatchRetry
	}
	expired := usageExpired(p.Sources[index].Usage, time.Now())
	if ferryProbe(ctx, s.profile, p.Selected) == nil {
		failed(0)
		// Working again after the account expired: renewed. Update the
		// subscription so its expiry, which the card shows, is current.
		// A disabled user whose node has not cut them off yet keeps the old
		// expiry, so this is not repeated every round.
		w.mu.Lock()
		due := time.Since(w.expiredUpdate) >= ferryExpiredUpdateEvery
		w.mu.Unlock()
		if expired && due && !s.busy() && ferryUpdateNow(s, p.Sources[index].ID) {
			w.mu.Lock()
			w.expiredUpdate = time.Now()
			w.mu.Unlock()
		}
		return ferryWatchEvery
	}
	if expired {
		// Ferry still serves the subscription of an expired or disabled user
		// but its nodes refuse them: updating would change nothing.
		failed(0)
		return ferryWatchEvery
	}
	w.mu.Lock()
	w.failures++
	failures := w.failures
	w.mu.Unlock()
	if failures < 2 || s.busy() || !ferryUpdateNow(s, p.Sources[index].ID) {
		return ferryWatchRetry
	}
	failed(0)
	diagnostics.Log("warn", "ferry", "Ferry 出口连不上，已提前更新 Ferry 订阅")
	return ferryWatchEvery
}

// usageExpired says the subscription's user has expired: its
// Subscription-Userinfo expire is past. Ferry gives a disabled user expire=1.
func usageExpired(usage string, now time.Time) bool {
	for _, field := range strings.Split(usage, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(field), "=")
		if ok && key == "expire" {
			n, err := strconv.ParseInt(value, 10, 64)
			return err == nil && n > 0 && n <= now.Unix()
		}
	}
	return false
}
