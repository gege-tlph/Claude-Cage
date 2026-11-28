package workbench

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestTerminalSubscribeFromResumesWhileOutputIsKept(t *testing.T) {
	// Ten bytes written, the last five kept.
	session := &terminalSession{history: []byte("world"), written: 10, clients: map[*terminalViewer]bool{}}
	for _, tc := range []struct {
		after  uint64
		data   string
		offset uint64
		reset  bool
	}{
		{7, "rld", 7, false},
		{10, "", 10, false},
		{5, "world", 5, false},
		{2, "world", 5, true},  // no longer kept
		{12, "world", 5, true}, // not written yet: another session's offset
	} {
		after := tc.after
		sub := session.subscribeFrom(&after)
		if string(sub.History) != tc.data || sub.HistoryOffset != tc.offset || sub.HistoryReset != tc.reset {
			t.Errorf("after %d: %q at %d reset=%t", tc.after, sub.History, sub.HistoryOffset, sub.HistoryReset)
		}
		sub.Close()
	}
	if sub := session.subscribe(); string(sub.History) != "world" || sub.HistoryOffset != 5 || !sub.HistoryReset {
		t.Errorf("plain subscribe: %q at %d reset=%t", sub.History, sub.HistoryOffset, sub.HistoryReset)
	}
}

func TestTerminalLiveOutputContinuesFromSubscriptionOffset(t *testing.T) {
	session, file := newControlledSession()
	t.Cleanup(func() { close(file.waitRelease); session.close() })
	first := session.subscribe()
	if _, err := io.WriteString(file.writer, "hello "); err != nil {
		t.Fatal(err)
	}
	readTerminalUntil(t, first, "hello ")
	first.Close()
	after := uint64(len("hello "))
	resumed := session.subscribeFrom(&after)
	defer resumed.Close()
	if len(resumed.History) != 0 || resumed.HistoryReset {
		t.Fatalf("resume repeated output: %q reset=%t", resumed.History, resumed.HistoryReset)
	}
	if _, err := io.WriteString(file.writer, "again"); err != nil {
		t.Fatal(err)
	}
	if text := readTerminalUntil(t, resumed, "again"); text != "again" {
		t.Fatalf("live output after resume: %q", text)
	}
}

type terminalFrame struct {
	text   bool
	Type   string `json:"type"`
	Offset uint64 `json:"offset"`
	Size   int    `json:"size"`
	Reset  bool   `json:"reset"`
	data   []byte
}

func readTerminalFrame(t *testing.T, ws *websocket.Conn) terminalFrame {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(10 * time.Second))
	kind, data, err := ws.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if kind == websocket.BinaryMessage {
		return terminalFrame{data: data}
	}
	frame := terminalFrame{text: true}
	if err := json.Unmarshal(data, &frame); err != nil {
		t.Fatal(err)
	}
	return frame
}

func TestTerminalStreamResumesAfterOffsetAndAnswersPing(t *testing.T) {
	s := testServer(t)
	session, file := newControlledSession()
	session.id = "resume"
	s.terminals.sessions = map[string]*terminalSession{session.id: session}
	t.Cleanup(func() { close(file.waitRelease); session.close() })
	server := httptest.NewServer(s.handler())
	defer server.Close()
	s.origin = server.URL
	probe := session.subscribe()
	if _, err := io.WriteString(file.writer, "first-part|"); err != nil {
		t.Fatal(err)
	}
	readTerminalUntil(t, probe, "first-part|")
	probe.Close()
	dial := func(query string) (*websocket.Conn, *http.Response, error) {
		headers := http.Header{"Origin": {server.URL}, "Cookie": {s.cookieName() + "=" + s.session}}
		return websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/api/terminals/resume/stream"+query, headers)
	}
	if _, response, err := dial("?after=x"); err == nil || response == nil || response.StatusCode != http.StatusBadRequest {
		t.Fatalf("malformed offset accepted: %v", err)
	}

	// From the start: the kept output, marked with its offset.
	ws, _, err := dial("?after=0")
	if err != nil {
		t.Fatal(err)
	}
	marker := readTerminalFrame(t, ws)
	body := readTerminalFrame(t, ws)
	if marker.Type != "history" || marker.Offset != 0 || marker.Reset || marker.Size != len("first-part|") || string(body.data) != "first-part|" {
		t.Fatalf("history: %+v %q", marker, body.data)
	}
	if err := ws.WriteJSON(map[string]string{"type": "ping"}); err != nil {
		t.Fatal(err)
	}
	if pong := readTerminalFrame(t, ws); pong.Type != "pong" {
		t.Fatalf("ping answered with %+v", pong)
	}
	// The heartbeat is answered while the host terminal has the input (B15).
	session.mu.Lock()
	session.hostInput = true
	session.mu.Unlock()
	if err := ws.WriteJSON(map[string]string{"type": "ping"}); err != nil {
		t.Fatal(err)
	}
	if pong := readTerminalFrame(t, ws); pong.Type != "pong" {
		t.Fatalf("ping during host input answered with %+v", pong)
	}
	session.mu.Lock()
	session.hostInput = false
	session.mu.Unlock()
	_ = ws.Close()

	// Output written while disconnected arrives alone after a resume.
	if _, err := io.WriteString(file.writer, "missed"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	ws, _, err = dial("?after=11")
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	marker = readTerminalFrame(t, ws)
	body = readTerminalFrame(t, ws)
	if marker.Type != "history" || marker.Offset != 11 || marker.Reset || string(body.data) != "missed" {
		t.Fatalf("resume: %+v %q", marker, body.data)
	}
}

func TestTerminalStreamSendsHeartbeatsAndExit(t *testing.T) {
	interval := terminalHeartbeatInterval
	terminalHeartbeatInterval = 50 * time.Millisecond
	t.Cleanup(func() { terminalHeartbeatInterval = interval })
	s := testServer(t)
	session, file := newControlledSession()
	session.id = "beat"
	s.terminals.sessions = map[string]*terminalSession{session.id: session}
	server := httptest.NewServer(s.handler())
	defer server.Close()
	s.origin = server.URL
	headers := http.Header{"Origin": {server.URL}, "Cookie": {s.cookieName() + "=" + s.session}}
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/api/terminals/beat/stream?after=0", headers)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	pinged := make(chan struct{}, 1)
	ws.SetPingHandler(func(string) error {
		select {
		case pinged <- struct{}{}:
		default:
		}
		return nil
	})
	// A resuming viewer always learns its offset, even with nothing to replay.
	if frame := readTerminalFrame(t, ws); frame.Type != "history" || frame.Size != 0 || frame.Offset != 0 {
		t.Fatalf("expected an empty history marker, got %+v", frame)
	}
	if frame := readTerminalFrame(t, ws); frame.Type != "heartbeat" {
		t.Fatalf("expected a heartbeat, got %+v", frame)
	}
	select {
	case <-pinged:
	default:
		t.Fatal("heartbeat came without a WebSocket ping")
	}
	_ = file.writer.Close()
	close(file.waitRelease)
	for {
		frame := readTerminalFrame(t, ws)
		if frame.Type == "exit" {
			break
		}
		if frame.Type != "heartbeat" {
			t.Fatalf("unexpected frame before exit: %+v", frame)
		}
	}
	session.close()
}
