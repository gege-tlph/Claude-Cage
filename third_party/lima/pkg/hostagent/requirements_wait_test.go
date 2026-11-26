// SPDX-FileCopyrightText: Copyright The Lima Authors
// SPDX-License-Identifier: Apache-2.0

package hostagent

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

// A followed first boot can install packages for longer than the ordinary
// 200 checks of 3 seconds: with a deadline the boot marker keeps being
// checked until it appears (upstream #981).
func TestFinalRequirementRetryBudget(t *testing.T) {
	marker := []requirement{{description: "boot marker"}}
	for _, tc := range []struct {
		name    string
		retries int
		ready   bool
		calls   int
	}{
		{"ordinary", 200, false, 200},
		{"followed first boot", 0, true, 201},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), cloudInitMonitorTimeout)
				defer cancel()
				calls := 0
				err := checkRequirements(ctx, "final", marker, tc.retries, func(requirement) error {
					calls++
					if calls == 201 {
						return nil
					}
					return errors.New("still installing")
				})
				if (err == nil) != tc.ready || calls != tc.calls {
					t.Fatalf("ready=%v after %d checks (%v), want ready=%v after %d", err == nil, calls, err, tc.ready, tc.calls)
				}
			})
		})
	}
}

// The deadline or a stop ends the wait at the next check, not after the
// remaining attempts.
func TestRequirementWaitStopsBetweenChecks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for _, userCancel := range []bool{false, true} {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			calls := 0
			err := checkRequirements(ctx, "final", []requirement{{description: "boot marker"}}, 0, func(requirement) error {
				calls++
				if userCancel {
					cancel()
				}
				return errors.New("still installing")
			})
			cancel()
			want := context.DeadlineExceeded
			if userCancel {
				want = context.Canceled
			}
			if !errors.Is(err, want) || calls != 1 {
				t.Fatalf("got %v after %d checks, want %v after one check", err, calls, want)
			}
		}
	})
}

// A fatal requirement stops the checks at once, also without an attempt
// limit.
func TestFatalRequirementStopsWithoutLimit(t *testing.T) {
	calls := 0
	err := checkRequirements(context.Background(), "essential", []requirement{{description: "ssh", fatal: true}, {description: "never"}}, 0, func(requirement) error {
		calls++
		return errors.New("refused")
	})
	if err == nil || calls != 1 {
		t.Fatalf("fatal requirement: %v after %d checks", err, calls)
	}
}
