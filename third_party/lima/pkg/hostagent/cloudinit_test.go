// SPDX-FileCopyrightText: Copyright The Lima Authors
// SPDX-License-Identifier: Apache-2.0

package hostagent

import (
	"bytes"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"unicode"

	"github.com/lima-vm/sshocker/pkg/ssh"
	"gotest.tools/v3/assert"

	"github.com/lima-vm/lima/v2/pkg/hostagent/events"
)

func TestEmitCloudInitProgressEventStripsControlChars(t *testing.T) {
	var buf bytes.Buffer
	a := &HostAgent{eventEnc: json.NewEncoder(&buf)}

	// A compromised guest can put arbitrary bytes in its cloud-init log, including
	// an ANSI escape sequence that would rewrite the operator's terminal.
	const payload = "starting\x1b[2Kspoofed\x07 line"
	a.emitCloudInitProgressEvent(t.Context(), &events.CloudInitProgress{LogLine: payload})

	var ev events.Event
	assert.NilError(t, json.Unmarshal(buf.Bytes(), &ev))
	assert.Assert(t, ev.Status.CloudInitProgress != nil)
	got := ev.Status.CloudInitProgress.LogLine
	for _, r := range got {
		assert.Assert(t, unicode.IsPrint(r), "control char %#q survived in %#q", r, got)
	}
	assert.Equal(t, got, "starting[2Kspoofed line")
	assert.Assert(t, !strings.ContainsRune(got, '\x1b'))
}

// A cloud-init line is sent once: the port forwarding events that follow do
// not repeat it, so the start log shows it, and "done", only once (#828).
func TestEmitEventDoesNotRepeatCloudInitProgress(t *testing.T) {
	var buf bytes.Buffer
	a := &HostAgent{eventEnc: json.NewEncoder(&buf)}
	a.emitCloudInitProgressEvent(t.Context(), &events.CloudInitProgress{Completed: true, LogLine: "Cloud-init monitoring completed successfully"})
	a.emitPortForwardEvent(t.Context(), &events.PortForwardEvent{Type: events.PortForwardEventForwarding})

	dec := json.NewDecoder(&buf)
	var first, second events.Event
	assert.NilError(t, dec.Decode(&first))
	assert.NilError(t, dec.Decode(&second))
	assert.Assert(t, first.Status.CloudInitProgress != nil)
	assert.Assert(t, second.Status.CloudInitProgress == nil, "repeated: %+v", second.Status.CloudInitProgress)
	assert.Assert(t, second.Status.PortForward != nil)
}

func TestIsCloudInitFinalMessage(t *testing.T) {
	assert.Assert(t, isCloudInitFinalMessage("Cloud-init v. 25.1.4 finished at Mon, 23 Nov 2026 16:25:10 +0000. Datasource DataSourceNoCloud [seed=/dev/sr0].  Up 91.86 seconds"))
	for _, line := range []string{
		"Cloud-init v. 25.1.4 running 'modules:final' at Mon, 23 Nov 2026 16:23:48 +0000. Up 10.41 seconds.",
		"LIMA 2026-11-24T03:25:10+11:00| Exiting with code 0",
		"Finished cloud-final.service - Cloud-init: Final Stage.",
		"Setting up cloud-init (25.1.4-1) ... finished at boot",
	} {
		assert.Assert(t, !isCloudInitFinalMessage(line), line)
	}
}

// On Windows the monitor's own SSH commands must not ask for a ControlMaster:
// the native client then exits at once with status -1 and the monitor never
// sees a line (#828).
func TestCommandSSHConfigMultiplexing(t *testing.T) {
	base := &ssh.SSHConfig{AdditionalArgs: []string{"-F", "/dev/null", "-o", "ControlMaster=auto", "-o", "ControlPath='C:/lima/ssh.sock'", "-o", "ControlPersist=yes", "-o", "User=u"}}
	a := &HostAgent{sshConfig: base}
	args := strings.Join(a.commandSSHConfig().Args(), " ")
	if runtime.GOOS != "windows" {
		assert.Equal(t, a.commandSSHConfig(), base)
		return
	}
	assert.Assert(t, strings.Contains(args, "ControlMaster=no"), args)
	assert.Assert(t, strings.Contains(args, "ControlPath=none"), args)
	assert.Assert(t, !strings.Contains(args, "ControlMaster=auto"), args)
	assert.Assert(t, !strings.Contains(args, "ssh.sock"), args)
	assert.Assert(t, strings.Contains(args, "User=u"), args)
	assert.Equal(t, strings.Join(base.AdditionalArgs, " "), "-F /dev/null -o ControlMaster=auto -o ControlPath='C:/lima/ssh.sock' -o ControlPersist=yes -o User=u")
}
