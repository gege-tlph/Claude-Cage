package workbench

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
)

// A terminal stream can cross proxies and tunnels (remote workbenches) that
// drop a silent WebSocket, or lose it without any close after the host slept.
// The server speaks at least every heartbeat interval and releases a viewer
// that stops answering pings; browsers answer them even in hidden pages.
var (
	terminalHeartbeatInterval = 20 * time.Second
	terminalWriteTimeout      = 30 * time.Second
	terminalReadTimeout       = 90 * time.Second
)

// terminalFrameLimit bounds one binary frame, so a slow path still delivers a
// frame well within the write timeout and the viewer sees steady progress.
const terminalFrameLimit = 64 << 10

// terminalInputMessageLimit bounds one message from a viewer. A paste is one
// JSON-escaped input message; one over the limit closed the stream and the
// paste was lost.
const terminalInputMessageLimit = 16 << 20

var terminalOffsetPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,19})$`)

// terminalResumeOffset reads the optional after=<byte offset> of a viewer that
// tracks offsets. A malformed or repeated value is refused.
func terminalResumeOffset(r *http.Request) (*uint64, bool) {
	values, present := r.URL.Query()["after"]
	if !present {
		return nil, true
	}
	if len(values) != 1 || !terminalOffsetPattern.MatchString(values[0]) {
		return nil, false
	}
	offset, err := strconv.ParseUint(values[0], 10, 64)
	if err != nil {
		return nil, false
	}
	return &offset, true
}

func (s *Server) listTerminals(w http.ResponseWriter, r *http.Request) {
	jsonReply(w, s.terminalSummaries())
}

func (s *Server) newTerminal(w http.ResponseWriter, r *http.Request) {
	var input terminalRequest
	if !readJSON(w, r, &input) {
		return
	}
	session, err := s.createTerminal(input)
	switch {
	case errors.Is(err, errTerminalsClosed):
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
	case errors.Is(err, errTerminalBusy), errors.Is(err, errTerminalNotReady), errors.Is(err, errTerminalLimit):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, errTerminalStart):
		http.Error(w, err.Error(), http.StatusInternalServerError)
	case err != nil:
		requestError(w, err)
	default:
		jsonReply(w, session.summary())
	}
}

func (s *Server) closeTerminal(w http.ResponseWriter, r *http.Request) {
	if terminal := s.terminals.get(r.PathValue("id")); terminal != nil && !terminal.ended() {
		if id, _ := terminal.summary()["cli_session_id"].(string); id != "" {
			if err := s.setSessionArchived(id, true, nil); errors.Is(err, os.ErrNotExist) {
				s.terminals.remove(terminal.id)
			} else if err != nil {
				requestError(w, err)
				return
			}
			jsonReply(w, map[string]bool{"ok": true})
			return
		}
	}
	s.terminals.remove(r.PathValue("id"))
	jsonReply(w, map[string]bool{"ok": true})
}

// terminalStream attaches a viewer. Without after it keeps the original
// framing (agent sockets use it): an optional history marker and one binary
// frame, then live output. With after, every history marker carries the
// offset of the bytes that follow, their size and whether the viewer must
// clear its screen first, so the viewer can resume after a reconnect from the
// last byte it received; heartbeats and an exit message are added.
func (s *Server) terminalStream(w http.ResponseWriter, r *http.Request) {
	session := s.terminals.get(r.PathValue("id"))
	if session == nil {
		http.NotFound(w, r)
		return
	}
	after, valid := terminalResumeOffset(r)
	if !valid {
		http.Error(w, "终端输出位置无效", http.StatusBadRequest)
		return
	}
	ws, err := s.upgrade(w, r)
	if err != nil {
		return
	}
	defer ws.Close()
	ws.SetReadLimit(terminalInputMessageLimit)
	_ = ws.SetReadDeadline(time.Now().Add(terminalReadTimeout))
	ws.SetPongHandler(func(string) error { return ws.SetReadDeadline(time.Now().Add(terminalReadTimeout)) })
	subscription := session.subscribeFrom(after)
	pongs, stop, done := make(chan struct{}, 1), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		defer ws.Close()
		writeTerminalStream(ws, subscription, after != nil, pongs, stop)
	}()
	defer func() {
		close(stop)
		subscription.Close()
		_ = ws.Close()
		<-done
	}()
	for {
		_, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		_ = ws.SetReadDeadline(time.Now().Add(terminalReadTimeout))
		var input struct {
			Type string `json:"type"`
			Data string `json:"data"`
			Cols uint16 `json:"cols"`
			Rows uint16 `json:"rows"`
		}
		if json.Unmarshal(data, &input) != nil {
			continue
		}
		// The heartbeat keeps counting while the host terminal has the input;
		// only the viewer's keys and size are set aside then.
		if input.Type == "ping" {
			select {
			case pongs <- struct{}{}:
			default:
			}
			continue
		}
		session.mu.Lock()
		hostInput := session.hostInput
		session.mu.Unlock()
		if hostInput {
			continue
		}
		switch input.Type {
		case "input":
			if err := session.enqueueInputWait(r.Context(), input.Data, terminalInputWait); err != nil {
				return
			}
		case "resize":
			if input.Cols > 1 && input.Cols < 600 && input.Rows > 1 && input.Rows < 400 {
				if err := session.file.Resize(input.Cols, input.Rows); err != nil {
					return
				}
			}
		}
	}
}

// writeTerminalStream is the only writer of a terminal stream. Live output is
// coalesced into frames of at most terminalFrameLimit bytes. The PTY never
// waits for this viewer, which falls back to the kept history if its queue
// overflows.
func writeTerminalStream(ws *websocket.Conn, subscription *terminalSubscription, resume bool, pongs, stop <-chan struct{}) {
	writeJSON := func(value any) bool {
		_ = ws.SetWriteDeadline(time.Now().Add(terminalWriteTimeout))
		return ws.WriteJSON(value) == nil
	}
	writeBinary := func(data []byte) bool {
		for len(data) > 0 {
			n := min(len(data), terminalFrameLimit)
			_ = ws.SetWriteDeadline(time.Now().Add(terminalWriteTimeout))
			if ws.WriteMessage(websocket.BinaryMessage, data[:n]) != nil {
				return false
			}
			data = data[n:]
		}
		return true
	}
	// Replayed output must render without repeating clipboard side effects.
	replay := func(data []byte, offset uint64, reset bool) bool {
		if !resume {
			if len(data) == 0 {
				return true
			}
			if !writeJSON(map[string]string{"type": "history"}) {
				return false
			}
			_ = ws.SetWriteDeadline(time.Now().Add(terminalWriteTimeout))
			return ws.WriteMessage(websocket.BinaryMessage, data) == nil
		}
		return writeJSON(map[string]any{"type": "history", "offset": offset, "reset": reset, "size": len(data)}) && writeBinary(data)
	}
	if subscription.CLIStartup != nil && !writeJSON(map[string]any{"type": "cli-startup", "pending": subscription.CLIStarting}) {
		return
	}
	if !replay(subscription.History, subscription.HistoryOffset, subscription.HistoryReset) {
		return
	}
	subscription.History = nil
	startup := subscription.CLIStartup
	if !subscription.CLIStarting {
		startup = nil
	}
	heartbeat := time.NewTicker(terminalHeartbeatInterval)
	defer heartbeat.Stop()
	started := func() bool {
		startup = nil
		return writeJSON(map[string]any{"type": "cli-startup", "pending": false})
	}
	pong := func() bool { return writeJSON(map[string]string{"type": "pong"}) }
	beat := func() bool {
		if ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(terminalWriteTimeout)) != nil {
			return false
		}
		// A page cannot see ping frames; this lets it notice silence.
		return !resume || writeJSON(map[string]string{"type": "heartbeat"})
	}
	for {
		// Control replies are never starved by a continuous stream of output.
		select {
		case <-stop:
			return
		case <-startup:
			if !started() {
				return
			}
		case <-pongs:
			if !pong() {
				return
			}
		case <-heartbeat.C:
			if !beat() {
				return
			}
		default:
		}
		batch := subscription.next(terminalFrameLimit)
		switch {
		case batch.Resync && !resume && batch.Reset:
			// A viewer without offsets cannot clear its screen. Ending the
			// stream gives it the kept history on its next connection.
			return
		case batch.Resync && resume:
			if !replay(batch.Data, batch.Offset, batch.Reset) {
				return
			}
			continue
		case len(batch.Data) > 0:
			if !writeBinary(batch.Data) {
				return
			}
			continue
		case batch.Done:
			if batch.Ended && resume && writeJSON(map[string]string{"type": "exit"}) {
				_ = ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
			}
			return
		}
		select {
		case <-stop:
			return
		case <-subscription.Wake():
		case <-startup:
			if !started() {
				return
			}
		case <-pongs:
			if !pong() {
				return
			}
		case <-heartbeat.C:
			if !beat() {
				return
			}
		}
	}
}
