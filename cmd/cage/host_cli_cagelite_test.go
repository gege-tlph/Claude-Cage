package main

import (
	"slices"
	"testing"
)

// The Windows installer runs `cage user-path add|remove DIR` (#861); it must
// stay a Cage command instead of reaching Claude Code.
func TestUserPathStaysACageCommand(t *testing.T) {
	in := []string{"user-path", "add", "C:/Program Files/Cage/resources"}
	if got := cageArgs(in); !slices.Equal(got, in) {
		t.Fatalf("%q -> %q", in, got)
	}
}
