// SPDX-FileCopyrightText: Copyright The Lima Authors
// SPDX-License-Identifier: Apache-2.0

package qemu

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// A guest stopped right after it started has no handler for the power button
// yet and loses the first press. Later presses reach it once it boots far
// enough, so it powers off before the timeout instead of being killed.
func TestAwaitPowerdownPressesAgainUntilTheGuestExits(t *testing.T) {
	saved := powerButtonRepeat
	powerButtonRepeat = 10 * time.Millisecond
	t.Cleanup(func() { powerButtonRepeat = saved })

	qWaitCh := make(chan error, 1)
	exitErr := errors.New("exit status 0")
	var presses atomic.Int32
	press := func() error {
		// The guest handles the third press in all, the second repeat.
		if presses.Add(1) == 2 {
			qWaitCh <- exitErr
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err, exited, closed := awaitPowerdown(ctx, qWaitCh, press)
	if !exited || closed || !errors.Is(err, exitErr) {
		t.Fatalf("got err=%v exited=%v closed=%v, want the guest's exit", err, exited, closed)
	}
	if presses.Load() < 2 {
		t.Fatalf("pressed again %d times, want at least 2", presses.Load())
	}
}

func TestAwaitPowerdownStopsAtTheTimeoutAndOnAClosedChannel(t *testing.T) {
	saved := powerButtonRepeat
	powerButtonRepeat = 10 * time.Millisecond
	t.Cleanup(func() { powerButtonRepeat = saved })

	// The guest never handles the button: the caller's timeout ends the wait
	// and it kills QEMU. A failing press does not end the wait early.
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Millisecond)
	defer cancel()
	var presses atomic.Int32
	_, exited, closed := awaitPowerdown(ctx, make(chan error), func() error {
		presses.Add(1)
		return errors.New("QMP is busy")
	})
	if exited || closed || presses.Load() == 0 {
		t.Fatalf("exited=%v closed=%v presses=%d, want a timeout after repeated presses", exited, closed, presses.Load())
	}

	qWaitCh := make(chan error)
	close(qWaitCh)
	if _, exited, closed := awaitPowerdown(t.Context(), qWaitCh, func() error { return nil }); !exited || !closed {
		t.Fatalf("closed channel: exited=%v closed=%v", exited, closed)
	}
}
