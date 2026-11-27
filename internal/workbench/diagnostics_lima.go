package workbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/sylvinus/agent-vm/internal/cage"
	"github.com/sylvinus/agent-vm/internal/containerenv"
	"github.com/sylvinus/agent-vm/internal/diagnostics"
	"github.com/sylvinus/agent-vm/internal/host"
	"github.com/sylvinus/agent-vm/internal/vm"
)

// limaDiagnostics are the Lima logs of the work environment and of an
// unfinished base build, each with a short state record. A display failure
// needs the hostagent's forwards next to the port lima.yaml names.
func limaDiagnostics(profile string) []diagnostics.Supplement {
	if containerenv.Configured(profile) {
		return nil
	}
	home, _ := limaHomeOf(profile)
	instances := [][2]string{{"base-building", "base-building"}}
	if workspace := environmentWorkspace(profile); workspace != "" {
		instances = append([][2]string{{"environment", cage.InstanceName(profile, workspace)}}, instances...)
	}
	var out []diagnostics.Supplement
	for _, instance := range instances {
		role, dir := instance[0], filepath.Join(home, instance[1])
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			continue
		}
		out = append(out, limaInstanceState(role, dir))
		out = append(out, diagnostics.LimaLogs(role, dir)...)
	}
	return out
}

type limaFileState struct {
	Bytes    int64  `json:"bytes"`
	Modified string `json:"modified"`
	PID      int    `json:"pid,omitempty"`
	Alive    *bool  `json:"alive,omitempty"`
}

// limaInstanceState records which files the instance has and when they last
// changed, the display port its lima.yaml forwards, and on Windows whether
// its hostagent holds the stop event. It copies no configuration.
func limaInstanceState(role, dir string) diagnostics.Supplement {
	state := map[string]any{"log_max_bytes": diagnostics.LimaLogMaxBytes}
	files := map[string]limaFileState{}
	for _, name := range []string{"lima.yaml", "ha.pid", "qemu.pid", "ha.sock", "ha.stdout.log", "ha.stderr.log", "serial.log"} {
		st, err := os.Lstat(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		file := limaFileState{Bytes: st.Size(), Modified: st.ModTime().Format(time.RFC3339Nano)}
		if strings.HasSuffix(name, ".pid") && st.Mode().IsRegular() && st.Size() < 64 {
			if b, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
				if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
					alive := processExists(pid)
					file.PID, file.Alive = pid, &alive
				}
			}
		}
		files[fileKey(name)] = file
	}
	state["files"] = files
	if _, ok := files[fileKey("lima.yaml")]; ok {
		state["vnc_host_address"] = vm.VNCAddress(dir)
	}
	if runtime.GOOS == "windows" {
		state["hostagent_stop_event"] = host.InstanceHostagentRunning(dir)
	}
	b, _ := json.MarshalIndent(state, "", "  ")
	return diagnostics.Supplement{Name: "lima/" + role + "/state.json", Data: b}
}

// fileKey spells a file name without dots: redaction takes a dotted name such
// as lima.yaml for a host.
func fileKey(name string) string { return strings.ReplaceAll(name, ".", "_") }
