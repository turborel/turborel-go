# turborel-go rules

How turborel-go is designed, written and verified. Every change follows
these rules; `go run ./internal/xtask verify` checks those a tool can
check, and review checks the rest.

Rules (R-…) say how code and tests are written. **shall** is mandatory,
and a departure needs a decision recorded here. **should** is expected,
and a reviewer may accept a reasoned exception.

Decisions (D1…) record how the driver is built and why. Change one only
by changing this file in a reviewed commit. A decision says what was
chosen and why, and the file it names holds the mechanism.

Cite both by ID in code, tests, reviews and commit messages, as in
`RULES.md D2` or `R-ERR-1`.

Before changing code, read §3 (code rules), §4 (test rules) and the
decisions covering your area. What the driver implements of the protocol
and of turborel-client is in [CONFORMANCE.md](CONFORMANCE.md), and open
work is in [TODO.md](TODO.md).

[1 Decisions](#1-decisions) · [2 Layout](#2-project-layout) ·
[3 Code](#3-code-rules) · [4 Tests](#4-test-rules) ·
[5 Coverage](#5-coverage) · [6 Process](#6-process-and-tools)

---

## 1. Decisions

| # | Decision |
|---|---|
| **The driver** | |
| D1 | The native protocol, version 1, in Go alone |
| D2 | database/sql, and a native API beside it |
| D3 | Go 1.26 and later |
| D4 | One package, `turborel` |
| D8 | The version is turborel's release |
| **Tooling and process** | |
| D5 | Automation in Go, in `internal/xtask` |
| D6 | Strict lints, tools pinned by version |
| D7 | The standard library alone |
| D9 | `verify` in a pre-commit hook and in CI, on three platforms |
| D10 | A coverage floor that only rises |
| D11 | Where Go's conventions and turborel's rules meet |

### The driver

#### D1. The native protocol, version 1, in Go alone

The driver speaks turborel's native protocol, version 1, itself, as The
Native Protocol (`doc/client/05-protocol.md` in turborel's repository)
describes it. It has no native core and uses no cgo: it is plain Go, so
`go get` installs it on every platform Go builds for, and cross-compiles
with no C toolchain. Where the spec and turborel-client, the reference
client in Rust (`client/src/` and `protocol/src/`), differ, the
reference client is right, and the difference is reported to turborel.

The driver matches every feature of turborel-client before it is called
done: the parity matrix in CONFORMANCE.md lists them.

#### D2. database/sql, and a native API beside it

The standard interface is a `database/sql/driver` driver, registered
under the name `turborel`, so a program opens a server with
`sql.Open("turborel", "turborel://...")`. Beside it, as pgx does for
PostgreSQL, a native API gives what `database/sql` cannot carry: notices,
conditions and diagnostics, large-object streams, copy, export,
pipelines, settings told by the server, and the SQL-agent.

A `context.Context` canceled while a statement runs cancels it on the
server, by the protocol's Cancel request on a connection of its own,
with the key the session's Ready gave. The canceled call returns the
statement's failure (SQLSTATE TB920) wrapping the context's error, so
`errors.Is(err, context.Canceled)` holds.

The standard API is built last, at milestone M7, over the native one.

#### D3. Go 1.26 and later

`go.mod` says `go 1.26`. Go supports its two latest releases, and in
November 2026 they are 1.26 and 1.27, so the driver supports both and is
tested on both. When a Go release comes out, the oldest is dropped as Go
drops it, in a commit of its own that changes `go.mod`, CI's matrix and
this decision together. Everything the protocol needs is in the
standard library of 1.26: `crypto/tls` with `ExportKeyingMaterial` for
`tls-exporter`, `crypto/sha256`, `crypto/hmac` and `crypto/pbkdf2` for
SCRAM, `compress/zlib` and `math/big` for exact numbers of 128 bits.

#### D4. One package, `turborel`

The module `github.com/turborel/turborel-go` is the package `turborel`:
its codec, transport, authentication, session and APIs are files of
that one package, the codec's unexported. So every failure is the one
`*Error` type without an import cycle, and a program imports one path,
as it does for lib/pq and go-sql-driver/mysql. Files stay small by
R-SIZE-2, and are named for their area, as CONTRIBUTING.md's commit
areas are: `frame.go`, then later `field.go`, `tail.go` and so on, so
the globs of `.github/labeler.yml` label a pull request by the areas it
touches. `internal/` holds only the project's tools.

#### D8. The version is turborel's release

The driver has no version scheme of its own. Each release of it carries
the number of the turborel release it is made for, the server's own, so
a turborel release and each driver's release share the number. The
alpha is 0.0.0, as turborel's is, and speaks protocol version 1. The
number is the package's `Version` and, once published, the module's git
tag (`v0.0.0`). See [VERSIONING.md](VERSIONING.md).

### Tooling and process

#### D5. Automation in Go, in `internal/xtask`

The project's checks are a Go program, run as `go run ./internal/xtask
<command>`: `verify`, `coverage`, `vulncheck` and `install-hooks`. It
uses only the standard library and runs the same on Linux, macOS and
Windows, so no shell script or Makefile is needed. `.githooks/pre-commit`
only hands over to it, because git runs hooks as scripts.

#### D6. Strict lints, tools pinned by version

`verify` runs `gofmt -s`, `go vet`, and golangci-lint v2.15.0 with
`.golangci.yml`, in which every linter is on but those its `disable`
lists: ones that fight Go's idiom or gofmt, and ones deprecated for a
successor that is on, each with its reason. A finding fails the check;
there is no baseline. Exceptions are `//nolint:<linter> // <reason>`,
naming one linter and saying why (`nolintlint`).

golangci-lint and govulncheck (v1.9.0) run by `go run` of their module
at a pinned version, named once in `internal/xtask/verify.go`, so every
machine and CI run the same, and nothing needs installing but Go. They
are tools, not dependencies: they are in no `go.mod` of this
repository, and no program that imports the driver builds them.

#### D7. The standard library alone

The driver imports nothing but Go's standard library (R-SAFE-2), so
every line is reviewable and no supply chain needs qualifying. A
dependency would need a decision here giving its reason, version,
license and review, and must be production-grade by its own
maintainers' claims. `depguard` refuses any other import.

#### D9. `verify` in a pre-commit hook and in CI, on three platforms

`verify` runs, in order: `gofmt -s -l`, `go mod tidy -diff`, `go vet`,
golangci-lint, the citation check (R-TRACE-1, R-TRACE-2, R-DOC-5), then
every test under the race detector with the coverage floor (R-COV-2).
`go run ./internal/xtask install-hooks` makes git run it before each
commit. CI (`.github/workflows/verify.yml`) runs it on Linux, macOS and
Windows, each on Go 1.26 and 1.27, the coverage report on Linux, and
govulncheck. Every action is pinned by its full commit SHA.

#### D10. A coverage floor that only rises

Coverage is the share of the driver's statements its tests run, by `go
test -covermode=atomic`, with xtask's own statements left out. The floor
is 90 %, in `internal/xtask/coverage.go`. It only rises: a change that
raises coverage well above it should raise it.

#### D11. Where Go's conventions and turborel's rules meet

Go's conventions are followed for layout, naming, docs and tests, with
these choices where they meet turborel's rules:

- A failure is returned as Go's `error`, never panicked, and its
  dynamic type is always `*Error` (R-ERR-1). A program tests it with
  `errors.As`.
- Go modules have no manifest of name, version and license. The module
  path is in `go.mod`; the version is `Version` and the git tag (D8);
  the license is `LICENSE`, which the module proxy ships with every
  version.
- Tests are Go's `testing` package, in `_test.go` files beside the code,
  in the package itself so the codec's unexported functions are tested
  directly (R-TEST-1). Each test's doc comment carries the citation
  turborel's tests carry in `/// Verifies:` (R-TRACE-1).

---

## 2. Project layout

```
go.mod                  the module, github.com/turborel/turborel-go
doc.go                  the package's documentation
error.go                Error, every failure the driver returns
frame.go                frame headers, encoded and decoded
version.go              Version, the turborel release it is made for
*_test.go               each file's tests, beside it
.golangci.yml           golangci-lint's configuration (D6)
internal/xtask/         the project's checks (D5)
doc/                    the manual, laid out as turborel's (R-DOC-6)
  README.md             the documentation set
  user/                 Volume I, the User's Guide
  client/               Volume II, the API Reference
  appendix/             the Appendices
.github/                CI (workflows/), issue forms, pull request
                        template, labels, Dependabot
.githooks/pre-commit    runs verify before each commit (D9)
README.md               what the driver is; how to build and test it
RULES.md                this file
CONFORMANCE.md          the parity matrix: protocol and turborel-client
TODO.md                 open work, by milestone
VERSIONING.md           the version, and how a release is published
CONTRIBUTING.md         changes, commit messages, pull requests, issues
SECURITY.md             how to report a vulnerability
AGENTS.md               the guide for coding agents
CLAUDE.md               points Claude Code to AGENTS.md
```

---

## 3. Code rules

### R-STYLE: formatting and idiom

| ID | Rule | Checked by |
|---|---|---|
| R-STYLE-1 | Code **shall** be formatted by `gofmt -s`, imports by `goimports`. | `gofmt -l`; golangci-lint |
| R-STYLE-2 | Code **shall** pass `go vet` and golangci-lint with `.golangci.yml`, every finding an error. | `go vet`; golangci-lint |
| R-STYLE-3 | Names **shall** follow Effective Go and the Go Code Review Comments: MixedCaps, initialisms in one case (`SQLState`, `URI`). | revive; review |
| R-STYLE-4 | Library code **shall not** print or log. | `forbidigo` |

### R-SIZE: short functions and files

| ID | Rule | Checked by |
|---|---|---|
| R-SIZE-1 | A function **shall** have at most 50 lines of code. | `funlen`; revive `function-length` |
| R-SIZE-2 | A source file **shall** have at most 600 lines; split by the spec's sections or by layer. | revive `file-length-limit` |
| R-SIZE-3 | A function **shall** take at most 5 parameters; else use a struct. | revive `argument-limit` |
| R-SIZE-4 | A file **should** hold one concept, said in its first comment. | review |

### R-DOC: comments and documentation

| ID | Rule | Checked by |
|---|---|---|
| R-DOC-1 | Every exported item **shall** have a doc comment in Go's form, beginning with its name. | revive `exported`; review |
| R-DOC-2 | Comments **shall** say *why*: the rule implemented, the choice made, the invariant relied on. | review |
| R-DOC-3 | Behaviour the spec governs **shall** cite it by its heading: `[PROTOCOL Frames]` for The Native Protocol, `[CLIENT Pipelines]` for The Client Library or Settings, Pipelines and Pools; choices of the driver's own cite `RULES.md D<n>`. | citation check; review |
| R-DOC-4 | Every file **shall** begin, after its package clause, with a comment of its responsibility and the sections it implements; `doc.go` holds the package's documentation. | review |
| R-DOC-5 | No commented-out code, no `TODO` or `FIXME` comments: open work goes in TODO.md, and code never cites TODO.md. | `godox`; citation check; review |
| R-DOC-6 | A change visible to a program **shall** update the manual, `doc/`, in the same commit, its examples run. The manual is laid out as turborel's, since turborel's docs site reads it: each page opens with its `# Title` and a line holding only `[Contents](README.md)`, and links between pages are relative `.md` links that resolve. | review |

### R-CONST: no magic numbers or strings

| ID | Rule | Checked by |
|---|---|---|
| R-CONST-1 | Numbers other than 0 and 1 **shall** be named constants, documenting the value's source. | `mnd`; review |
| R-CONST-2 | Protocol tags, message kinds, capability, mechanism and setting names, and SQLSTATEs **shall** be named constants, defined once. | `goconst`; review |
| R-CONST-3 | Tests may use literal fixtures, such as a frame's bytes; an expected value restating a constant **should** use it. | review |

### R-ERR: errors

| ID | Rule | Checked by |
|---|---|---|
| R-ERR-1 | Every failure **shall** be returned as an `error` whose dynamic type is `*Error`, carrying SQLCODE, SQLSTATE, message and the rest of the failure. A failure the driver finds itself uses the SQLCODE and SQLSTATE the protocol gives for it. | tests; review |
| R-ERR-2 | Library code **shall not** panic, call `log.Fatal` or exit the process; a value that cannot be is an `*Error`. | `forbidigo`; revive `deep-exit` |
| R-ERR-3 | No error **shall** be discarded silently: `_ =` needs a comment saying why it cannot matter. | `errcheck`; review |
| R-ERR-4 | No reply **shall** make the driver hold memory without bound: every length and count from the server is checked against what remains before it is used to allocate. | tests; review |
| R-ERR-5 | Integer conversions **shall** be checked where they can overflow. | `gosec` G115; review |

### R-SAFE: language subset and resources

| ID | Rule | Checked by |
|---|---|---|
| R-SAFE-1 | No `unsafe`, no cgo. | `depguard`; review |
| R-SAFE-2 | No import outside the standard library without a decision (D7). | `depguard` |
| R-SAFE-3 | No global mutable state. | `gochecknoglobals` |
| R-SAFE-4 | A goroutine **shall** be started only where a decision says, and end with what started it. | review; race detector |
| R-SAFE-5 | Secrets (a password, SCRAM keys, the cancel key) **shall** never be written to an error's message, a log or a `String` method. | review; tests |

### R-PLAT: platforms

| ID | Rule | Checked by |
|---|---|---|
| R-PLAT-1 | The driver **shall** build and pass every test on Linux, macOS and Windows. | CI |
| R-PLAT-2 | Code of one platform, such as the Windows named pipe, **shall** sit behind a build constraint, and elsewhere fail with a clear `*Error`. | review; CI |

---

## 4. Test rules

### R-TEST: levels and layout

| ID | Rule | Checked by |
|---|---|---|
| R-TEST-1 | A test **shall** sit at the lowest level showing its requirement: codec tests against byte fixtures, live tests against a server on loopback. | review |
| R-TEST-2 | Tests **shall** derive from requirements, not code: normal cases with their boundaries, and robustness cases with the exact SQLCODE and SQLSTATE expected. | review |
| R-TEST-3 | Tests **shall** be deterministic and independent: no clock, randomness or network beyond loopback. | review |
| R-TEST-4 | Tests **shall** pass under the race detector, and **should** call `t.Parallel`. | `go test -race`; `paralleltest` |
| R-TEST-5 | Every fixed defect **shall** get a regression test that fails without the fix, its doc comment saying so. | review |
| R-TEST-6 | Tests use the `testing` package and no other framework; a helper shared by files is written once. | `depguard`; review |

### R-TRACE: traceability

| ID | Rule | Checked by |
|---|---|---|
| R-TRACE-1 | Every test, fuzz test, benchmark and example **shall** have a paragraph in its doc comment beginning `Verifies:` and a tag: `[PROTOCOL <heading>]`, `[CLIENT <heading>]`, `[DESIGN] D<n>`, or `[TOOL] R-<id>` for xtask's own. | citation check |
| R-TRACE-2 | A citation of the spec or the client's documents **shall** name a heading they have; the list is in `internal/xtask/headings.go`, at turborel's commit it names. | citation check |
| R-TRACE-3 | CONFORMANCE.md **shall** change with the change implementing a row of it. | review |

---

## 5. Coverage

| ID | Rule | Checked by |
|---|---|---|
| R-COV-1 | Coverage **shall** be measured by `go run ./internal/xtask coverage`, over the driver's statements. | xtask |
| R-COV-2 | Coverage **shall not** fall below the floor (D10). | xtask |

---

## 6. Process and tools

| ID | Rule | Checked by |
|---|---|---|
| R-PROC-1 | `go run ./internal/xtask verify` **shall** pass before every commit. | pre-commit hook; CI |
| R-PROC-2 | Every change **shall** be reviewed against the checklist below. | review |
| R-PROC-3 | Commits **should** be small and single-purpose, in CONTRIBUTING.md's form. | review |
| R-TOOL-1 | Project automation **shall** be Go in `internal/xtask`, never shell scripts (D5). | review |
| R-TOOL-2 | xtask **shall** follow these rules, use only the standard library, and test its own decisions. | verify |

The review checklist:

- the spec cited in code (R-DOC-3) and tests (R-TRACE-1);
- normal and robustness tests (R-TEST-2), a failing-first test for a fix
  (R-TEST-5);
- every failure an `*Error` (R-ERR-1), no length from the server trusted
  (R-ERR-4), no secret disclosed (R-SAFE-5);
- no magic numbers or strings (R-CONST);
- comments explain why (R-DOC-2);
- CONFORMANCE.md updated (R-TRACE-3), and TODO.md where open work
  changed.
