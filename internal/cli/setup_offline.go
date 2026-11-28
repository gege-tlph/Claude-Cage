package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/sylvinus/agent-vm/internal/cage"
	"github.com/sylvinus/agent-vm/internal/offlinepack"
	"github.com/sylvinus/agent-vm/internal/version"
	"github.com/sylvinus/agent-vm/internal/vm"
)

// offlineData is the full package's installation data used by this setup:
// everything is installed as usual, the downloads come from it. Claude Code
// is not in it and still downloads.
type offlineData struct {
	dir string
	m   *offlinepack.Manifest
}

// OfflineFailedMarker starts the line that names data which failed its check,
// for the workbench to explain (offline_failure.go there).
const OfflineFailedMarker = "CAGE_OFFLINE_FAILED"

// findOfflineData is the data for this app version, in a Cage setup only;
// nil when the setup downloads.
func findOfflineData() (*offlineData, error) {
	if os.Getenv(cage.ConfigEnv) == "" {
		return nil, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, nil
	}
	dir, m, err := offlinepack.Find(exe, version.Version, runtime.GOARCH)
	if err != nil {
		return nil, fmt.Errorf("%s manifest\nthe installation files of the full package cannot be read: %w", OfflineFailedMarker, err)
	}
	if m == nil {
		return nil, nil
	}
	return &offlineData{dir: dir, m: m}, nil
}

// progressPrinter prints a byte progress line for the workbench at most twice
// a second, and the last one.
func progressPrinter(w io.Writer, op string) func(done, total int64) {
	if os.Getenv("CAGE_SETUP_PROGRESS") != "1" {
		return nil
	}
	var last time.Time
	return func(done, total int64) {
		if now := time.Now(); done == total || now.Sub(last) >= 500*time.Millisecond {
			last = now
			fmt.Fprintf(w, "CAGE_OFFLINE_BYTES %s %d %d\n", op, done, total)
		}
	}
}

// offlineFailure marks err with a fixed code: redaction would hide a file
// name (package names have dots), the code names the kind of file.
func offlineFailure(err error) error {
	var corrupt *offlinepack.CorruptError
	switch {
	case errors.As(err, &corrupt):
		kind, _, _ := strings.Cut(corrupt.Path, "/")
		switch kind {
		case "apt", "tools", "image":
		default:
			kind = "other"
		}
		return fmt.Errorf("%s corrupt %s\n%w", OfflineFailedMarker, kind, err)
	case errors.Is(err, offlinepack.ErrImageNotPinned):
		return fmt.Errorf("%s image\n%w", OfflineFailedMarker, err)
	}
	return err
}

// prepare checks the data and puts the Debian image where Lima looks for its
// download. The image is checked against the digest the app pins.
func (o *offlineData) prepare(ctx context.Context, b vm.Backend, w io.Writer) error {
	fmt.Fprintln(w, "Checking the installation files...")
	if err := offlinepack.Verify(o.dir, o.m, progressPrinter(w, "verify")); err != nil {
		return offlineFailure(err)
	}
	pinned, ok := b.(interface {
		PinnedDebianImage(context.Context) (string, string, error)
	})
	if !ok {
		return nil
	}
	url, digest, err := pinned.PinnedDebianImage(ctx)
	if err != nil {
		return err
	}
	cache, err := offlinepack.LimaCacheDir()
	if err != nil {
		return err
	}
	fmt.Fprintln(w, "Checking the Debian image...")
	if err := offlinepack.SeedImage(cache, o.dir, o.m, url, digest, progressPrinter(w, "image")); err != nil {
		return offlineFailure(err)
	}
	return nil
}

// copyIn unpacks the data in the running build and points APT at it.
func (o *offlineData) copyIn(ctx context.Context, b vm.Backend, name string, w io.Writer) error {
	fmt.Fprintln(w, "Copying installation files...")
	// The tar writer reports progress while the guest's output arrives.
	w = &lockedWriter{w: w}
	pr, pw := io.Pipe()
	streamed := make(chan error, 1)
	go func() {
		err := offlinepack.WriteTar(pw, o.dir, o.m, progressPrinter(w, "copy"))
		pw.CloseWithError(err)
		streamed <- err
	}()
	root := offlinepack.GuestRoot
	unpack := "set -e; sudo rm -rf " + root + "; sudo install -d -m 0755 " + root + "; sudo tar -x --no-same-owner -C " + root
	code, err := b.Shell(ctx, name, vm.ShellOpts{Args: []string{"bash", "-c", unpack}, Stdin: pr, Stdout: w, Stderr: w})
	// A shell that ended early leaves the writer blocked: release it.
	pr.CloseWithError(io.ErrClosedPipe)
	if serr := <-streamed; serr != nil && !errors.Is(serr, io.ErrClosedPipe) {
		return offlineFailure(serr)
	}
	if err == nil && code != 0 {
		err = fmt.Errorf("unpacking the installation files: exit status %d", code)
	}
	if err != nil {
		return err
	}
	code, err = b.Shell(ctx, name, vm.ShellOpts{Args: []string{"bash", "-s"}, Stdin: strings.NewReader(offlinepack.GuestPrepareScript), Stdout: w, Stderr: w})
	if err == nil && code != 0 {
		err = fmt.Errorf("preparing the installation files: exit status %d", code)
	}
	return err
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// finishInstall ends every Cage setup: the copied data and APT's offline
// configuration go, and the disk is marked as installed.
func finishInstall(ctx context.Context, b vm.Backend, name string, w io.Writer) error {
	if os.Getenv(cage.ConfigEnv) == "" {
		return nil
	}
	code, err := b.Shell(ctx, name, vm.ShellOpts{Args: []string{"bash", "-s"}, Stdin: strings.NewReader(offlinepack.GuestFinishScript), Stdout: w, Stderr: w})
	if err == nil && code != 0 {
		err = fmt.Errorf("finishing the installation: exit status %d", code)
	}
	return err
}
