package main

import (
	"io"
	"os"

	ssh "github.com/gliderlabs/ssh"
)

// sessionInput feeds the SSH input stream to a command through an OS pipe.
// With the session itself as Stdin, cmd.Wait also waits for the input copy,
// so `ssh host cmd` never returned while the client kept stdin open.
func sessionInput(s ssh.Session) (*os.File, func(), error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	go func() { _, _ = io.Copy(w, s); _ = w.Close() }()
	return r, func() { _ = r.Close(); _ = w.Close() }, nil
}
