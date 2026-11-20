package main

import (
	"errors"
	"strings"
	"testing"
)

// TestRunRefusesUsage refuses a command line it does not take, before
// running anything.
//
// Verifies: [TOOL] R-PROC-5 xtask takes exactly one command it knows.
func TestRunRefusesUsage(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{nil, {"verify", "extra"}, {"nope"}} {
		var out strings.Builder
		if err := run(args, &out); !errors.Is(err, errUsage) {
			t.Errorf("run(%q) = %v, want %v", args, err, errUsage)
		}
	}
}
