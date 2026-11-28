package cli

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sylvinus/agent-vm/internal/offlinepack"
	"github.com/sylvinus/agent-vm/internal/version"
	"github.com/sylvinus/agent-vm/internal/vm"
	"github.com/sylvinus/agent-vm/internal/vm/vmtest"
)

// offlineDataDir makes installation data for this build with one guest file.
func offlineDataDir(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "apt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "apt", "sources.list"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("deb file:/x trixie main\n"))
	m := offlinepack.Manifest{Format: offlinepack.Format, Version: version.Version, Arch: runtime.GOARCH,
		Image: offlinepack.Image{Path: "image/debian.qcow2", URL: "https://cloud.debian.org/x.qcow2", Digest: "sha512:00", Size: 1},
		Files: []offlinepack.File{{Path: "apt/sources.list", Size: int64(len("deb file:/x trixie main\n")), SHA256: hex.EncodeToString(sum[:])}}}
	data, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, offlinepack.ManifestName), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCageSetupInstallsFromTheOfflineData(t *testing.T) {
	t.Setenv("CLAUDE_CAGE_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv(offlinepack.DataEnv, offlineDataDir(t, "deb file:/x trixie main\n"))
	te := newTestEnv(t, vmtest.New())
	var commands, unpacked []string
	te.fake.ShellFunc = func(_ string, o vm.ShellOpts) int {
		command := strings.Join(o.Args, " ")
		if strings.HasPrefix(command, "bash -c ") && strings.Contains(command, "tar -x") {
			r := tar.NewReader(o.Stdin)
			for h, err := r.Next(); err == nil; h, err = r.Next() {
				unpacked = append(unpacked, h.Name)
			}
		} else {
			_, _ = io.Copy(io.Discard, o.Stdin)
		}
		if os.Getenv(offlinepack.InstallEnv) != "1" {
			t.Errorf("%s ran without %s", command, offlinepack.InstallEnv)
		}
		commands = append(commands, command)
		return 0
	}
	if code := te.setupBase([]string{"setup", "--preinstall=none"}); code != 0 {
		t.Fatalf("%d: %s", code, te.out())
	}
	if len(commands) != 4 || !strings.HasPrefix(commands[0], "bash -c ") || commands[1] != "bash -s" || commands[2] != "bash -l" || commands[3] != "bash -s" {
		t.Fatalf("guest commands: %q", commands)
	}
	if strings.Join(unpacked, ",") != "apt/,apt/sources.list" {
		t.Fatalf("unpacked %q", unpacked)
	}
	if os.Getenv(offlinepack.InstallEnv) != "" {
		t.Fatal("the offline flag outlived the setup")
	}
}

func TestCageSetupStopsOnChangedOfflineData(t *testing.T) {
	t.Setenv("CLAUDE_CAGE_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv(offlinepack.DataEnv, offlineDataDir(t, "deb file:/y trixie main\n"))
	te := newTestEnv(t, vmtest.New())
	if code := te.setupBase([]string{"setup", "--preinstall=none"}); code != 1 {
		t.Fatalf("changed data accepted: %s", te.out())
	}
	if !strings.Contains(te.out(), OfflineFailedMarker+" corrupt apt") || strings.Contains(te.fake.CallLog(), "create ") {
		t.Fatalf("output %s\ncalls %s", te.out(), te.fake.CallLog())
	}
}

func TestCageSetupWithoutDataDownloadsAsBefore(t *testing.T) {
	t.Setenv("CLAUDE_CAGE_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv(offlinepack.DataEnv, t.TempDir())
	te := newTestEnv(t, vmtest.New())
	var commands []string
	te.fake.ShellFunc = func(_ string, o vm.ShellOpts) int {
		_, _ = io.Copy(io.Discard, o.Stdin)
		commands = append(commands, strings.Join(o.Args, " "))
		return 0
	}
	if code := te.setupBase([]string{"setup", "--preinstall=none"}); code != 0 {
		t.Fatalf("%d: %s", code, te.out())
	}
	if strings.Join(commands, "|") != "bash -l|bash -s" || strings.Contains(te.out(), "installation files") {
		t.Fatalf("guest commands %q, output %s", commands, te.out())
	}
}
