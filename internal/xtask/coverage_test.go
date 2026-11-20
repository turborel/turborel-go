package main

import (
	"errors"
	"strings"
	"testing"
)

// TestDriverCoverage counts the driver's statements that ran, and
// leaves xtask's own out.
//
// Verifies: [TOOL] R-COV-1 coverage is the share of the driver's
// statements that ran, xtask's own not counted.
func TestDriverCoverage(t *testing.T) {
	t.Parallel()

	profile := strings.Join([]string{
		"mode: atomic",
		"github.com/turborel/turborel-go/frame.go:1.1,2.2 3 1",
		"github.com/turborel/turborel-go/error.go:1.1,2.2 1 0",
		toolPackage + "main.go:1.1,2.2 50 0",
		"",
	}, "\n")

	got, err := driverCoverage(strings.NewReader(profile))
	if err != nil {
		t.Fatalf("driverCoverage: %v", err)
	}

	if want := 75.0; got != want {
		t.Errorf("driverCoverage = %v, want %v", got, want)
	}
}

// TestDriverCoverageRefuses refuses a profile it cannot read, and one of
// no driver statement.
//
// Verifies: [TOOL] R-COV-1 a malformed profile, or one that counts
// nothing, fails the check rather than passing it.
func TestDriverCoverageRefuses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, profile string
		want          error
	}{
		{"empty", "mode: atomic\n", errNoStatements},
		{"fields", "f.go:1.1,2.2 3\n", errMalformedBlock},
		{"statements", "f.go:1.1,2.2 x 1\n", errMalformedBlock},
		{"count", "f.go:1.1,2.2 3 x\n", errMalformedBlock},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if _, err := driverCoverage(strings.NewReader(c.profile)); !errors.Is(err, c.want) {
				t.Errorf("driverCoverage(%q) = %v, want %v", c.profile, err, c.want)
			}
		})
	}
}

// TestHoldToFloor passes coverage at or above the floor and fails it
// below.
//
// Verifies: [TOOL] R-COV-2 coverage below its floor fails verify.
func TestHoldToFloor(t *testing.T) {
	t.Parallel()

	if err := holdToFloor(coverageFloor, coverageFloor); err != nil {
		t.Errorf("holdToFloor at the floor = %v, want nil", err)
	}

	if err := holdToFloor(coverageFloor-1, coverageFloor); !errors.Is(err, errBelowFloor) {
		t.Errorf("holdToFloor below the floor = %v, want %v", err, errBelowFloor)
	}
}
