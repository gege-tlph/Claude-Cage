package workbench

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

var errTerminalInputFull = errors.New("terminal input queue is full")

type terminalPTY interface {
	io.ReadWriteCloser
	Resize(cols, rows uint16) error
	Wait() error
	Kill() error
}

type terminalSession struct {
	backgroundLogin       bool
	mu                    sync.Mutex
	id, title             string
	creationOrder         int
	sharedID              string
	connectionID          string
	conversationEnded     bool
	paused                bool
	workspace             string
	kind, claudeAccountID string
	loginGuestID          string
	physicalProfile       string
	loginRequested        time.Time
	loginPrefix           int
	loginLinkMu           sync.Mutex
	loginLinkCleanup      sync.Once
	claudeAccountSource   string
	cmd                   *exec.Cmd
	file                  terminalPTY
	history               []byte
	written               uint64 // output bytes read so far; history holds the last of them
	loginNotice           string
	dismissed             bool
	exitOK                bool
	exitCode              int
	hostInput             bool
	closed                bool
	clients               map[*terminalViewer]bool
	done                  chan struct{}
	inputs                chan string
	stopping              chan struct{}
	closeOnce             sync.Once
	cliStartupDone        chan struct{}
}

func (t *terminalSession) accountID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.claudeAccountID
}

func (t *terminalSession) ended() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.closed {
		return false
	}
	if t.done != nil {
		select {
		case <-t.done:
		default:
			return false
		}
	}
	return true
}

func (t *terminalSession) replaceable() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closed && !t.dismissed && t.kind == "claude-login"
}

func (t *terminalSession) ownsConversation() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return !t.closed && !t.conversationEnded
}

func newTerminalSession(file terminalPTY) *terminalSession {
	return &terminalSession{file: file, clients: map[*terminalViewer]bool{}, done: make(chan struct{}), inputs: make(chan string, 8), stopping: make(chan struct{})}
}

func (t *terminalSession) summary() map[string]any {
	t.mu.Lock()
	defer t.mu.Unlock()
	closed := t.closed
	if t.done != nil {
		select {
		case <-t.done:
			closed = true
		default:
			closed = false
		}
	}
	return map[string]any{"id": t.id, "title": t.title, "closed": closed, "workspace": t.workspace, "kind": t.kind,
		"claude_account_id": t.claudeAccountID, "claude_account_source": t.claudeAccountSource, "cli_session_id": t.sharedID, "paused": t.paused,
		"background_login": t.backgroundLogin, "login_notice": t.loginNotice, "cli_starting": t.cliStartupPendingLocked(), "host_input": t.hostInput}
}

func (t *terminalSession) cliStartupPendingLocked() bool {
	if t.cliStartupDone == nil || t.closed {
		return false
	}
	select {
	case <-t.cliStartupDone:
		return false
	default:
		return true
	}
}

func (t *terminalSession) finishCLIStartup() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cliStartupPendingLocked() {
		close(t.cliStartupDone)
	}
}

func (t *terminalSession) stopPTY() {
	t.closeOnce.Do(func() {
		t.mu.Lock()
		t.closed = true
		t.mu.Unlock()
		if t.stopping != nil {
			close(t.stopping)
		}
		if t.file != nil {
			// Close may wait inside ConPTY. Never hold the session mutex while
			// the reader needs it to drain the final output and finish.
			_ = t.file.Kill()
			_ = t.file.Close()
		}
	})
}

func (t *terminalSession) close() {
	t.mu.Lock()
	t.dismissed = true
	t.mu.Unlock()
	t.stopPTY()
	if t.done != nil {
		<-t.done
	}
}

// Input belongs to the session once admitted. A viewer never waits inside a
// PTY write, so disconnecting or locking the account can drain its handler
// while a guest process is not reading input. A full queue rejects the viewer
// immediately, so the handler never waits to read a WebSocket close either.
// Keep only the length of a matching command prefix, never the input line.
// This also identifies the origin on platforms whose native login browser
// cannot invoke the guest's BROWSER launcher.
func (t *terminalSession) noteLoginInputLocked(data string) {
	const command = "/login"
	if t.kind != "claude" {
		return
	}
	for _, c := range []byte(data) {
		switch c {
		case '\r', '\n':
			if t.loginPrefix == len(command) {
				t.loginRequested = time.Now()
			}
			t.loginPrefix = 0
		case 3, 21:
			t.loginPrefix = 0
		case 8, 127:
			if t.loginPrefix > 0 && t.loginPrefix <= len(command) {
				t.loginPrefix--
			}
		default:
			if t.loginPrefix < len(command) && c == command[t.loginPrefix] {
				t.loginPrefix++
			} else {
				t.loginPrefix = len(command) + 1
			}
		}
	}
}

func (t *terminalSession) enqueueInput(ctx context.Context, input string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return os.ErrClosed
	}
	// A stale viewer can still send keystrokes after a stop. Keep its stream
	// open, but never queue that line for the job or its eventual shell.
	if t.paused {
		return nil
	}
	select {
	case t.inputs <- input:
		t.noteLoginInputLocked(input)
		return nil
	default:
		return errTerminalInputFull
	}
}

// terminalInputWait bounds how long a burst of client input, such as a
// terminal answering many queries at once, may wait for the PTY writer.
const terminalInputWait = 2 * time.Second

// enqueueInputWait retries a full queue until the writer drains it, keeping
// input order; it fails only when the PTY stays blocked or the request ends.
func (t *terminalSession) enqueueInputWait(ctx context.Context, input string, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	for {
		err := t.enqueueInput(ctx, input)
		if !errors.Is(err, errTerminalInputFull) || !time.Now().Before(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (t *terminalSession) writeInput() {
	for {
		select {
		case <-t.stopping:
			return
		case input := <-t.inputs:
			if _, err := io.WriteString(t.file, input); err != nil {
				t.stopPTY()
				return
			}
		}
	}
}

// terminalHistoryLimit is the output a session keeps for viewers that
// connect or resume later.
const terminalHistoryLimit = 512 << 10

// terminalViewerBudget bounds the live output queued for one viewer that is
// not keeping up, such as a slow WebView2 page during a long build log. Past
// it the queue is dropped and the viewer is caught up from the kept history,
// instead of being disconnected as it was with a fixed 64-chunk channel.
var terminalViewerBudget = 4 << 20

// terminalViewer is the live output queued for one attached stream. The PTY
// reader only appends under the session lock and never waits for a viewer.
type terminalViewer struct {
	pending  []byte        // queued output, contiguous from offset
	offset   uint64        // absolute output offset of pending[0]
	lagged   bool          // the queue overflowed; catch up from history
	closed   bool          // the session ended; nothing more is queued
	detached bool          // the viewer left
	wake     chan struct{} // signalled after every change
}

func (v *terminalViewer) signal() {
	select {
	case v.wake <- struct{}{}:
	default:
	}
}

// push queues live output. Caller holds the session lock.
func (v *terminalViewer) push(chunk []byte) {
	if v.lagged {
		return
	}
	if len(v.pending)+len(chunk) > terminalViewerBudget {
		v.lagged, v.pending = true, nil
	} else {
		v.pending = append(v.pending, chunk...)
	}
	v.signal()
}

type terminalSubscription struct {
	// History is sent before live output. HistoryOffset is the absolute
	// offset of its first byte; HistoryReset reports that it does not continue
	// from the offset the viewer asked to resume after.
	History       []byte
	HistoryOffset uint64
	HistoryReset  bool
	CLIStartup    <-chan struct{}
	CLIStarting   bool
	session       *terminalSession
	viewer        *terminalViewer
}

// terminalBatch is the next output for one viewer.
type terminalBatch struct {
	Data   []byte
	Offset uint64 // absolute offset of Data[0]
	Resync bool   // the viewer fell behind; Data restarts it from history
	Reset  bool   // Data does not continue from the output sent before
	Done   bool   // nothing more will arrive
	Ended  bool   // the session ended
}

// subscribe attaches a viewer that receives all kept history first.
func (t *terminalSession) subscribe() *terminalSubscription { return t.subscribeFrom(nil) }

// subscribeFrom captures history and registers live output under the same
// lock. Every byte belongs to exactly one side of this history/live boundary.
// With after, history starts at that absolute offset while it is still kept;
// otherwise it is everything kept, marked as a reset.
func (t *terminalSession) subscribeFrom(after *uint64) *terminalSubscription {
	t.mu.Lock()
	defer t.mu.Unlock()
	viewer := &terminalViewer{offset: t.written, wake: make(chan struct{}, 1)}
	if t.closed {
		viewer.closed = true
	} else {
		t.clients[viewer] = true
	}
	subscription := &terminalSubscription{CLIStartup: t.cliStartupDone, CLIStarting: t.cliStartupPendingLocked(), session: t, viewer: viewer}
	switch {
	case t.kind == "claude-login":
		subscription.History, subscription.HistoryReset = []byte("请在浏览器的临时登录页完成授权。若页面提供验证码，可返回此终端输入。\r\n"), true
	case after != nil:
		subscription.History, subscription.HistoryOffset, subscription.HistoryReset = t.historyAfterLocked(*after)
	default:
		subscription.History, subscription.HistoryOffset, subscription.HistoryReset = append([]byte(nil), t.history...), t.oldestLocked(), true
	}
	return subscription
}

// oldestLocked is the absolute offset of the first kept byte.
func (t *terminalSession) oldestLocked() uint64 {
	if t.written < uint64(len(t.history)) {
		return 0
	}
	return t.written - uint64(len(t.history))
}

// historyAfterLocked is the kept output after an absolute offset, or all kept
// output marked as a reset when that offset is no longer (or not yet) kept.
func (t *terminalSession) historyAfterLocked(after uint64) (data []byte, offset uint64, reset bool) {
	oldest := t.oldestLocked()
	if after < oldest || after > t.written {
		after, reset = oldest, true
	}
	return append([]byte(nil), t.history[after-oldest:]...), after, reset
}

// next takes the viewer's next output, at most limit bytes, without waiting.
// A viewer whose queue overflowed restarts from the kept history at the first
// byte it has not received, or from the oldest kept byte when that is gone.
func (s *terminalSubscription) next(limit int) terminalBatch {
	t, viewer := s.session, s.viewer
	t.mu.Lock()
	defer t.mu.Unlock()
	if viewer.lagged {
		if t.kind == "claude-login" {
			// Login output is never kept, so it cannot be sent again.
			return terminalBatch{Done: true}
		}
		data, offset, reset := t.historyAfterLocked(viewer.offset)
		viewer.lagged, viewer.pending, viewer.offset = false, nil, t.written
		return terminalBatch{Data: data, Offset: offset, Resync: true, Reset: reset}
	}
	if len(viewer.pending) > 0 {
		n := min(len(viewer.pending), limit)
		batch := terminalBatch{Data: viewer.pending[:n:n], Offset: viewer.offset}
		viewer.offset += uint64(n)
		if viewer.pending = viewer.pending[n:]; len(viewer.pending) == 0 {
			viewer.pending = nil
		}
		return batch
	}
	return terminalBatch{Done: viewer.closed || viewer.detached, Ended: viewer.closed}
}

// Wake is signalled whenever next may return something new.
func (s *terminalSubscription) Wake() <-chan struct{} { return s.viewer.wake }

// Close detaches the viewer; the PTY and its process continue running.
func (s *terminalSubscription) Close() {
	s.session.mu.Lock()
	defer s.session.mu.Unlock()
	delete(s.session.clients, s.viewer)
	s.viewer.detached = true
	s.viewer.signal()
}

func (t *terminalSession) run() {
	writerDone := make(chan struct{})
	go func() { defer close(writerDone); t.writeInput() }()
	t.read()
	// An SSH process can remain alive after its output side closes. Once no
	// output can be read, terminate and reap it before completing the session.
	t.stopPTY()
	waitErr := t.file.Wait()
	<-writerDone
	// Admission is sealed under mu before stopping the writer. Release queued
	// input even when the closed terminal remains visible with its history.
	for len(t.inputs) > 0 {
		<-t.inputs
	}
	t.mu.Lock()
	t.exitOK = waitErr == nil
	t.exitCode = hostCLIExitCode(waitErr)
	for viewer := range t.clients {
		viewer.closed = true
		viewer.signal()
		delete(t.clients, viewer)
	}
	t.mu.Unlock()
	close(t.done)
}

func (t *terminalSession) read() {
	buffer := make([]byte, 16<<10)
	for {
		n, err := t.file.Read(buffer)
		if n > 0 {
			chunk := append([]byte(nil), buffer[:n]...)
			t.mu.Lock()
			if t.kind != "claude-login" {
				t.history = append(t.history, chunk...)
				t.written += uint64(len(chunk))
				if len(t.history) > terminalHistoryLimit {
					t.history = t.history[len(t.history)-terminalHistoryLimit:]
				}
			}
			for viewer := range t.clients {
				viewer.push(chunk)
			}
			t.mu.Unlock()
		}
		if err != nil {
			break
		}
	}
}
