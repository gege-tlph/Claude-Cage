//go:build !windows

package workbench

import (
	"context"
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

func rawCLIConsole(in, out *os.File) (func(), error) {
	fd := int(in.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	return func() { restoreCLIDisplay(out); _ = term.Restore(fd, state) }, nil
}

// pumpCLIInput sends what r gives until it ends. rawConsole says the input is
// a console in raw mode; a raw Unix terminal hands Ctrl+Z over as a byte, so
// nothing differs here.
func pumpCLIInput(ctx context.Context, r io.Reader, rawConsole bool, send func([]byte) error) error {
	f, ok := r.(*os.File)
	if !ok {
		return copyCLIInput(ctx, r.Read, send)
	}
	fd := int(f.Fd())
	poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	return copyCLIInput(ctx, func(buf []byte) (int, error) {
		for ctx.Err() == nil {
			n, err := unix.Poll(poll, 150)
			if errors.Is(err, unix.EINTR) {
				continue
			}
			if err != nil {
				return 0, err
			}
			if n == 0 {
				continue
			}
			n, err = unix.Read(fd, buf)
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR) {
				continue
			}
			if n == 0 && err == nil {
				return 0, io.EOF
			}
			return n, err
		}
		return 0, ctx.Err()
	}, send)
}
