# Guide for coding agents

How to work on turborel-go as a coding agent, with any tool. If this file
disagrees with [RULES.md](RULES.md) or [CONTRIBUTING.md](CONTRIBUTING.md),
follow those.

turborel-go is the Go driver for turborel, a relational database system.
It speaks turborel's native protocol, version 1, in Go alone, and will
give a `database/sql` driver and a native API beside it. See
[README.md](README.md).

## Before you change anything

Read RULES.md §3 (code), §4 (tests), §6 (process) and the §1 decisions
for your area. Read CONTRIBUTING.md for how a change is made and the
extra checks for changes that touch the wire.

## Build and check

Every change must pass `go run ./internal/xtask verify` before it is
handed back.

```
go run ./internal/xtask verify          # every check a change must pass
go run ./internal/xtask coverage        # the tests, coverage and its floor alone
go run ./internal/xtask vulncheck       # govulncheck, as CI runs it
go run ./internal/xtask install-hooks   # run verify before every commit (once per clone)
go test -race ./...                     # the tests alone
```

`verify` stops at the first failure. CI runs it on Linux, macOS and
Windows, on Go 1.26 and 1.27 (D9). The coverage floor is in
`internal/xtask/coverage.go` and only rises (D10). golangci-lint and
govulncheck run by `go run` at the versions `internal/xtask/verify.go`
pins (D6), so nothing needs installing but Go.

Project automation is Go in `internal/xtask/`, never shell scripts (D5,
R-TOOL-1).

## Where things are

| Path | What |
|---|---|
| `doc.go` | the package's documentation |
| `error.go` | `Error`, every failure the driver returns |
| `frame.go` | frame headers, encoded and decoded |
| `version.go` | `Version`, the turborel release it is made for |
| `internal/xtask/` | the project's checks |
| `doc/` | the manual: `user/` (Volume I), `client/` (Volume II), `appendix/` |

The driver is one package, `turborel` (D4): each layer is files of it,
named by what they hold, each with its tests beside it in `_test.go`.

What the driver implements comes from turborel's repository, read only:

| Path in turborel | What |
|---|---|
| `doc/client/05-protocol.md` | The Native Protocol, the spec |
| `doc/client/02-library.md` | The Client Library, the features to match |
| `doc/client/02-library-connections.md` | Settings, Pipelines and Pools, the same |
| `client/src/`, `protocol/src/` | turborel-client, the reference: where it and the spec differ, it is right |

## Rules that are broken most often

`verify` treats every finding as an error. RULES.md §3 and §4 have the
full list.

- Every failure is returned as an `error` whose dynamic type is
  `*Error`, with SQLCODE and SQLSTATE (R-ERR-1). Never return a bare
  `errors.New` or `fmt.Errorf` from the library.
- No `panic`, `log.Fatal` or `os.Exit` in library code (R-ERR-2), and no
  printing (R-STYLE-4).
- No length or count from the server is used to allocate before it is
  checked against what remains (R-ERR-4).
- No `unsafe`, no cgo (R-SAFE-1), no import outside the standard library
  (D7, R-SAFE-2), no global mutable state (R-SAFE-3).
- Keep functions to 50 lines and 5 parameters, and files to 600 lines
  (R-SIZE).
- No magic numbers or strings; protocol tags and kinds are named
  constants (R-CONST).
- Exported items have doc comments beginning with their name (R-DOC-1).
  Each file's first comment says its responsibility (R-DOC-4).
- Comments say why (R-DOC-2). No `TODO` comments or commented-out code
  (R-DOC-5).
- An exception to a linter is `//nolint:<linter> // <reason>`, one
  linter and a reason (D6).
- Never write a secret into an error, a log or a `String` method
  (R-SAFE-5).
- Code of one platform sits behind a build constraint (R-PLAT-2).

## Tests

Every test has a paragraph in its doc comment beginning `Verifies:` and a
tag (R-TRACE-1): `[PROTOCOL <heading>]` for The Native Protocol,
`[CLIENT <heading>]` for The Client Library or Settings, Pipelines and
Pools, `[DESIGN] D<n>` for a decision, `[TOOL] R-<id>` for xtask's own.

```go
// TestFrameHeaderLongestBody reads the longest body taken, and refuses
// one byte more.
//
// Verifies: [PROTOCOL Frames] a body of the longest length taken is
// read, and a longer one fails the connection with SQLCODE -919 and
// SQLSTATE 08006.
func TestFrameHeaderLongestBody(t *testing.T) {
```

Tests are deterministic, with no clock, randomness or network beyond
loopback (R-TEST-3), pass under `-race`, and call `t.Parallel`
(R-TEST-4). A fixed defect gets a failing-first regression test
(R-TEST-5).

## Citing the spec

Cite a section by its heading exactly as the spec prints it. The headings
`verify` accepts are in `internal/xtask/headings.go`, taken from
turborel's main at the commit it names; a citation of any other fails.
Never cite from memory: open the document and check. Code never cites
TODO.md, and `verify` refuses it (R-DOC-5).

## Documents

A finished feature updates these in the same change:

- RULES.md, if it made a decision, including the row in §1's table
- CONFORMANCE.md, its row marked done with its tests (R-TRACE-3)
- TODO.md, which only holds open work
- [doc/](doc/README.md), for anything a program sees (R-DOC-6): its User's
  Guide chapter, its API Reference page, and an appendix row for a type,
  an error or a setting

The manual keeps turborel's layout, since turborel's docs site reads it:
each page opens with its `# Title` and a line holding only
`[Contents](README.md)`, links between pages are relative `.md` links,
and a chapter not yet filled says what it will cover and its milestone.

Committed documents say nothing of benchmarks or timings, and every
claim in them matches the code.

## Commits

- Commit only when the maintainer asks, and never push without approval.
- Follow CONTRIBUTING.md's form, with one logical change per commit.
- Add no AI or agent attribution to a commit or pull request. That means
  no `Co-Authored-By` for an agent, no "Generated with" and no
  `Signed-off-by`.
- Stage with an explicit path list, `git add -- <path> ...`. Never use
  `git add -A` or `git add .` on the whole tree.
- Never stage `LICENSE` or scratch files.
- Code compiled for one platform only can't be verified locally on another.
  Say so when handing it back.
