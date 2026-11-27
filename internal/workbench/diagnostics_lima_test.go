package workbench

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/sylvinus/agent-vm/internal/diagnostics"
)

func TestLimaInstanceStateNamesDisplayPortAndPIDs(t *testing.T) {
	dir := t.TempDir()
	rule := "portForwards:\n- guestIP: 127.0.0.1\n  guestPortRange: [15900, 15900]\n  hostIP: 127.0.0.1\n  hostPortRange: [5573, 5573]\n  proto: tcp\n"
	files := map[string]string{
		"lima.yaml":     rule,
		"ha.pid":        strconv.Itoa(os.Getpid()) + "\n",
		"qemu.pid":      "not a pid",
		"ha.stdout.log": "{}\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	supplement := limaInstanceState("environment", dir)
	if supplement.Name != "lima/environment/state.json" {
		t.Fatalf("name %q", supplement.Name)
	}
	var state struct {
		VNC   string                   `json:"vnc_host_address"`
		Files map[string]limaFileState `json:"files"`
	}
	// Read it back from a bundle: the record must survive redaction.
	var bundle bytes.Buffer
	if err := diagnostics.New(t.TempDir()).Export(&bundle, "test", nil, "offline", supplement); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(bundle.Bytes()), int64(bundle.Len()))
	if err != nil {
		t.Fatal(err)
	}
	f, err := archive.Open(supplement.Name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(&state); err != nil {
		t.Fatal(err)
	}
	if state.VNC != "127.0.0.1:5573" {
		t.Errorf("display %q", state.VNC)
	}
	if ha := state.Files["ha_pid"]; ha.PID != os.Getpid() || ha.Alive == nil || !*ha.Alive {
		t.Errorf("ha.pid %+v", ha)
	}
	if qemu, ok := state.Files["qemu_pid"]; !ok || qemu.PID != 0 || qemu.Alive != nil {
		t.Errorf("qemu.pid %+v", qemu)
	}
	if _, ok := state.Files["serial_log"]; ok || len(state.Files) != len(files) {
		t.Errorf("files %v", state.Files)
	}
}
