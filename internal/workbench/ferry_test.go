package workbench

import (
	"context"
	"crypto/ecdh"
	"crypto/hpke"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const ferryTestSub = "https://ferry.example.com/sub/private-token?f=cage"

// fakeFerry answers /base/cage/v1/login and /escrow like the panel does, and
// keeps the last sealed recovery key it was given.
type fakeFerry struct {
	*httptest.Server
	mu      sync.Mutex
	sealed  []byte
	device  string
	logins  int
	escrows int
}

func newFakeFerry(t *testing.T, escrowKey string, exits []ferryExit) *fakeFerry {
	t.Helper()
	f := &fakeFerry{}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case "/base/cage/v1/login":
			f.logins++
			var in struct{ Username, Password string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in.Username != "张三" || in.Password != "correct horse" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"bad_login"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"user": "张三", "token": "session-token", "sub_url": ferryTestSub, "exits": exits, "escrow_key": escrowKey})
		case "/base/cage/v1/escrow":
			f.escrows++
			if r.Header.Get("Authorization") != "Bearer session-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			var in struct{ Ciphertext, Username, Device string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			f.sealed, _ = base64.StdEncoding.DecodeString(in.Ciphertext)
			f.device = in.Device
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	old := ferryClientFor
	t.Cleanup(func() { ferryClientFor = old })
	ferryClientFor = func(*Server) (*http.Client, error) { return f.Client(), nil }
	return f
}

func ferryEscrowKeyPair(t *testing.T, kem hpke.KEM) (hpke.PrivateKey, string) {
	t.Helper()
	private, err := kem.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return private, "hpke:" + strconv.Itoa(int(kem.ID())) + ":" + base64.StdEncoding.EncodeToString(private.PublicKey().Bytes())
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

func TestFerrySignInSealsTheRecoveryKeyForTheOrganisationOnly(t *testing.T) {
	for _, kem := range []hpke.KEM{hpke.MLKEM768X25519(), hpke.DHKEM(ecdh.X25519())} {
		private, public := ferryEscrowKeyPair(t, kem)
		f := newFakeFerry(t, public, ferryTestExits)
		session, err := (&Server{}).ferrySignIn(context.Background(), f.URL+"/base", " 张三 ", "correct horse", "RECOVERY-KEY-1")
		if err != nil {
			t.Fatal(err)
		}
		if session.escrow != "stored" || session.escrowErr != nil || session.reply.SubURL != ferryTestSub || len(session.reply.Exits) != 1 || session.reply.Exits[0].Region != "AU" {
			t.Fatalf("%+v", session)
		}
		if strings.Contains(string(f.sealed), "RECOVERY-KEY-1") {
			t.Fatal("recovery key reached Ferry in the clear")
		}
		opened, err := hpke.Open(private, hpke.HKDFSHA256(), hpke.AES256GCM(), []byte(ferryEscrowInfo), f.sealed)
		if err != nil || string(opened) != "RECOVERY-KEY-1" {
			t.Fatalf("organisation key cannot open the escrow: %q %v", opened, err)
		}
		if _, err := hpke.Open(private, hpke.HKDFSHA256(), hpke.AES256GCM(), []byte("other use"), f.sealed); err == nil {
			t.Fatal("escrow is not bound to its purpose")
		}
	}
}

func TestFerrySignInWithoutEscrowKeyOrRecoveryKeyLeavesNothing(t *testing.T) {
	f := newFakeFerry(t, "", ferryTestExits)
	session, err := (&Server{}).ferrySignIn(context.Background(), f.URL+"/base", "张三", "correct horse", "RECOVERY-KEY-1")
	if err != nil || session.escrow != "none" || f.escrows != 0 {
		t.Fatal(session, err, f.escrows)
	}
	_, public := ferryEscrowKeyPair(t, hpke.MLKEM768X25519())
	f = newFakeFerry(t, public, ferryTestExits)
	session, err = (&Server{}).ferrySignIn(context.Background(), f.URL+"/base", "张三", "correct horse", "")
	if err != nil || session.escrow != "none" || f.escrows != 0 {
		t.Fatal(session, err, f.escrows)
	}
}

func TestFerryEscrowFailureKeepsTheSignIn(t *testing.T) {
	for _, key := range []string{"hpke:16:" + base64.StdEncoding.EncodeToString(make([]byte, 65)), "hpke:25722:not-base64", "pem:abc", "hpke:32:" + base64.StdEncoding.EncodeToString([]byte("short"))} {
		f := newFakeFerry(t, key, ferryTestExits)
		session, err := (&Server{}).ferrySignIn(context.Background(), f.URL+"/base", "张三", "correct horse", "RECOVERY-KEY-1")
		if err != nil || session.escrow != "failed" || session.escrowErr == nil || f.escrows != 0 {
			t.Fatal(key, session, err, f.escrows)
		}
	}
}

func TestFerryLoginAnswersInWords(t *testing.T) {
	f := newFakeFerry(t, "", ferryTestExits)
	_, err := (&Server{}).ferrySignIn(context.Background(), f.URL+"/base", "张三", "wrong", "")
	var fe *ferryError
	if !errors.As(err, &fe) || fe.status != http.StatusUnprocessableEntity || fe.message != "Ferry 用户名或密码错误" {
		t.Fatal(err)
	}
	if _, err := (&Server{}).ferrySignIn(context.Background(), f.URL+"/base", "", "x", ""); err == nil || f.logins != 1 {
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
	good := ferryLoginReply{Token: "t", SubURL: ferryTestSub, Exits: ferryTestExits, EscrowKey: "hpke:1:a"}
	if err := good.check(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*ferryLoginReply){
		"http link":       func(r *ferryLoginReply) { r.SubURL = "http://ferry.example.com/sub/x" },
		"no exits":        func(r *ferryLoginReply) { r.Exits = nil },
		"empty exit name": func(r *ferryLoginReply) { r.Exits = []ferryExit{{Name: " "}} },
		"bad region":      func(r *ferryLoginReply) { r.Exits = []ferryExit{{Name: "x", Region: "au"}} },
		"escrow no token": func(r *ferryLoginReply) { r.Token = "" },
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
	_, public := ferryEscrowKeyPair(t, hpke.MLKEM768X25519())
	f := newFakeFerry(t, public, ferryTestExits)
	oldImport, oldCore := ferryImport, proxyCorePath
	t.Cleanup(func() { ferryImport, proxyCorePath = oldImport, oldCore })
	proxyCorePath = func() (string, error) { return "mihomo", nil }
	var imported proxyInput
	ferryImport = func(_ *Server, input proxyInput, _ *uint64) error { imported = input; return nil }
	body, _ := json.Marshal(map[string]string{"server": f.URL + "/base", "username": "张三", "password": "correct horse", "recovery_key": "RECOVERY-KEY-1"})
	w := request(s, "POST", "/api/ferry/login", s.origin, string(body), true)
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "private-token") || strings.Contains(w.Body.String(), "RECOVERY-KEY") {
		t.Fatal("subscription link or recovery key returned to the page:", w.Body.String())
	}
	var out struct {
		Job    Job         `json:"job"`
		Source string      `json:"source"`
		Exits  []ferryExit `json:"exits"`
		Escrow string      `json:"escrow"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Job.ID == "" || out.Source != "Ferry" || out.Escrow != "stored" || len(out.Exits) != 1 {
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
