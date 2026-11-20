package main

// The checks of `verify`, in the order they run: the cheapest first, so a
// change fails as soon as it can (RULES.md R-PROC-1).

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// The tools xtask runs by module path and version, pinned so that every
// machine runs the same (RULES.md D11). `go run` builds each once and
// keeps it in the build cache.
const (
	golangciLintModule = "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0"
	govulncheckModule  = "golang.org/x/vuln/cmd/govulncheck@v1.9.0"
)

// errUnformatted is gofmt naming files it would change.
var errUnformatted = errors.New("gofmt: these files are not formatted")

// verify runs every check a change must pass, stopping at the first
// that fails.
func verify(out io.Writer) error {
	checks := []func(io.Writer) error{
		gofmt,
		func(w io.Writer) error {
			return step(w, "go mod tidy", "go", "mod", "tidy", "-diff")
		},
		func(w io.Writer) error {
			return step(w, "go vet", "go", "vet", "./...")
		},
		func(w io.Writer) error {
			return step(w, "golangci-lint", "go", "run", golangciLintModule, "run", "./...")
		},
		citations,
		manual,
		coverage,
	}
	for _, check := range checks {
		if err := check(out); err != nil {
			return err
		}
	}

	report(out, "every check passed")

	return nil
}

// gofmt fails if gofmt, simplifying, would change any file (R-STYLE-1).
func gofmt(out io.Writer) error {
	report(out, "gofmt")

	cmd := exec.CommandContext(context.Background(), "gofmt", "-s", "-l", ".")
	cmd.Stderr = os.Stderr

	listed, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("gofmt: %w", err)
	}

	if len(bytes.TrimSpace(listed)) > 0 {
		return fmt.Errorf("%w:\n%s", errUnformatted, listed)
	}

	return nil
}
