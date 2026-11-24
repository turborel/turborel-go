# turborel-go rules

How turborel-go is designed, written and verified. Every change follows
these rules; `go run ./internal/xtask verify` checks those a tool can check, and
review checks the rest.

Rules (R-…) say how code and tests are written. **shall** is mandatory,
and a departure needs a decision recorded here (§7). **should** is
expected, and a reviewer may accept a reasoned exception.

Decisions (D1…) record how the driver is built and why. Change one only
by changing this file in a reviewed commit. A decision says what was
chosen and why, and the module it names holds the mechanism.

Cite both by ID in code, tests, reviews and commit messages, as in
`RULES.md D6` or `R-ERR-2`.

The rules are modelled on Turborel's own
[RULES.md](https://github.com/turborel/turborel/blob/main/RULES.md),
adapted to drivers. Every Turborel driver keeps the same requirement IDs
with the same meaning, and the same decisions D1 to D8, so a rule is
cited the same way in every repository; a driver's own decisions begin
at D9, and its own requirements are in families of their own. Where
Go's library conventions and a rule here disagree, the rule is
kept, and D8 lists where.

Before changing code, read §3 (code rules), §4 (test rules) and the
decisions covering your area. What the driver implements is in
[CONFORMANCE.md](CONFORMANCE.md), and open work is in [TODO.md](TODO.md).

[1 Decisions](#1-decisions) · [2 Layout](#2-project-layout) ·
[3 Code](#3-code-rules) · [4 Tests](#4-test-rules) ·
[5 Coverage](#5-coverage) · [6 Process](#6-process-and-tools) ·
[7 The decisions](#7-the-decisions) · [8 References](#8-references)

---

## 1. Decisions

Each is told in full in [§7](#7-the-decisions).

| # | Decision |
|---|---|
| **Shared by every Turborel driver** | |
| D1 | The native protocol, version 1, in layers that follow turborel-client |
| D2 | The language's standard API, over a low-level client of turborel-client's features |
| D3 | The runtime minimum follows the ecosystem's own support |
| D4 | One command runs every check, before each commit and in CI on three platforms |
| D5 | The version is the Turborel release's: 0.0.0 in the alpha, speaking protocol 1 |
| D6 | Tests and code cite Turborel's documents by their headings |
| D7 | A coverage floor, enforced in CI, that only rises |
| D8 | Where these rules depart from Go's conventions |
| **This driver's own** | |
| D9 | One package, `turborel` |
| D10 | The standard library alone |
| D11 | Strict lints, tools pinned by version |
| D12 | The checks tool is Go, in `internal/xtask` |
| D13 | Go's `testing` package and its fuzzing |
| D14 | The live tests' server, from the environment |
| D15 | The sample frames, a fixture copied at a pinned commit |

---

## 2. Project layout

| Path | What |
|---|---|
| `go.mod` | the module, `github.com/turborel/turborel-go`, `go 1.26`, no requirement |
| `doc.go` | the package's documentation |
| `*.go` at the root | the package `turborel`, one file or more for each layer of D1, named for its area (D9): `frame.go` of the codec, and the rest as they come |
| `*_test.go` at the root | each file's tests, beside it, in the package itself |
| `version.go` | `Version`, the Turborel release the driver is made for (D5) |
| `internal/xtask/` | the checks tool (D12) |
| `internal/testserver/` | the live tests' server (D14) |
| `testdata/` | Turborel's sample frames, copied at the pinned commit (D15) |
| `.golangci.yml` | golangci-lint's configuration (D11) |
| `doc/` | the manual (R-DOC-7) |
| `.github/` | CI, issue forms, the pull request template, labels, Dependabot |
| `.githooks/pre-commit` | hands over to the checks tool (D4) |

---

## 3. Code rules

The tools named by "the formatter", "the linters", "the type checker" and
"the checks tool" are D4's.

### R-STYLE: formatting and idiom

| ID | Rule | Checked by |
|---|---|---|
| R-STYLE-1 | Code **shall** be formatted by the formatter. | the formatter |
| R-STYLE-2 | Names **shall** follow the language's own conventions; code implementing a message, field or setting is named after it, as the spec names it. | the linters; review |
| R-STYLE-3 | Library code **shall not** print, or write to a stream it was not given. | the linters or the checks tool |
| R-STYLE-4 | What is not public **shall** be private, by the language's own means, so the public API is what is documented. | review |

### R-SIZE: short functions and files

| ID | Rule | Checked by |
|---|---|---|
| R-SIZE-1 | A function **shall** have at most 50 lines of code, blank lines and comments not counted. | the linters or the checks tool |
| R-SIZE-2 | A source file **shall** have at most 600 lines; split by the spec's sections or by layer. | the linters or the checks tool |
| R-SIZE-3 | A function **shall** take at most 5 parameters; else a structure or an options object. | the linters |
| R-SIZE-4 | A module **should** hold one concept, stated in its doc comment. | review |
| R-SIZE-5 | A function's complexity **shall** be at most 15, and its blocks nested at most 5 deep. | the linters |

### R-DOC: comments and documentation

| ID | Rule | Checked by |
|---|---|---|
| R-DOC-1 | Every public item **shall** have a doc comment in the language's documentation format, saying each parameter, what it returns, and each failure it gives. | the linters; review |
| R-DOC-2 | Comments **shall** say *why*: the section implemented, the choice made, the invariant relied on. | review |
| R-DOC-3 | The API reference, where the language's tools build one, **shall** build without warnings. | the checks tool |
| R-DOC-4 | Behaviour the protocol or turborel-client governs **shall** cite it by its heading, `[PROTOCOL Frames]`, `[CLIENT Pools]`; the driver's own choices cite their decision (D6). | review; the checks tool |
| R-DOC-5 | No commented-out code, and no `TODO`, `FIXME` or `XXX` comments: open work goes in TODO.md, and code never cites TODO.md. | the linters or the checks tool |
| R-DOC-6 | Every source file **shall** begin with a doc comment giving its responsibility and the sections it implements. | review |
| R-DOC-7 | A change a program can see **shall** update the manual, `doc/`, in the same commit; every claim is checked against the code, and every example is run. The manual keeps Turborel's layout, which turborel.com's documentation site reads: each page opens with its `# Title` and a line holding only `[Contents](README.md)`, each volume's README.md lists its pages, links within `doc/` and to other files of this repository are relative links that resolve (the site points the latter at GitHub, at the same tag), a link to Turborel's own manual is its GitHub address under `https://github.com/turborel/turborel/blob/main/doc/`, `user/01-installing.md` keeps its name, and `doc/` holds no images. | review; the checks tool |

### R-CONST: no magic numbers or strings

| ID | Rule | Checked by |
|---|---|---|
| R-CONST-1 | Numbers in library code other than `0` or `1` **shall** be named constants, documenting the value's source. | the linters; review |
| R-CONST-2 | Every protocol tag **shall** be a named constant, defined once: a message's kind, a data type's and a value's tag, a tail tag, a capability's name, a mechanism's name, a status name, and each SQLSTATE the driver raises. | review |
| R-CONST-3 | Strings matched or looked up, such as settings' names, URI parts and environment variables, **shall** be named constants. Messages for people are not. | review |
| R-CONST-4 | Related constants **shall** live together in their layer's module, under a comment naming the section they come from. | review |
| R-CONST-5 | Tests may use literal fixtures, as the spec prints its bytes; an expected value restating a constant **should** use it. | review |

### R-ERR: errors

| ID | Rule | Checked by |
|---|---|---|
| R-ERR-1 | Every failure the library gives **shall** be the driver's error (D2): no stray exception, panic, abort, assertion or other error of the language or its libraries reaches a program, and no library code ends the process. | the linters; tests; review |
| R-ERR-2 | Each failure **shall** carry its SQLCODE, SQLSTATE and message, and, once read from a server, the rest of it: the rule it cites, its position, whether a transaction is still active, and its diagnostics. A failure the driver raises itself carries the SQLCODE and SQLSTATE turborel-client gives the same failure. | tests |
| R-ERR-3 | No failure **shall** be silenced. One caught and given again as the driver's keeps its cause; a result left unread says why it cannot matter. | the linters; review |
| R-ERR-4 | A connection that fails **shall** be closed, and every request waiting on it given that failure; a failed call leaks nothing and leaves nothing waiting. | tests; review |

### R-NUM: integers and numbers

| ID | Rule | Checked by |
|---|---|---|
| R-NUM-1 | Every integer of the protocol **shall** be read and written at its width, little-endian, explicitly: never by the platform's byte order, by casting a buffer, or by bitwise operators on numbers wider than the language's operators keep exact. | review; tests |
| R-NUM-2 | A value out of range for its field, or for the type it is read into, **shall** be refused with the driver's error, never truncated, wrapped or rounded. | tests |
| R-NUM-3 | An exact number (units `i128`, scale `u32`) and a DECFLOAT **shall** never pass through a binary floating-point type. | review; tests |
| R-NUM-4 | An approximate number **shall** keep its bits: NaN and -0.0 are read and written as the double they are, and floats are compared bit for bit, never for equality. | tests; review |
| R-NUM-5 | In a language whose integers are bounded, arithmetic on sizes, counts and offsets **shall** be shown not to overflow before it is done. | the linters; review |

### R-LINT: warnings

| ID | Rule | Checked by |
|---|---|---|
| R-LINT-1 | A warning is an error: of the compiler, the linters and the type checker, and of the runtime itself while the tests run. | the checks tool |
| R-LINT-2 | The strictest standard linters and type checking the language has **shall** be on; a check left off says why in the configuration that leaves it off. | review |
| R-LINT-3 | A warning **shall** be silenced only on its line, naming its rule and saying why; a suppression no longer needed **shall** be removed. | the linters; review |

### R-DEP: dependencies

| ID | Rule | Checked by |
|---|---|---|
| R-DEP-1 | The library **shall** depend on nothing but the language's standard library and runtime, unless a decision here admits a dependency, with its reason, version, license and review. | review |
| R-DEP-2 | A dependency, of the library or of its tools, **shall** be production-grade by its makers' own claims: a stable release they support. | review |
| R-DEP-3 | Tools **shall** be pinned by version, with the ecosystem's lock file under version control where it has one; a change of a tool's version is a commit of its own. | review |

### R-SAFE: what a server sends, and secrets

| ID | Rule | Checked by |
|---|---|---|
| R-SAFE-1 | A length or count a server, or a file the driver reads, gives **shall** be checked against what follows and the limit taken before anything is held for it, so no reply takes the program's memory without bound. | tests; review |
| R-SAFE-2 | What a server sends **shall** never be run, loaded or deserialized as code. | the linters; review |
| R-SAFE-3 | TLS **shall** be used and verified as the settings ask, and a connection **shall not** fall back to less than its mode allows. | tests |
| R-SAFE-4 | A secret (a password, anything derived from one, a SCRAM key or proof, a cancel key) **shall** never be logged, put in a failure's message or an object's display form, or sent where it was refused; where the language allows, it is cleared from memory once used. | tests; review |
| R-SAFE-5 | No module-level mutable state: what lasts belongs to an object a program made, such as a connection, a pool or a key cache. | review |
| R-SAFE-6 | The library **shall** start no thread, timer or task but for an object a program made, and a closed connection, cursor or pool leaves none behind. | tests; review |

### R-PLAT: platforms

| ID | Rule | Checked by |
|---|---|---|
| R-PLAT-1 | The driver **shall** run on Linux, macOS and Windows, on every runtime D3 supports; CI checks each (D4). | CI |
| R-PLAT-2 | Code of one platform, such as a Unix socket or a named pipe, **shall** be chosen by the platform, and elsewhere refused with the driver's error, saying so; never silently something else. | tests; review |
| R-PLAT-3 | Paths **shall** be made by the language's path functions, and files kept with LF line endings (`.gitattributes`). | review |
| R-PLAT-4 | Code built for one platform only **shall** say, when handed back, that it was not run on the others. | review |

### R-GO: Go

| ID | Rule | Checked by |
|---|---|---|
| R-GO-1 | No `unsafe` and no cgo. | `depguard`; review |
| R-GO-2 | A file of one platform **shall** carry a build constraint, and the other platforms a file that refuses with the driver's error (R-PLAT-2). | review; CI |
| R-GO-3 | A function **shall** return a failure as an `error` whose dynamic type is `*Error`, never a typed nil in an `error`. | review; tests |
| R-GO-4 | A switch on one of the driver's enumerations **shall** name every value; a default does not stand for them. | `exhaustive` |

---

## 4. Test rules

### R-TEST: levels and layout

| ID | Rule | Checked by |
|---|---|---|
| R-TEST-1 | A test **shall** sit at the lowest level that shows its requirement: a codec test of bytes, a test of a layer over a stream the test controls, a test of the API, or a live test against a server. | review |
| R-TEST-2 | Tests **shall** derive from the spec and turborel-client, not from the code: normal cases, boundaries included, and robustness cases with the exact SQLCODE and SQLSTATE expected. | review |
| R-TEST-3 | A test's name **shall** say the behaviour checked. | review |
| R-TEST-4 | Test code may assert, print and use literal fixtures; every other rule applies. | the linters' configuration |
| R-TEST-5 | Tests **shall** be deterministic and independent: no clock, randomness, network beyond loopback, or order between them. A case generator uses a fixed seed. A live test runs against a server on loopback that its own set-up starts, on its own port and data directory. | review |
| R-TEST-6 | Every fixed defect **shall** get a regression test that fails without the fix, its doc comment saying so. | review |
| R-TEST-7 | A fixture or helper shared by tests **shall** be written once, in one place. Tests use the one framework D9 or later names, no other. | review |
| R-TEST-8 | Live tests **shall** run against `ghcr.io/turborel/turborel:nightly` in CI, set up by the repository's own server set-up, with test certificates, SCRAM users and SERVER.TRUST_LOCAL. | CI |
| R-TEST-9 | SCRAM **shall** be tested by RFC 7677's test vector, and settings by the URI, service file, password file and environment cases of turborel-client's settings tests, so every driver reads settings as turborel-client does. | review |

### R-TRACE: traceability

| ID | Rule | Checked by |
|---|---|---|
| R-TRACE-1 | Every test **shall** cite what it verifies, by its doc comment, beginning `Verifies:`, beginning with a tag of D6: `[PROTOCOL <heading>]`, `[CLIENT <heading>]`, `[DESIGN] D<n>` or `[TOOL] R-<id>`. | the checks tool |
| R-TRACE-2 | A citation **shall** name a heading, decision or rule that exists. | the checks tool |
| R-TRACE-3 | Code implementing a requirement **shall** cite it (R-DOC-4). | review |
| R-TRACE-4 | CONFORMANCE.md **shall** change with the change that implements or changes a row. A row is marked done only where the driver's code for it exists and tests citing its section pass, or, of a driver built over another, as CONFORMANCE.md says. | review |

---

## 5. Coverage

| ID | Rule | Checked by |
|---|---|---|
| R-COV-1 | Coverage **shall** be measured by `go run ./internal/xtask coverage`, over the library and every test. | the checks tool |
| R-COV-2 | Coverage **shall not** fall below the floor (D7); a change raising it **should** raise the floor. | the checks tool, in CI |
| R-COV-3 | A gap is closed by a test derived from a requirement, or by removing dead code; never by a test of no requirement, and never by excluding the code. | review |

---

## 6. Process and tools

| ID | Rule | Checked by |
|---|---|---|
| R-PROC-1 | `go run ./internal/xtask verify` **shall** pass before every commit; CI adds every platform, every supported runtime and the coverage floor (D4). | pre-commit hook; CI |
| R-PROC-2 | Every change **shall** be reviewed against the checklist below. | review |
| R-PROC-3 | Commits **should** be small and single-purpose, in CONTRIBUTING.md's form (`area: summary`, then why), citing the rules applied or changed. | review |
| R-PROC-4 | A threshold change **shall** change RULES.md and the configuration that holds it together. | review |
| R-PROC-5 | Project automation **shall** be written in the language's own tooling, never shell scripts; the git hook only hands over to the checks tool. | review |
| R-PROC-6 | CI's actions **shall** be pinned by their full commit SHA, with the version in a comment. | review |
| R-PROC-7 | A release **shall** be published only as VERSIONING.md says, by a maintainer (D5). | review |

The review checklist:

- the spec or turborel-client cited in code (R-DOC-4) and tests
  (R-TRACE-1);
- normal and robustness tests (R-TEST-2), and a failing-first test for a
  fix (R-TEST-6);
- every failure the driver's, carrying its condition (R-ERR);
- no magic numbers or strings, protocol tags named (R-CONST);
- every length a server sends bounded, no secret disclosed (R-SAFE);
- comments explain why (R-DOC-2);
- CONFORMANCE.md (R-TRACE-4), the manual (R-DOC-7) and TODO.md updated
  where they change.

---

## 7. The decisions

### Shared by every Turborel driver

#### D1. The native protocol, version 1, in layers that follow turborel-client

The driver speaks Turborel's native protocol, version 1, as
`doc/client/05-protocol.md` in Turborel's repository lays it out,
itself, in Go alone, on the standard library, with no cgo. Where that page and turborel-protocol's code differ,
the code is right, and the driver follows it; CONFORMANCE.md names each
such place.

The reference is turborel-client. The driver does all it does before it
is called done, row by row in CONFORMANCE.md, and is built in the same
layers as turborel-client's modules, so a reviewer checks one against
the other. Each layer is a commit area of CONTRIBUTING.md.

| Layer | What it covers | turborel-client |
|---|---|---|
| `codec` | the 5-byte frame header; fields; the tail and its tag order; the data types and values; JSON values; zlib `z` frames within the length bound; the 16 MiB body limit | `protocol/src` |
| `transport` | TCP on 7422; Unix sockets; the Windows named pipe `\\.\pipe\turborel`; TLS begun on the same port; the connect and read timeouts | `target.rs`, `timeouts.rs`, `pipe.rs` |
| `auth`, `tls` | trust, password, certificate, SCRAM-SHA-256 and -PLUS; secrets refused over plain TCP; the SCRAM key cache; TLS | `mechanism.rs`, `scram.rs`, `tls.rs` |
| `session` | Hello, Ready, Status, notices, conditions, diagnostics, autocommit, cancel, terminate, ping, reset, change-user, query attributes, multi-statement | `connection.rs`, `session.rs`, `cancel.rs`, `told.rs` |
| `statements` | queries and streamed queries; prepare, execute, fetch, close, deallocate; numbered cursors; describe; completions | `statements.rs`, `frames.rs` |
| `lobs`, `copy` | locators, their reads, writes and streams; copy, import and export | `lobs.rs`; `copy.rs`, `import/`, `export.rs` |
| `settings` | the URI, options, variables, service and password files, several hosts, roles, TLS modes, channel binding | `settings/`, `failover.rs`, `role.rs` |
| `pipeline`, `pool` | pipelines; pools; the SQL-agent | `pipeline.rs`; `pool/`, `agent.rs` |
| `api` | the standard API (D2) | the language's own |

Every layer is files of the one package `turborel` (D9), each named for
its area, so the codec is `frame.go` and the files beside it. TLS is
`crypto/tls`, whose `ExportKeyingMaterial` gives SCRAM-SHA-256-PLUS its
`tls-exporter` binding on TLS 1.3; SCRAM is `crypto/sha256`,
`crypto/hmac` and `crypto/pbkdf2`; compressed frames are `compress/zlib`;
exact numbers of 128 bits use `math/big`.

#### D2. The language's standard API, over a low-level client of turborel-client's features

The driver's standard interface is a `database/sql` driver, registered as `turborel`, and a native API beside it, cancelled through `context`, so a program, and the
tools built for Go's database interface, use it as they use
any other driver. It sits over a low-level client that has every feature
of turborel-client. What the standard interface has no place for, such
as pipelines, pools, copy, large objects by locator, notices and the
session's status, is reached through the low-level client or the
driver's own extensions of the standard interface.

Every failure is the driver's error, carrying its SQLCODE and SQLSTATE
(R-ERR). The standard interface is built at milestone M7 and checked in
M8's audit; until then its names may change.

The standard interface is a `database/sql/driver` driver, registered
under the name `turborel`, so a program opens a server with
`sql.Open("turborel", "turborel://...")`. Beside it, the native API
gives what `database/sql` has no place for. A
`context.Context` canceled while a statement runs cancels it on the
server, by the protocol's Cancel on a connection of its own, and the
call's failure has the context's error as its cause, so
`errors.Is(err, context.Canceled)` holds. A mechanism of the program's
whose own error is not the driver's has it given as a connection
failure caused by it.

The driver's error is `*Error`, returned as Go's `error` and found by
`errors.As`. An exact number is a `Decimal`, which holds every units and
scale the protocol carries and gives its units as a `*big.Int`.

#### D3. The runtime minimum follows the ecosystem's own support

The driver supports Go 1.26 or later. The rule is the ecosystem's own:
support every release its makers still support, and drop each at its
end of life. Where a major framework of the language sets a higher
floor, the driver matches it. The minimum changes in a commit of its
own, which changes this decision, the package's metadata, CI and
README.md together.

Go supports its two latest releases, and in November 2026 they are 1.26
and 1.27, so `go.mod` says `go 1.26` and CI tests both. When Go 1.28
comes out, 1.26 is dropped, as Go drops it.

#### D4. One command runs every check, before each commit and in CI on three platforms

`go run ./internal/xtask verify` runs every check a change must pass, stopping at the
first failure: the formatter, the linters, the type checker where the
language has one, the checks tool's own rules (lengths a linter does not
measure, citations and their targets, open-work markers, the manual's
titles, `[Contents](README.md)` lines and links), and every test.
`go run ./internal/xtask coverage` measures coverage and holds it to its floor (D7).

`.githooks/pre-commit` hands over to the same command. It is a script
only because git runs hooks as scripts. CI runs it on Linux, macOS and
Windows, over every runtime D3 supports, with the live tests against the
nightly image and coverage on Linux. Every action is pinned by its full
commit SHA, its version in a comment, and Dependabot proposes each bump.

The checks tool is `internal/xtask` (D12). Its `verify` runs, in order:
`gofmt -s -l`, `go mod tidy -diff`, `go vet`, golangci-lint with
`.golangci.yml` (D11), the citation and manual checks, then every test
under the race detector with the coverage floor. golangci-lint v2.15.0
and govulncheck v1.9.0 run by `go run` of their module at the version
`internal/xtask/verify.go` pins, so nothing needs installing but Go; CI
runs govulncheck too, by `go run ./internal/xtask vulncheck`.

#### D5. The version is the Turborel release's: 0.0.0 in the alpha, speaking protocol 1

The driver has no version scheme of its own. Its version is the number
of the Turborel release it is made for, the same as the server's: a
Turborel release and each driver's release for it share the number,
**MAJOR.MINOR.PATCH**, as Turborel's VERSIONING.md numbers it. The alpha
is 0.0.0, as Turborel's is, and speaks protocol version 1.

The number is written once, in the constant `turborel.Version`, in `version.go`. Nothing is
published, and no release is tagged, until every Turborel driver is
done; then the maintainers publish them together (VERSIONING.md).

#### D6. Tests and code cite Turborel's documents by their headings

A test cites what it verifies, and code what it implements, by the
heading of the section that says it, as printed:

- `[PROTOCOL <heading>]`, a heading of The Native Protocol,
  `doc/client/05-protocol.md`, as `[PROTOCOL Frames]`;
- `[CLIENT <heading>]`, a heading of The Client Library,
  `doc/client/02-library.md`, or of Settings, Pipelines and Pools,
  `doc/client/02-library-connections.md`, which share no heading, as
  `[CLIENT Pipelines]`;
- `[DESIGN] D<n>`, a decision of this file;
- `[TOOL] R-<id>`, a rule the checks tool holds, for the tool's own
  tests.

What else a test relies on follows the tag, as an RFC by its section:
`[PROTOCOL Mechanisms] [RFC 7677 §3]`. The heading `Lob 'L'` is printed
twice in The Native Protocol, under Requests and under Replies, so a
citation of it says which it means.

The checks tool keeps the headings of those three documents, copied at
the Turborel commit CONFORMANCE.md names, and refuses a citation of a
heading, decision or rule that is not there (R-TRACE-2). In Go,
a test cites by its doc comment, beginning `Verifies:`.

#### D7. A coverage floor, enforced in CI, that only rises

`go run ./internal/xtask coverage` measures the library's coverage over every test,
and CI fails below the floor. In every Turborel driver the floor is at
least 90 % of lines, and of branches where the tool measures them. It
only rises: a change that raises coverage well above it raises it. A
floor below 100 % says why.

The floor is 95 % of statements, `coverageFloor` in
`internal/xtask/coverage.go`; Go's coverage tool counts statements and
measures no branches. The checks tool's statements and the live
tests' server's are left out, as neither is part of the library (D12,
D14). It is below 100 % because code of one
platform, such as the Windows named pipe, runs only on its platform,
while coverage is measured on Linux.

#### D8. Where these rules depart from Go's conventions

The driver follows its language's library conventions for layout,
naming, packaging, documentation and testing. Where one conflicts with a
rule here, the rule is kept:

- Libraries commonly leave the length of functions and files to taste.
  These are bounded (R-SIZE).
- Libraries commonly fail an argument out of range with the language's
  own error. Here every failure is the driver's, an argument's too, with
  the SQLCODE and SQLSTATE turborel-client gives it (R-ERR-1), so a
  program handles one kind of failure.

- A Go library commonly lets a program's misuse panic. Here every
  failure is returned as an `error` whose dynamic type is `*Error`
  (R-ERR-1, R-GO-3).
- Go modules have no manifest of version and license. The module path is
  in `go.mod`, the version is `Version` (D5), and the license is
  `LICENSE`, which the module proxy ships with every version.
- Tests are in the package itself, not in a `_test` package, so the
  codec's unexported functions are tested directly (R-TEST-1, D13).

Where turborel-client is inconsistent, every Turborel driver follows one
rule, so all behave alike; each is a departure from that client:

- Value tags 18 and 19, the server's stored form of large objects, never
  sent on the wire, are refused as a value the driver cannot take,
  SQLCODE -916, SQLSTATE TB916: the request fails, the rest of its
  replies read past, and the connection goes on, as of any such value.
  turborel-protocol's reader takes them.
- Attributes past their bounds are refused before anything is sent,
  -919/08006, the connection kept. turborel-client sends them, and the
  server then fails the connection.


### This driver's own

#### D9. One package, `turborel`

The module `github.com/turborel/turborel-go` is the package `turborel`:
its codec, transport, authentication, session and APIs are files of that
one package, the codec's unexported. So every failure is the one `*Error`
type without an import cycle, and a program imports one path. Files stay
small by R-SIZE-2 and are
named for their area, as CONTRIBUTING.md's commit areas are, so the
globs of `.github/labeler.yml` label a pull request by the areas it
touches. `internal/` holds only the checks tool.

#### D10. The standard library alone

The driver imports nothing but Go's standard library (R-DEP-1), so every
line is reviewable and no supply chain needs qualifying. golangci-lint's
`depguard` refuses any other import.

#### D11. Strict lints, tools pinned by version

golangci-lint v2.15.0 runs with `.golangci.yml`, in which every linter is
on but those its `disable` lists: ones that fight Go's idiom or gofmt,
and ones deprecated for a successor that is on, each with its reason. A
finding fails the check; there is no baseline. An exception is
`//nolint:<linter> // <reason>`, naming one linter and saying why
(`nolintlint`, R-LINT-3).

golangci-lint and govulncheck (v1.9.0) run by `go run` of their module at
a pinned version, named once in `internal/xtask/verify.go`, so every
machine and CI run the same, and nothing needs installing but Go. They
are tools, not dependencies: they are in no `go.mod` of this repository,
and no program that imports the driver builds them. Dependabot cannot
see these pins, so they are raised by hand, each in a commit of its own
(R-DEP-3).

#### D12. The checks tool is Go, in `internal/xtask`

The project's checks are a Go program, run as `go run ./internal/xtask
<command>`: `verify`, `coverage`, `vulncheck` and `install-hooks`. It
uses only the standard library and runs the same on Linux, macOS and
Windows, so no shell script or Makefile is needed (R-PROC-5). Besides
running the tools, it holds the rules no linter measures: every test's
citation and its target (D6, R-TRACE-1, R-TRACE-2), no comment citing
TODO.md (R-DOC-5), and the manual's titles, `[Contents](README.md)`
lines, contents pages and links (R-DOC-7). It keeps the headings of
Turborel's three documents in `internal/xtask/headings.go`, at the
commit CONFORMANCE.md names.

#### D13. Go's `testing` package and its fuzzing

Tests use Go's `testing` package and nothing else (R-TEST-7), in
`_test.go` files beside the code they test. A decoder of what a server
sends has a fuzz test of Go's own, whose seeds run with every `go test`.

#### D14. The live tests' server, from the environment

The live tests run every request and capability against a real server,
opt-in by the environment's TURBOREL_TEST_SERVER, so the unit tests stay
deterministic (R-TEST-5): unset, they skip. It names a `turboreld`
program, or `docker:` and an image, as
`docker:ghcr.io/turborel/turborel:nightly`, run with the host's network
so its clients are of its own machine. `internal/testserver` starts it,
once for the test binary, on turborel-go's own port, 17433, with a Unix
socket in a directory of its own, TLS by an authority, server and client
certificates made as it starts, so no secret is ever committed, a client
authority, SERVER.TRUST_LOCAL on, and an owner of a password; and stops
it, waiting for its own process alone. CI sets the image on Linux; the
image is for Linux alone, and macOS and Windows runners have no
`turboreld`, so there the live tests skip. The package is test support,
not part of the library, so its statements, like xtask's, are left out of
coverage (D7).

#### D15. The sample frames, a fixture copied at a pinned commit

`testdata/05-protocol-frames.tsv` is Turborel's
`doc/client/05-protocol-frames.tsv` copied byte for byte at 5863fe65,
the commit CONFORMANCE.md names, and moves with that pin. A copy, rather
than a read of a Turborel checkout in a test skipped where there is
none, runs wherever the unit tests run, on every platform, and gives the
same result each time (R-TEST-5). The tests read each server frame from
a stream as a connection does, inflated if compressed, and compare its
reply with the one its meaning gives, written in the test. They encode
each client frame by the driver's own requests and compare it byte for
byte. The one compressed client frame is compared by what it holds, its
kind, its length and the frame it inflates to, since its zlib stream is
the compressor's choice: Go's `compress/zlib` writes other bytes than
turborel's own deflate, and the server inflates either alike
[PROTOCOL Compressed frames].
---

## 8. References

- Turborel, `doc/client/05-protocol.md`, *The Native Protocol*,
  version 1: the requirements of the codec and every message.
- Turborel, `doc/client/02-library.md`, *The Client Library*, and
  `doc/client/02-library-connections.md`, *Settings, Pipelines and
  Pools*, and the crates turborel-client and turborel-protocol: the
  features to match, and the reference where the spec is silent or
  differs.
- Turborel, `RULES.md` D89, the JSON value's binary tree, and D184, the
  UUID's tag.
- RFC 1950, *ZLIB Compressed Data Format Specification*; RFC 5802 and
  RFC 7677, SCRAM and SCRAM-SHA-256; RFC 5929 and RFC 9266, the
  `tls-server-end-point` and `tls-exporter` channel bindings.
- The standards of Go and of its database interface, named in
  D2 and D3.
