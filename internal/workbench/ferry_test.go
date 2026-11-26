package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sylvinus/agent-vm/internal/managedproxy"
)

const ferryTestSub = "https://ferry.example.com/sub/private-token?f=cage"

// fakeFerry answers /base/cage/v1/login like the panel does.
type fakeFerry struct {
	*httptest.Server
	mu     sync.Mutex
	logins int
}

func newFakeFerry(t *testing.T, exits []ferryExit) *fakeFerry {
	t.Helper()
	f := &fakeFerry{}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Path != "/base/cage/v1/login" {
			http.NotFound(w, r)
			return
		}
		f.logins++
		var in struct{ Username, Password string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Username != "张三" || in.Password != "correct horse" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"bad_login"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"user": "张三", "sub_url": ferryTestSub, "exits": exits})
	}))
	t.Cleanup(f.Close)
	old := ferryClientFor
	t.Cleanup(func() { ferryClientFor = old })
	ferryClientFor = func(*Server) (*http.Client, error) { return f.Client(), nil }
	return f
}

var ferryTestExits = []ferryExit{{Name: "🇦🇺 悉尼住宅", Region: "AU"}}

func TestFerryEndpointNeedsAFullHTTPSAddress(t *testing.T) {
	for _, bad := range []string{"http://ferry.example.com", "ferry.example.com", "https://user:pw@ferry.example.com", "https://ferry.example.com/?a=1", "https://ferry.example.com/#x", "https://"} {
		if _, err := ferryEndpoint(bad, "cage/v1/login"); err == nil {
			t.Error("accepted", bad)
		}
	}
	got, err := ferryEndpoint(" https://ferry.example.com:8443/panel/ ", "cage/v1/login")
	if err != nil || got != "https://ferry.example.com:8443/panel/cage/v1/login" {
		t.Fatal(got, err)
	}
}

// A sign-in returns the subscription and exits and remembers the address,
// the user and the exits for 「从 Ferry 导入」.
func TestFerrySignInRemembersTheUserAndExits(t *testing.T) {
	f := newFakeFerry(t, ferryTestExits)
	s := &Server{profile: t.TempDir()}
	reply, err := s.ferrySignIn(context.Background(), f.URL+"/base", " 张三 ", "correct horse")
	if err != nil || reply.SubURL != ferryTestSub || len(reply.Exits) != 1 || reply.Exits[0].Region != "AU" {
		t.Fatalf("%+v %v", reply, err)
	}
	rec := httptest.NewRecorder()
	s.ferryStatus(rec, httptest.NewRequest(http.MethodGet, "/api/ferry", nil))
	var status struct {
		Server, User string
		Exits        []ferryExit
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil || status.Server != f.URL+"/base" || status.User != "张三" || len(status.Exits) != 1 || strings.Contains(rec.Body.String(), "private-token") {
		t.Fatal(rec.Body.String(), err)
	}
}

func TestFerryLoginAnswersInWords(t *testing.T) {
	f := newFakeFerry(t, ferryTestExits)
	_, err := (&Server{}).ferrySignIn(context.Background(), f.URL+"/base", "张三", "wrong")
	var fe *ferryError
	if !errors.As(err, &fe) || fe.status != http.StatusUnprocessableEntity || fe.message != "Ferry 用户名或密码错误" {
		t.Fatal(err)
	}
	if _, err := (&Server{}).ferrySignIn(context.Background(), f.URL+"/base", "", "x"); err == nil || f.logins != 1 {
		t.Fatal("empty user name was sent", err)
	}
	for status, want := range map[int]string{http.StatusTooManyRequests: "Ferry 登录尝试过多，请稍后再试", http.StatusInternalServerError: "Ferry 返回 HTTP 500"} {
		if got := ferryStatusError(status, nil).Error(); got != want {
			t.Error(status, got)
		}
	}
	for code, want := range map[string]string{"disabled": "Ferry 账号已停用", "expired": "Ferry 账号已过期"} {
		if got := ferryStatusError(http.StatusForbidden, []byte(`{"error":"`+code+`"}`)).Error(); got != want {
			t.Error(code, got)
		}
	}
}

func TestFerryReplyIsChecked(t *testing.T) {
	good := ferryLoginReply{SubURL: ferryTestSub, Exits: ferryTestExits}
	if err := good.check(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*ferryLoginReply){
		"http link":       func(r *ferryLoginReply) { r.SubURL = "http://ferry.example.com/sub/x" },
		"no exits":        func(r *ferryLoginReply) { r.Exits = nil },
		"empty exit name": func(r *ferryLoginReply) { r.Exits = []ferryExit{{Name: " "}} },
		"bad region":      func(r *ferryLoginReply) { r.Exits = []ferryExit{{Name: "x", Region: "au"}} },
	} {
		reply := good
		change(&reply)
		if reply.check() == nil {
			t.Error("accepted", name)
		}
	}
}

func TestFerryLoginAPIImportsTheExitsWithoutShowingTheLink(t *testing.T) {
	s := testServer(t)
	f := newFakeFerry(t, ferryTestExits)
	oldImport, oldCore := ferryImport, proxyCorePath
	t.Cleanup(func() { ferryImport, proxyCorePath = oldImport, oldCore })
	proxyCorePath = func() (string, error) { return "mihomo", nil }
	var imported proxyInput
	ferryImport = func(_ *Server, input proxyInput, _ *uint64) error { imported = input; return nil }
	body, _ := json.Marshal(map[string]string{"server": f.URL + "/base", "username": "张三", "password": "correct horse"})
	w := request(s, "POST", "/api/ferry/login", s.origin, string(body), true)
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "private-token") {
		t.Fatal("subscription link returned to the page:", w.Body.String())
	}
	var out struct {
		Job    Job         `json:"job"`
		Source string      `json:"source"`
		Exits  []ferryExit `json:"exits"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Job.ID == "" || out.Source != "Ferry" || len(out.Exits) != 1 {
		t.Fatal(w.Body.String(), err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for s.jobs.snapshot().Running && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if imported.Name != "Ferry" || imported.URL != ferryTestSub || imported.Interval != 12 || imported.HostDownload || imported.Content != "" {
		t.Fatalf("exits not imported as a refreshed subscription through the route: %+v", imported)
	}
	w = request(s, "POST", "/api/ferry/login", s.origin, `{"server":"`+f.URL+`/base","username":"张三","password":"wrong"}`, true)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "Ferry 用户名或密码错误") {
		t.Fatal(w.Code, w.Body.String())
	}
}

// An update of the Ferry source that drops the exit in use moves the route to
// Ferry's new exit and keeps the relay; any other route is left alone.
func TestFerryUpdateMovesTheRouteToFerrysNewExit(t *testing.T) {
	nodes := func(names ...string) []byte {
		var b strings.Builder
		b.WriteString("proxies:\n")
		for i, name := range names {
			b.WriteString("  - {name: \"" + name + "\", type: socks5, server: 192.0.2." + strconv.Itoa(10+i) + ", port: 1080}\n")
		}
		return []byte(b.String())
	}
	setup := func(t *testing.T, ferrySource string) (*Server, managedproxy.Store) {
		t.Helper()
		profile := t.TempDir()
		p, err := managedproxy.Load(profile)
		if err != nil {
			t.Fatal(err)
		}
		if err = p.Import("前置订阅", "", nodes("🇦🇺 悉尼 01", "🇯🇵 东京 01"), "relay", 0, ""); err != nil {
			t.Fatal(err)
		}
		if err = p.Import(ferrySourceName, ferryTestSub, nodes("🇦🇺 AU", "🇦🇺 AU · SS"), "ferry", ferryRefreshHours, ""); err != nil {
			t.Fatal(err)
		}
		p.Chain = []string{ferryNodeID(t, p, "relay", "🇦🇺 悉尼 01"), ferryNodeID(t, p, "ferry", "🇦🇺 AU")}
		p.Selected = p.Chain[1]
		if _, err = p.Route(); err != nil {
			t.Fatal(err)
		}
		if err = saveFerryState(profile, ferryState{Source: ferrySource}); err != nil {
			t.Fatal(err)
		}
		return &Server{profile: profile}, p
	}
	update := proxyInput{ID: "ferry", Name: ferrySourceName, URL: ferryTestSub, Interval: ferryRefreshHours}
	index := func(p managedproxy.Store) int {
		for i, src := range p.Sources {
			if src.ID == "ferry" {
				return i
			}
		}
		return -1
	}

	// Renamed, with another region listed first: the same region wins.
	s, p := setup(t, "ferry")
	renamed := nodes("🇯🇵 JP", "🇦🇺 悉尼")
	if probe := p; probe.Import(update.Name, update.URL, renamed, update.ID, update.Interval, "") == nil {
		t.Fatal("a plain update should refuse to drop the exit in use")
	}
	next, followed := s.ferryFollowExit(p, index(p), update, renamed, "")
	if !followed || next.Selected != ferryNodeID(t, next, "ferry", "🇦🇺 悉尼") || len(next.Chain) != 2 || next.Chain[0] != p.Chain[0] || next.Chain[1] != next.Selected {
		t.Fatalf("followed=%v selected=%q chain=%q", followed, next.Selected, next.Chain)
	}
	if len(p.Sources[index(p)].Nodes) != 2 || p.Sources[index(p)].Nodes[0].Name != "🇦🇺 AU" {
		t.Fatal("the store given was changed")
	}
	// Moved to another region: Ferry's exit there.
	next, followed = s.ferryFollowExit(p, index(p), update, nodes("🇯🇵 JP"), "")
	if !followed || next.Selected != ferryNodeID(t, next, "ferry", "🇯🇵 JP") {
		t.Fatalf("followed=%v selected=%q", followed, next.Selected)
	}
	// The exit stays: the normal update.
	if _, followed = s.ferryFollowExit(p, index(p), update, nodes("🇦🇺 AU", "🇯🇵 JP"), ""); followed {
		t.Fatal("followed an update that keeps the exit")
	}
	// The user's own exit, or a source that is not Ferry's: left alone.
	own := p
	own.Chain, own.Selected = nil, ferryNodeID(t, p, "relay", "🇯🇵 东京 01")
	if _, followed = s.ferryFollowExit(own, index(own), update, renamed, ""); followed {
		t.Fatal("moved the user's own exit")
	}
	s, p = setup(t, "other")
	if _, followed = s.ferryFollowExit(p, index(p), update, renamed, ""); followed {
		t.Fatal("followed a source that is not Ferry's")
	}
	if flagRegion("🇦🇺 AU · SS") != "AU" || flagRegion("AU") != "" || flagRegion(" 🇯🇵 东京") != "JP" {
		t.Fatal("flagRegion")
	}
}

func ferryNodeID(t *testing.T, p managedproxy.Store, source, name string) string {
	t.Helper()
	for _, src := range p.Sources {
		if src.ID != source {
			continue
		}
		for _, n := range src.Nodes {
			if n.Name == name {
				return n.ID
			}
		}
	}
	t.Fatalf("no node %q in %s", name, source)
	return ""
}

// ferryChainProfile is a profile whose route is 前置订阅 → Ferry exit, with
// the Ferry source recorded in ferry.json.
func ferryChainProfile(t *testing.T) (*Server, managedproxy.Store) {
	t.Helper()
	profile := t.TempDir()
	if err := writePrivate(filepath.Join(profile, "config.json"), defaultConfig()); err != nil {
		t.Fatal(err)
	}
	p, err := managedproxy.Load(profile)
	if err != nil {
		t.Fatal(err)
	}
	relay := []byte("proxies:\n  - {name: \"🇦🇺 悉尼 01\", type: socks5, server: 192.0.2.1, port: 1080}\n")
	exits := []byte("proxies:\n  - {name: \"🇦🇺 AU\", type: socks5, server: 192.0.2.2, port: 1080}\n")
	if err = p.Import("前置订阅", "", relay, "relay", 0, ""); err != nil {
		t.Fatal(err)
	}
	if err = p.Import(ferrySourceName, ferryTestSub, exits, "ferry", ferryRefreshHours, ""); err != nil {
		t.Fatal(err)
	}
	p.Chain = []string{ferryNodeID(t, p, "relay", "🇦🇺 悉尼 01"), ferryNodeID(t, p, "ferry", "🇦🇺 AU")}
	p.Selected = p.Chain[1]
	p.Revision = "test-revision"
	if err = p.Save(profile); err != nil {
		t.Fatal(err)
	}
	cfg := defaultConfig()
	cfg.ProxyURL, cfg.ProxyRevision = p.Endpoint(), p.Revision
	if err = writePrivate(filepath.Join(profile, "config.json"), cfg); err != nil {
		t.Fatal(err)
	}
	if err = saveFerryState(profile, ferryState{Source: "ferry"}); err != nil {
		t.Fatal(err)
	}
	return &Server{profile: profile, configPath: filepath.Join(profile, "config.json")}, p
}

// Cage reaches Ferry through the route without its exit; only an update of
// the Ferry source uses that client.
func TestFerryIsReachedThroughTheRelayHop(t *testing.T) {
	s, p := ferryChainProfile(t)
	relayEndpoint, err := p.NodeEndpoint(p.Chain[0])
	if err != nil {
		t.Fatal(err)
	}
	if got, err := ferryHopEndpoint(p, p.Endpoint()); err != nil || got != relayEndpoint || got == p.Endpoint() {
		t.Fatal("a chain does not reach Ferry through its relay:", got, err)
	}
	single := p
	single.Chain = nil
	if got, err := ferryHopEndpoint(single, p.Endpoint()); err != nil || got != p.Endpoint() {
		t.Fatal("a route of one node is not used as it is:", got, err)
	}
	if _, err := s.ferryHopClient(); err != nil {
		t.Fatal(err)
	}
	ferry, relay := 1, 0
	if _, ok := s.ferrySourceClient("refresh", p, ferry, false); !ok {
		t.Fatal("the Ferry source is not updated through the relay hop")
	}
	for name, ok := range map[string]bool{
		"import":        func() bool { _, ok := s.ferrySourceClient("import", p, ferry, false); return ok }(),
		"host download": func() bool { _, ok := s.ferrySourceClient("refresh", p, ferry, true); return ok }(),
		"other source":  func() bool { _, ok := s.ferrySourceClient("refresh", p, relay, false); return ok }(),
	} {
		if ok {
			t.Error(name, "uses the Ferry client")
		}
	}
}

// Two failed probes of the Ferry exit in a row update the Ferry source at
// once; a working exit, or an exit that is not Ferry's, is left alone.
func TestFerryWithdrawnExitUpdatesTheFerrySourceAtOnce(t *testing.T) {
	s, p := ferryChainProfile(t)
	oldProbe, oldReady, oldUpdate := ferryProbe, ferryCoreReady, ferryUpdateNow
	t.Cleanup(func() { ferryProbe, ferryCoreReady, ferryUpdateNow = oldProbe, oldReady, oldUpdate })
	ferryCoreReady = func(context.Context, *Server, managedproxy.Store) bool { return true }
	var probeErr error
	probed, updated := []string{}, []string{}
	ferryProbe = func(_ context.Context, _ string, id string) error { probed = append(probed, id); return probeErr }
	ferryUpdateNow = func(_ *Server, id string) bool { updated = append(updated, id); return true }

	if wait := s.ferryWatchOnce(); wait != ferryWatchEvery || len(updated) != 0 || len(probed) != 1 || probed[0] != p.Selected {
		t.Fatal("working exit", wait, probed, updated)
	}
	probeErr = errors.New("connect error")
	if wait := s.ferryWatchOnce(); wait != ferryWatchRetry || len(updated) != 0 {
		t.Fatal("one failure updates", wait, updated)
	}
	if wait := s.ferryWatchOnce(); wait != ferryWatchEvery || len(updated) != 1 || updated[0] != "ferry" {
		t.Fatal("two failures in a row do not update the Ferry source", wait, updated)
	}
	// Counting starts again after an update.
	if s.ferryWatchOnce(); len(updated) != 1 {
		t.Fatal("updated again after one more failure", updated)
	}
	// The user's own exit is not probed.
	own := p
	own.Chain, own.Selected = nil, p.Chain[0]
	if err := own.Save(s.profile); err != nil {
		t.Fatal(err)
	}
	probed = nil
	s.ferryWatchOnce()
	if s.ferryWatchOnce(); len(probed) != 0 || len(updated) != 1 {
		t.Fatal("probed an exit that is not Ferry's", probed, updated)
	}
}

// An expired or disabled Ferry user still gets the subscription but no
// node lets them in: the watcher stops updating, and updates once when the
// exit works again so the expiry shown is current.
func TestFerryExpiredUserIsNotUpdatedAgainAndAgain(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	for usage, want := range map[string]bool{"": false, "upload=1; download=2; total=3": false, "expire=0": false, "expire=1": true, "total=5; expire=1799999999": true, "expire=1800000001": false, "expire=x": false} {
		if usageExpired(usage, now) != want {
			t.Error(usage, !want)
		}
	}
	s, p := ferryChainProfile(t)
	p.Sources[1].Usage = "upload=0; download=0; total=0; expire=1"
	if err := p.Save(s.profile); err != nil {
		t.Fatal(err)
	}
	oldProbe, oldReady, oldUpdate := ferryProbe, ferryCoreReady, ferryUpdateNow
	t.Cleanup(func() { ferryProbe, ferryCoreReady, ferryUpdateNow = oldProbe, oldReady, oldUpdate })
	ferryCoreReady = func(context.Context, *Server, managedproxy.Store) bool { return true }
	probeErr := errors.New("connect error")
	updated := 0
	ferryProbe = func(context.Context, string, string) error { return probeErr }
	ferryUpdateNow = func(*Server, string) bool { updated++; return true }
	for range 4 {
		s.ferryWatchOnce()
	}
	if updated != 0 {
		t.Fatal("an expired user's subscription was updated", updated)
	}
	probeErr = nil
	if s.ferryWatchOnce(); updated != 1 {
		t.Fatal("a renewed user's subscription was not updated", updated)
	}
	// The update brought the same expiry (a disabled user the node still
	// lets in): not updated again every round.
	for range 3 {
		s.ferryWatchOnce()
	}
	if updated != 1 {
		t.Fatal("an expired user whose exit works was updated every round", updated)
	}
	s.ferryWatch.expiredUpdate = time.Now().Add(-ferryExpiredUpdateEvery)
	if s.ferryWatchOnce(); updated != 2 {
		t.Fatal("not updated again after an hour", updated)
	}
}

// A sign-in updates the Ferry source in place, never a source of the user's
// that only shares its name.
func TestFerrySignInUpdatesOnlyItsOwnSource(t *testing.T) {
	var p managedproxy.Store
	p.Sources = []managedproxy.Source{
		{ID: "mine", Name: ferrySourceName, URL: "https://example.com/colleague"},
		{ID: "local", Name: ferrySourceName},
		{ID: "ferry", Name: ferrySourceName, URL: "https://ferry.example/old-token"},
	}
	input := proxyInput{Name: ferrySourceName, URL: ferryTestSub}
	if i := ferryKnownSource(p, ferryState{Source: "ferry"}, input); i != 2 {
		t.Fatal("the recorded Ferry source with an older link is not updated in place", i)
	}
	if i := ferryKnownSource(p, ferryState{}, input); i != -1 {
		t.Fatal("a source that only shares the name would be overwritten", i)
	}
	p.Sources[0].URL = ferryTestSub
	if i := ferryKnownSource(p, ferryState{Source: "gone"}, input); i != 0 {
		t.Fatal("the same subscription under Ferry's name is not reused", i)
	}
}
