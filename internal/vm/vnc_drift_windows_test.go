package vm

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lima-vm/lima/v2/pkg/limatype/filenames"
	"github.com/lima-vm/lima/v2/pkg/portfwd"
)

// The display's address is the one lima.yaml names (VNCAddress), and a start
// chooses it. Whatever state a start finds the VM's files and processes in,
// it must leave the two agreeing: while the VM's own hostagent still forwards
// the display, lima.yaml names that forward's port; once nothing of the VM
// does, a port the next forward can bind. Each state below is built for real
// (PID files, stand-in processes, held ports) and Lima's own code reads it.

type pidState int

const (
	pidAbsent pidState = iota
	pidLive
	pidExited  // a process that has exited
	pidReused  // a live process newer than the file: Lima takes the PID for reused
	pidCorrupt // not a number
)

var pidNames = [...]string{"无", "存活", "已退出", "PID复用", "损坏"}

type apiState int

const (
	apiNone apiState = iota // no ha.sock
	apiOK
	apiSlow // answers after Lima's 3 s limit
)

var apiNames = [...]string{"无接口", "接口正常", "接口超时"}

type holderState int

const (
	holderNone    holderState = iota
	holderOwn                 // the VM's own hostagent, alive, forwards the display there
	holderForeign             // another program
)

var holderNames = [...]string{"端口空闲", "本环境转发占用", "其他程序占用"}

type ruleState int

const (
	ruleNone    ruleState = iota // a VM from before 0.2.3: Lima's default forward
	ruleDefault                  // the rule names the default port
	ruleOther                    // the rule names another port
)

var ruleNames = [...]string{"无规则", "规则=默认端口", "规则=其他端口"}

type opKind int

const (
	opStart   opKind = iota
	opRestart        // Stop, then Start once the stop succeeded
	opTwoStarts
)

var opNames = [...]string{"启动", "重启", "并发两次启动"}

type driftCase struct {
	ha     pidState
	api    apiState
	qemu   pidState
	holder holderState
	rule   ruleState
	op     opKind
}

func (c driftCase) String() string {
	return fmt.Sprintf("ha.pid=%s/%s qemu.pid=%s %s %s %s", pidNames[c.ha], apiNames[c.api], pidNames[c.qemu], holderNames[c.holder], ruleNames[c.rule], opNames[c.op])
}

// driftCases lists every state: the API only matters for a hostagent whose
// PID Lima takes. Without CAGE_VNC_DRIFT_FULL a subset that still has every
// hostagent state: a slow API costs 3 s per inspection, and the 855 states
// with every operation take about 15 minutes here.
func driftCases(full bool) []driftCase {
	var cases []driftCase
	for _, ha := range []pidState{pidAbsent, pidLive, pidExited, pidReused, pidCorrupt} {
		apis := []apiState{apiNone}
		if ha == pidLive {
			apis = []apiState{apiNone, apiOK, apiSlow}
		}
		for _, api := range apis {
			for _, qemu := range []pidState{pidAbsent, pidLive, pidExited, pidReused, pidCorrupt} {
				for _, holder := range []holderState{holderNone, holderOwn, holderForeign} {
					for _, rule := range []ruleState{ruleNone, ruleDefault, ruleOther} {
						for _, op := range []opKind{opStart, opRestart, opTwoStarts} {
							if !full && (api == apiSlow || qemu == pidExited || qemu == pidReused || holder == holderNone || rule != ruleOther) {
								continue
							}
							cases = append(cases, driftCase{ha, api, qemu, holder, rule, op})
						}
					}
				}
			}
		}
	}
	return cases
}

type standIn struct {
	cmd   *exec.Cmd
	stdin io.Closer
	done  chan struct{}
}

func (p *standIn) alive() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func (p *standIn) stop() {
	p.stdin.Close()
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		p.cmd.Process.Kill()
		<-p.done
	}
}

type driftHarness struct {
	t       *testing.T
	l       *Lima
	name    string
	dir     string
	yaml    []byte
	exe     string
	qemuExe string
	// defaultPort stands for 15900: Lima's default forward, and what a start
	// tries after the previous port.
	defaultPort int
}

func (h *driftHarness) spawn(exe, role string, env []string, args ...string) *standIn {
	h.t.Helper()
	cmd := exec.Command(exe, args...)
	cmd.Env = append(append(os.Environ(), standInEnv+"="+role), env...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		h.t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		h.t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		h.t.Fatal(err)
	}
	p := &standIn{cmd: cmd, stdin: stdin, done: make(chan struct{})}
	lines := bufio.NewReader(stdout)
	if role != "exit" {
		line, _ := lines.ReadString('\n')
		if strings.TrimSpace(line) != "ready" {
			cmd.Process.Kill()
			h.t.Fatalf("%s stand-in: %q", role, line)
		}
	}
	go func() {
		io.Copy(io.Discard, lines)
		cmd.Wait()
		close(p.done)
	}()
	return p
}

func (h *driftHarness) exitedPID() int {
	p := h.spawn(h.exe, "exit", nil)
	<-p.done
	return p.cmd.Process.Pid
}

func (h *driftHarness) writePID(file string, state pidState, live *standIn) {
	h.t.Helper()
	path := filepath.Join(h.dir, file)
	var content string
	switch state {
	case pidAbsent:
		return
	case pidLive, pidReused:
		content = strconv.Itoa(live.cmd.Process.Pid)
	case pidExited:
		content = strconv.Itoa(h.exitedPID())
	case pidCorrupt:
		content = "not-a-pid"
	}
	if err := os.WriteFile(path, []byte(content+"\n"), 0o644); err != nil {
		h.t.Fatal(err)
	}
	if state == pidReused {
		past := time.Now().Add(-time.Hour)
		if err := os.Chtimes(path, past, past); err != nil {
			h.t.Fatal(err)
		}
	}
}

func (h *driftHarness) reset() {
	h.t.Helper()
	for _, f := range []string{filenames.HostAgentPID, filenames.PIDFile("qemu"), filenames.HostAgentSock, filenames.BootSession} {
		if err := os.Remove(filepath.Join(h.dir, f)); err != nil && !errors.Is(err, os.ErrNotExist) {
			h.t.Fatal(err)
		}
	}
}

type driftResult struct {
	c        driftCase
	status   string
	err      error
	got      int // the port lima.yaml names afterwards
	forward  int // the port the VM's own hostagent forwards, 0 when none is alive
	verdict  string
	duration time.Duration
}

func (h *driftHarness) run(c driftCase) driftResult {
	h.t.Helper()
	h.reset()
	defer h.reset()
	// The port lima.yaml names before the start, and the own forward's.
	previous := h.defaultPort
	yaml := h.yaml
	if c.rule != ruleNone {
		if c.rule == ruleOther {
			previous = unusedPort(h.t)
		}
		yaml = append(append([]byte{}, yaml...), fmt.Sprintf("\nportForwards:\n- guestIP: 127.0.0.1\n  guestPortRange: [%d, %d]\n  hostIP: 127.0.0.1\n  hostPortRange: [%d, %d]\n  proto: tcp\n", vncGuestPort, vncGuestPort, previous, previous)...)
	}
	if err := os.WriteFile(filepath.Join(h.dir, filenames.LimaYAML), yaml, 0o644); err != nil {
		h.t.Fatal(err)
	}
	defer os.WriteFile(filepath.Join(h.dir, filenames.LimaYAML), h.yaml, 0o644)

	var qemu, agent *standIn
	if c.qemu == pidLive || c.qemu == pidReused {
		qemu = h.spawn(h.qemuExe, "qemu", nil, "-name", "lima-"+h.name, "-pidfile", filepath.Join(h.dir, filenames.PIDFile("qemu")))
		defer qemu.stop()
	}
	if c.ha == pidLive || c.ha == pidReused || c.holder == holderOwn {
		env := []string{standInStopDir + "=" + h.dir}
		if qemu != nil {
			env = append(env, standInQEMU+"="+strconv.Itoa(qemu.cmd.Process.Pid))
		}
		if c.holder == holderOwn {
			env = append(env, standInListen+"="+net.JoinHostPort("127.0.0.1", strconv.Itoa(previous)))
		}
		if c.ha == pidLive && c.api != apiNone {
			env = append(env, standInSocket+"="+filepath.Join(h.dir, filenames.HostAgentSock))
			if c.api == apiSlow {
				env = append(env, standInSocketDelay+"=4s")
			}
		}
		agent = h.spawn(h.exe, "hostagent", env)
		defer agent.stop()
	}
	if c.holder == holderForeign {
		foreign, err := (&net.ListenConfig{Control: portfwd.Control}).Listen(context.Background(), "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(previous)))
		if err != nil {
			h.t.Fatal(err)
		}
		defer foreign.Close()
	}
	h.writePID(filenames.HostAgentPID, c.ha, agent)
	h.writePID(filenames.PIDFile("qemu"), c.qemu, qemu)

	ctx := context.Background()
	r := driftResult{c: c}
	if inst, err := h.l.inspect(ctx, h.name); err == nil {
		r.status = string(inst.Status)
	}
	before := VNCAddress(h.dir)
	stopLeftAgent := false
	began := time.Now()
	switch c.op {
	case opStart:
		r.err = h.l.Start(ctx, h.name, io.Discard)
	case opRestart:
		if r.err = h.l.Stop(ctx, h.name); r.err == nil {
			stopLeftAgent = agent != nil && agent.alive()
			r.err = h.l.Start(ctx, h.name, io.Discard)
		} else {
			r.err = fmt.Errorf("stop: %w", r.err)
		}
	case opTwoStarts:
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i := range errs {
			wg.Add(1)
			go func() { defer wg.Done(); errs[i] = h.l.Start(ctx, h.name, io.Discard) }()
		}
		wg.Wait()
		r.err = errors.Join(errs...)
	}
	r.duration = time.Since(began)

	after := VNCAddress(h.dir)
	_, port, _ := net.SplitHostPort(after)
	r.got, _ = strconv.Atoi(port)
	agentAlive := agent != nil && agent.alive()
	if c.holder == holderOwn && agentAlive {
		r.forward = previous
	}
	switch {
	case r.forward != 0 && r.got != r.forward:
		// The display dark: the workbench dials a port nothing forwards.
		r.verdict = "错位"
	case agentAlive && after != before:
		// The running hostagent keeps the forward it started with.
		r.verdict = "运行中被改"
	case stopLeftAgent:
		// A stop that reported success, its hostagent still running.
		r.verdict = "停止未停"
	case !agentAlive && errors.Is(r.err, ErrUnconfined) && portfwd.InUse(ctx, "tcp", after) != nil:
		// A start that chose the port and would boot now: its hostagent
		// could not forward the display. (One that stopped before, such as
		// after a stop that failed, leaves the choice to the next start.)
		r.verdict = "端口不可用"
	default:
		r.verdict = "ok"
	}
	return r
}

func shortErr(err error) string {
	if err == nil {
		return "nil"
	}
	if errors.Is(err, ErrUnconfined) {
		return "(到达开机前检查)"
	}
	s := strings.ReplaceAll(err.Error(), "\n", " ")
	if len(s) > 90 {
		s = s[:90] + "…"
	}
	return s
}

func TestVNCDrift(t *testing.T) {
	ctx := context.Background()
	t.Setenv("CLAUDE_CAGE_CONFIG", "")
	l := newTestLima(t)
	const name = "d-12345678"
	// Unconfined, as in TestLimaEphemeralPorts: Start chooses the port and
	// writes lima.yaml, then refuses to boot it.
	plain := []Mount{{Location: t.TempDir(), Writable: true}}
	rev := ReverseSSHFS
	if err := l.Create(ctx, name, "template:debian-13", Settings{Mounts: &plain, MountType: &rev}, io.Discard); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(os.Getenv("LIMA_HOME"), name)
	base, err := os.ReadFile(filepath.Join(dir, filenames.LimaYAML))
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// windowsQEMUCommandMatches takes a QEMU by its program's name.
	qemuExe := filepath.Join(t.TempDir(), "qemu-system-x86_64.exe")
	if b, err := os.ReadFile(exe); err != nil {
		t.Fatal(err)
	} else if err := os.WriteFile(qemuExe, b, 0o755); err != nil {
		t.Fatal(err)
	}
	h := &driftHarness{t: t, l: l, name: name, dir: dir, yaml: base, exe: exe, qemuExe: qemuExe, defaultPort: unusedPort(t)}
	savedPort, savedAddress := vncDefaultPort, DefaultVNCAddress
	vncDefaultPort, DefaultVNCAddress = h.defaultPort, net.JoinHostPort("127.0.0.1", strconv.Itoa(h.defaultPort))
	t.Cleanup(func() { vncDefaultPort, DefaultVNCAddress = savedPort, savedAddress })

	full := os.Getenv("CAGE_VNC_DRIFT_FULL") != ""
	cases := driftCases(full)
	if filter := os.Getenv("CAGE_VNC_DRIFT_FILTER"); filter != "" {
		kept := cases[:0]
		for _, c := range cases {
			if strings.Contains(c.String(), filter) {
				kept = append(kept, c)
			}
		}
		cases = kept
	}
	bad := map[string]int{}
	for _, c := range cases {
		r := h.run(c)
		line := fmt.Sprintf("%-6s %s | 状态前=%s 结果=%s 端口=%d 转发=%d %s", r.verdict, c, r.status, shortErr(r.err), r.got, r.forward, r.duration.Round(time.Millisecond))
		if os.Getenv("CAGE_VNC_DRIFT_LOG") != "" {
			t.Log(line)
		}
		if r.verdict != "ok" {
			bad[r.verdict]++
			t.Error(line)
		}
	}
	t.Logf("%d cases, problems: %v", len(cases), bad)
}
