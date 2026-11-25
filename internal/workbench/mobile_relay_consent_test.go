package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sylvinus/agent-vm/internal/mobilelink"
	"github.com/sylvinus/agent-vm/internal/relay"
	"github.com/sylvinus/agent-vm/internal/remotelink"
)

// #740: the official relay is a third-party server reached over the host
// network. Generating a pairing code must not contact it until the user has
// confirmed that address, and an existing pairing must show which relay it
// keeps contacting and stop once removed.
func TestOfficialRelayNeedsConfirmation(t *testing.T) {
	edge, err := relay.New(relay.Config{PublicRooms: true, AdminToken: strings.Repeat("x", 32)})
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	relayHTTP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		edge.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { edge.Close(); relayHTTP.Close() })
	official := relayHTTP.URL
	previous := officialMobileRelay
	officialMobileRelay = official
	t.Cleanup(func() { officialMobileRelay = previous })

	s, _ := grantedToolUpdateServer(t)
	server := httptest.NewUnstartedServer(s.handler())
	s.origin = "http://" + server.Listener.Addr().String()
	server.Start()
	t.Cleanup(server.Close)
	t.Cleanup(s.stopPhone)
	manage := func(method, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, s.origin+"/api/mobile-pair", strings.NewReader(body))
		r.Header.Set("Origin", s.origin)
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(&http.Cookie{Name: s.cookieName(), Value: s.session})
		r.AddCookie(&http.Cookie{Name: s.cookieName() + "-desktop", Value: trustedViewProof(s.token)})
		w := httptest.NewRecorder()
		s.handler().ServeHTTP(w, r)
		return w
	}
	status := func() map[string]any {
		t.Helper()
		w := manage("GET", "")
		var reply map[string]any
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &reply) != nil {
			t.Fatalf("pairing status: HTTP %d", w.Code)
		}
		return reply
	}
	host := mustURL(t, official).Host

	// The settings page names the relay before anything is sent to it.
	if got := status()["official_relay"]; got != host {
		t.Fatalf("official relay shown as %v, want %s", got, host)
	}
	for _, body := range []string{`{}`, `{"confirm":"203.0.113.7"}`, `{"confirm":"` + official + `"}`, `{"relay":"` + official + `"}`} {
		if w := manage("POST", body); w.Code != http.StatusPreconditionRequired {
			t.Fatalf("%s: unconfirmed official relay answered HTTP %d", body, w.Code)
		}
	}
	if n := requests.Load(); n != 0 {
		t.Fatalf("relay contacted %d times before the user confirmed it", n)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := manage("POST", `{"confirm":"`+host+`"}`)
	var code struct{ Manual string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &code) != nil || code.Manual == "" {
		t.Fatalf("confirmed official relay: HTTP %d %s", w.Code, w.Body.String())
	}
	if requests.Load() == 0 {
		t.Fatal("confirmed pairing did not reach the relay")
	}
	phone, err := remotelink.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := mobilelink.RedeemPair(ctx, code.Manual, official, phone.Pin)
	if err != nil {
		t.Fatal(err)
	}
	var record mobilelink.PairedHost
	if err := json.Unmarshal([]byte(credentials), &record); err != nil {
		t.Fatal(err)
	}

	// A paired phone keeps a room open on the relay: say which and its state.
	firstDevice := func(reply map[string]any) map[string]any {
		devices, _ := reply["devices"].([]any)
		if len(devices) == 0 {
			return nil
		}
		device, _ := devices[0].(map[string]any)
		return device
	}
	for {
		reply := status()
		if device := firstDevice(reply); device != nil && device["relay"] == host && device["link"] == "已连接" {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("paired status never showed the relay link: %v", reply)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := mobilelink.LookupRoom(ctx, official, record.Lookup); err != nil {
		t.Fatalf("paired host is not reachable through the relay: %v", err)
	}

	// Removing the phone stops every later request to the relay.
	if w := manage("DELETE", "{}"); w.Code != 200 {
		t.Fatalf("remove phone: HTTP %d", w.Code)
	}
	if reply := status(); firstDevice(reply) != nil || reply["paired"] != false {
		t.Fatalf("removed pairing still reports a relay link: %v", reply)
	}
	for {
		_, err := mobilelink.LookupRoom(ctx, official, record.Lookup)
		if errors.Is(err, mobilelink.ErrNoInvitation) {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("removed host still serves a room: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	settled := requests.Load()
	// The reconnect loop retries after one second at first.
	time.Sleep(2500 * time.Millisecond)
	if n := requests.Load(); n != settled {
		t.Fatalf("relay contacted %d more times after the phone was removed", n-settled)
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
