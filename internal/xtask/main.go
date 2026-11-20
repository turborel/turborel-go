// Command xtask runs the driver's checks, as `go run ./internal/xtask
// <command>`, the same on Linux, macOS and Windows (RULES.md D5). It is
// Go, never a shell script, so every platform runs one program.
//
// Its commands:
//
//	verify         every check a change must pass (RULES.md R-PROC-1)
//	coverage       the tests with coverage, its report and its floor
//	vulncheck      govulncheck over the module, as CI runs it
//	install-hooks  run verify before every commit, once per clone
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// The commands, as typed after `go run ./internal/xtask`.
const (
	commandVerify       = "verify"
	commandCoverage     = "coverage"
	commandVulncheck    = "vulncheck"
	commandInstallHooks = "install-hooks"
)

// usage is printed for a command not known.
const usage = "usage: go run ./internal/xtask {verify|coverage|vulncheck|install-hooks}"

// exitFailed is the status of a check that failed, or a command not
// known.
const exitFailed = 1

// errUsage is a command line xtask does not take.
var errUsage = errors.New(usage)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "xtask:", err)
		os.Exit(exitFailed)
	}
}

// run runs the command args names, reporting each step to out.
func run(args []string, out io.Writer) error {
	if len(args) != 1 {
		return errUsage
	}

	switch args[0] {
	case commandVerify:
		return verify(out)
	case commandCoverage:
		return coverage(out)
	case commandVulncheck:
		return step(out, "govulncheck", "go", "run", govulncheckModule, "./...")
	case commandInstallHooks:
		return step(out, "hooks", "git", "config", "core.hooksPath", ".githooks")
	default:
		return errUsage
	}
}

// step runs one program as a named step, its output passed through. The
// step fails if the program does.
func step(out io.Writer, name, program string, args ...string) error {
	report(out, name)

	cmd := exec.CommandContext(context.Background(), program, args...)
	cmd.Stdout = out
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	return nil
}

// report writes the line that begins a step.
func report(out io.Writer, line string) {
	_, _ = io.WriteString(out, "== "+line+"\n") // A progress line lost changes no check's outcome.
}
