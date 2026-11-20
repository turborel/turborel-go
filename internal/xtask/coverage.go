package main

// The tests, under the race detector, and the coverage floor (RULES.md
// R-COV-1, R-COV-2).

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// coverageFloor is the least statement coverage of the driver, in
// percent. It only rises (R-COV-2).
const coverageFloor = 90.0

// toolPackage is the path prefix of xtask's own files in a coverage
// profile. Its tests verify its decisions (R-TOOL-2), but it is no part
// of the driver, so its statements do not count toward the floor.
const toolPackage = "github.com/turborel/turborel-go/internal/xtask/"

// percent turns a fraction into a percentage.
const percent = 100.0

// profileFields is the number of fields of a coverage profile's line
// after its mode line: the block, its statements, and its count.
const profileFields = 3

// The errors of the coverage check.
var (
	errBelowFloor     = errors.New("coverage is below its floor")
	errNoStatements   = errors.New("the coverage profile counts no statement of the driver")
	errMalformedBlock = errors.New("a coverage profile line is malformed")
)

// coverage runs every test with the race detector and a coverage
// profile, then holds the driver's coverage to its floor.
func coverage(out io.Writer) error {
	dir, err := os.MkdirTemp("", "turborel-go-coverage-")
	if err != nil {
		return fmt.Errorf("coverage: %w", err)
	}

	defer func() { _ = os.RemoveAll(dir) }() // A temporary directory left behind harms nothing.

	profile := filepath.Join(dir, "cover.out")
	err = step(out, "go test -race", "go", "test", "-race", "-covermode=atomic", "-coverprofile="+profile, "./...")
	if err != nil {
		return err
	}

	file, err := os.Open(profile) //nolint:gosec // G304: the path is the profile this function just had written.
	if err != nil {
		return fmt.Errorf("coverage: %w", err)
	}

	defer func() { _ = file.Close() }() // Read only: closing cannot lose data.

	covered, err := driverCoverage(file)
	if err != nil {
		return err
	}

	report(out, fmt.Sprintf("coverage %.1f%% of statements, floor %.1f%%", covered, coverageFloor))

	return holdToFloor(covered, coverageFloor)
}

// holdToFloor fails if covered is below floor.
func holdToFloor(covered, floor float64) error {
	if covered < floor {
		return fmt.Errorf("%w: %.1f%% < %.1f%%", errBelowFloor, covered, floor)
	}

	return nil
}

// driverCoverage reads a coverage profile and gives the percentage of the
// driver's statements that ran, xtask's own left out.
func driverCoverage(profile io.Reader) (float64, error) {
	var statements, ran uint64

	scanner := bufio.NewScanner(profile)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "mode:") || strings.HasPrefix(line, toolPackage) || line == "" {
			continue
		}

		count, hit, err := profileBlock(line)
		if err != nil {
			return 0, err
		}

		statements += count
		if hit {
			ran += count
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("coverage: %w", err)
	}

	if statements == 0 {
		return 0, errNoStatements
	}

	return float64(ran) * percent / float64(statements), nil
}

// profileBlock reads one block of a profile, `file:from,to statements
// count`, and gives its statements and whether it ran.
func profileBlock(line string) (uint64, bool, error) {
	fields := strings.Fields(line)
	if len(fields) != profileFields {
		return 0, false, fmt.Errorf("%w: %q", errMalformedBlock, line)
	}

	statements, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("%w: %q", errMalformedBlock, line)
	}

	count, err := strconv.ParseUint(fields[2], 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("%w: %q", errMalformedBlock, line)
	}

	return statements, count > 0, nil
}
