package workbench

import (
	"bytes"
	"context"
	"crypto/hpke"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sylvinus/agent-vm/internal/diagnostics"
	"github.com/sylvinus/agent-vm/internal/managedproxy"
)

// Ferry is a sing-box panel an organisation runs for its exits. In the
// "从 Ferry 配置" first run the employee's own subscription is imported and
// applied first, then Ferry is reached through that route only, so a panel
// the host network cannot reach still works:
//
//	POST <server>/cage/v1/login  {"username","password"}
//	  -> {"user","token","sub_url","exits":[{"name","region"}],"escrow_key"}
//	POST <server>/cage/v1/escrow (Bearer token) {"ciphertext","username","device"}
//
// The account's recovery key is sealed with HPKE to the organisation's escrow
// key (escrow_key "hpke:<KEM id>:<base64 public key>"); Ferry keeps only the
// ciphertext and cannot open it.
const (
	ferryEscrowInfo = "cage-lite recovery escrow v1"
	ferryMaxReply   = 1 << 20
	ferryMaxExits   = 300
)

// The escrow KEMs Cage seals to: DHKEM(X25519, HKDF-SHA256) and the hybrid
// ML-KEM-768 + X25519.
var ferryEscrowKEMs = map[uint16]bool{0x0020: true, 0x647a: true}

var ferryRegionCode = regexp.MustCompile(`^[A-Z]{2}$`)

// ferryClientFor gives the client Ferry requests use: the route applied by
// the first-run steps, never the host network. Tests replace it.
var ferryClientFor = func(s *Server) (*http.Client, error) {
	client, err := s.subscriptionClient(false)
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
	for _, src := range p.Sources {
		if src.Name != input.Name || src.URL == "" {
			continue
		}
		if src.URL != input.URL {
			if err := s.changeProxy("edit", proxyInput{ID: src.ID, Name: src.Name, URL: input.URL, Interval: src.Interval}, pauseVersion); err != nil {
				return err
			}
		}
		return s.changeProxy("refresh", proxyInput{ID: src.ID}, pauseVersion)
	}
	return s.changeProxy("import", input, pauseVersion)
}

// ferryState is what the profile keeps of Ferry (ferry.json): the address and
// user last signed in with, and which recovery key Ferry holds, as the
// fingerprint of the account's recovery record when it was handed over.
type ferryState struct {
	Server   string `json:"server,omitempty"`
	User     string `json:"user,omitempty"`
	Recovery string `json:"recovery,omitempty"`
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

// recoveryFingerprint names the account's current recovery key without
// revealing it; it changes whenever a new key is made.
func (a *localAccount) recoveryFingerprint() string {
	if a == nil {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.record == nil || a.record.Recovery == nil {
		return ""
	}
	sum := sha256.Sum256(append(append([]byte(nil), a.record.Recovery.Nonce...), a.record.Recovery.SealedKey...))
	return hex.EncodeToString(sum[:16])
}

// ferryStatus is GET /api/ferry: the address and user to prefill, and whether
// Ferry holds the account's current recovery key.
func (s *Server) ferryStatus(w http.ResponseWriter, r *http.Request) {
	state := loadFerryState(s.profile)
	current := s.account.recoveryFingerprint()
	jsonReply(w, map[string]any{"server": state.Server, "user": state.User, "escrowed": current != "" && state.Recovery == current})
}

type ferryExit struct {
	Name   string `json:"name"`
	Region string `json:"region"`
}

type ferryLoginReply struct {
	User      string      `json:"user"`
	Token     string      `json:"token"`
	SubURL    string      `json:"sub_url"`
	Exits     []ferryExit `json:"exits"`
	EscrowKey string      `json:"escrow_key"`
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
		Server      string `json:"server"`
		Username    string `json:"username"`
		Password    string `json:"password"`
		RecoveryKey string `json:"recovery_key"`
	}
	if !readJSON(w, r, &in, 64<<10) {
		return
	}
	session, err := s.ferrySignIn(r.Context(), in.Server, in.Username, in.Password, in.RecoveryKey)
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
	input := proxyInput{Name: ferrySourceName, URL: session.reply.SubURL, Interval: ferryRefreshHours}
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
	out := map[string]any{"job": job, "user": session.reply.User, "source": ferrySourceName, "exits": session.reply.Exits, "escrow": session.escrow}
	if session.escrowErr != nil {
		out["escrow_error"] = session.escrowErr.Error()
	}
	jsonReply(w, out)
}

// The source the exits are imported as, and how often it is refreshed
// (Ferry answers with Profile-Update-Interval: 12).
const (
	ferrySourceName   = "Ferry"
	ferryRefreshHours = 12
)

// ferrySession is a sign-in and what became of the recovery key: "stored",
// "failed" (escrowErr says why) or "none" (no key given, or the organisation
// has no escrow key). A failed escrow does not fail the sign-in.
type ferrySession struct {
	reply     ferryLoginReply
	escrow    string
	escrowErr error
}

// ferrySignIn signs in and, when the organisation has an escrow key, leaves
// the account's recovery key sealed with Ferry: the key given (a new
// account's), or, when Ferry does not hold the current one (an earlier run
// stopped before handing it over, or a key was made since), a new key made
// with the password. All of it is silent: the employee never sees the key,
// and a Cage password other than the Ferry one leaves the key as it is.
func (s *Server) ferrySignIn(ctx context.Context, server, username, password, recoveryKey string) (ferrySession, error) {
	login, err := ferryEndpoint(server, "cage/v1/login")
	if err != nil {
		return ferrySession{}, err
	}
	escrowURL, _ := ferryEndpoint(server, "cage/v1/escrow")
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return ferrySession{}, &ferryError{http.StatusBadRequest, "请填写 Ferry 用户名和密码"}
	}
	client, err := ferryClientFor(s)
	if err != nil {
		return ferrySession{}, &ferryError{http.StatusConflict, err.Error()}
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	reply, err := ferryPostLogin(ctx, client, login, username, password)
	if err != nil {
		return ferrySession{}, err
	}
	session := ferrySession{reply: reply, escrow: "none"}
	state := loadFerryState(s.profile)
	state.Server, state.User = strings.TrimSpace(server), username
	if reply.EscrowKey != "" {
		current := s.account.recoveryFingerprint()
		if recoveryKey == "" && s.account != nil && current != state.Recovery {
			if key, err := s.account.generateRecovery(password); err == nil {
				recoveryKey, current = key, s.account.recoveryFingerprint()
			}
		}
		if recoveryKey != "" {
			session.escrow = "stored"
			if session.escrowErr = ferryEscrow(ctx, client, escrowURL, reply.Token, reply.EscrowKey, username, recoveryKey); session.escrowErr != nil {
				session.escrow = "failed"
				diagnostics.Log("warn", "ferry", "恢复密钥未能交给 Ferry："+session.escrowErr.Error())
			} else {
				state.Recovery = current
			}
		}
	}
	if err := saveFerryState(s.profile, state); err != nil {
		diagnostics.Log("warn", "ferry", "Ferry 状态未能保存："+err.Error())
	}
	return session, nil
}

func ferryPostLogin(ctx context.Context, client *http.Client, endpoint, username, password string) (ferryLoginReply, error) {
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	status, raw, err := ferryPost(ctx, client, endpoint, "", body)
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
	if r.EscrowKey != "" && (r.Token == "" || len(r.Token) > 4096) {
		return errors.New("Ferry 返回的会话无效")
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

// ferryEscrowRetry is the pause before each further escrow attempt after the
// connection failed. Tests shorten it.
var ferryEscrowRetry = []time.Duration{time.Second, 2 * time.Second}

func ferryEscrow(ctx context.Context, client *http.Client, endpoint, token, escrowKey, username, recoveryKey string) error {
	sealed, err := ferrySealRecovery(escrowKey, recoveryKey)
	if err != nil {
		return err
	}
	device, _ := os.Hostname()
	body, _ := json.Marshal(map[string]string{"ciphertext": base64.StdEncoding.EncodeToString(sealed), "username": username, "device": device})
	for attempt := 0; ; attempt++ {
		status, raw, err := ferryPost(ctx, client, endpoint, token, body)
		if err == nil {
			if status != http.StatusOK {
				return ferryStatusError(status, raw)
			}
			return nil
		}
		// Only a lost connection is tried again; an answer from Ferry stands.
		if attempt >= len(ferryEscrowRetry) || ctx.Err() != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(ferryEscrowRetry[attempt]):
		}
	}
}

// ferrySealRecovery seals the recovery key to "hpke:<KEM id>:<base64 key>"
// with HKDF-SHA256 and AES-256-GCM; the result is enc || ciphertext.
func ferrySealRecovery(escrowKey, recoveryKey string) ([]byte, error) {
	invalid := errors.New("Ferry 的保管公钥无效")
	rest, ok := strings.CutPrefix(escrowKey, "hpke:")
	if !ok {
		return nil, invalid
	}
	idText, keyText, ok := strings.Cut(rest, ":")
	id, err := strconv.ParseUint(idText, 10, 16)
	if !ok || err != nil || !ferryEscrowKEMs[uint16(id)] {
		return nil, invalid
	}
	kem, err := hpke.NewKEM(uint16(id))
	if err != nil {
		return nil, invalid
	}
	raw, err := base64.StdEncoding.DecodeString(keyText)
	if err != nil {
		return nil, invalid
	}
	public, err := kem.NewPublicKey(raw)
	if err != nil {
		return nil, invalid
	}
	return hpke.Seal(public, hpke.HKDFSHA256(), hpke.AES256GCM(), []byte(ferryEscrowInfo), []byte(recoveryKey))
}

func ferryPost(ctx context.Context, client *http.Client, endpoint, token string, body []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
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
