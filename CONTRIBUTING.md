# Contributing to turborel-go

turborel-go carries a program's data to and from a turborel server, and
its passwords and keys with it. So a change is judged first on whether
every value arrives as it was sent, every failure is told, and no secret
leaks, and then on what it adds. The process follows the Linux kernel's,
adapted to pull requests and to a database driver.

[Before you start](#before-you-start) · [Setting up](#setting-up) ·
[Making a change](#making-a-change) ·
[Changes that touch the wire](#changes-that-touch-the-wire) ·
[Commit messages](#commit-messages) · [Pull requests](#pull-requests) ·
[Review](#review) · [Reporting issues](#reporting-issues) ·
[Security](#security) · [License](#license)

---

## Before you start

Read [RULES.md](RULES.md) §3 (code), §4 (tests), §6 (process) and the
decisions for your area. Every rule applies to every change, and
`go run ./internal/xtask verify` checks those a tool can.
[CONFORMANCE.md](CONFORMANCE.md) says what is implemented of the protocol
and of turborel-client, and [TODO.md](TODO.md) lists open work.

Open an issue first for anything larger than a fix. That covers new
features, changes to a decision in RULES.md, new dependencies (D7) and
changes to the public API. Agreeing on the design first saves everyone a
rewrite.

The protocol starts from its spec, The Native Protocol
(`doc/client/05-protocol.md` in turborel's repository), and from
turborel-client, the reference client (`client/src/` and
`protocol/src/`). Where they differ the reference client is right. Read
the section before writing code, and cite it by its heading, as in
`[PROTOCOL Frames]`. Never cite from memory.

## Setting up

Go 1.26 or later, and git, are all that is needed.

```
go run ./internal/xtask install-hooks   # run verify before each commit (once per clone)
go run ./internal/xtask verify          # every check a change must pass
go run ./internal/xtask coverage        # the tests, coverage and its floor alone
go test -race ./...                     # the tests alone
```

`verify` runs `gofmt`, `go mod tidy -diff`, `go vet`, golangci-lint, the
citation check, and every test under the race detector with the coverage
floor. golangci-lint runs by `go run` at the version RULES.md D6 pins, so
nothing needs installing. CI runs `verify` on Linux, macOS and Windows,
on Go 1.26 and 1.27, and govulncheck (D9). A change that passes on one
platform and fails on another isn't done.

## Making a change

Keep one logical change per commit, and make each commit pass `verify`
on its own, because anyone bisecting may land on it. Move code in one
commit and change it in another. Don't mix a reformat or rename with a
change of behaviour.

Tests come with the change, at the lowest level that shows it (R-TEST-1):
codec tests against the bytes the spec gives, live tests against a
server on loopback. Cover normal cases, boundaries, and robustness cases
with the exact SQLCODE and SQLSTATE expected (R-TEST-2). Each test's doc
comment has a paragraph beginning `Verifies:` and its citation
(R-TRACE-1). A fixed defect gets a test that fails without the fix
(R-TEST-5).

Documents change in the same commit. Update CONFORMANCE.md for a row
implemented (R-TRACE-3), RULES.md for a decision made or changed, and
TODO.md for open work done or found. Code never cites TODO.md. A change
a program sees updates the manual, [doc/](doc/README.md), its examples
run (R-DOC-6): the User's Guide chapter and the API Reference page that
state it, and the appendix row of a type, an error or a setting. The
manual is laid out as turborel's, since turborel's docs site reads it:
each page opens with its `# Title` and a line holding only
`[Contents](README.md)`, and links between pages are relative `.md`
links.

Some rules deserve a reminder.

- No `unsafe` or cgo (R-SAFE-1), no panics (R-ERR-2), and no dependency
  without a decision in RULES.md (D7).
- Every failure is an `*Error` (R-ERR-1).
- No length or count from the server is trusted before it is checked
  (R-ERR-4).
- The coverage floor only rises (D10).

## Changes that touch the wire

These need more than passing tests. Say in the commit message what was
done for each that applies.

For the codec, show the bytes: a test of the message as the spec lays it
out, and of each malformed form that must fail the connection (SQLCODE
-919, SQLSTATE 08006).

For authentication and TLS, say what a server in the middle, or one
overhearing, could do before and after the change, and show that a
secret is never sent where it was refused.

For anything the server answers, run it against a server built from
turborel's main, and name the commit.

## Commit messages

The form is Linux's.

```
area: summary in the imperative, at most 75 characters

The problem: what goes wrong, or is missing, and who it affects. A
wrong value, a failure not told, a hang or a secret disclosed is said
first, with the call that shows it.

Why this change, and how it solves the problem. Mention the choices
made and the alternatives rejected, the rules and sections applied
(RULES.md D2, R-ERR-4, [PROTOCOL Frames]), and what was measured, with
numbers.

Fixes: 54a4f0239f2e ("codec: refuse a flag other than 0 or 1")
Closes: <link to the issue>
Reported-by: A Reporter <reporter@example.org>
```

The subject is `area: summary`, at most 75 characters, in the imperative
("refuse a frame...", not "refused"), with no full stop. It says what
changes, isn't a file name, and isn't reused within a series.

The body is wrapped at 75 columns and written for someone reading `git
log` years later with no context. Say what the change does in plain
prose, since the diff shows how. Don't write "as discussed" or "v2".

Pick the area where most of the change is.

| Area | Covers |
|---|---|
| `codec` | frames, fields, the tail, data types and values, compression |
| `transport` | TCP, the Unix socket, the named pipe, timeouts |
| `auth` | mechanisms, SCRAM and its key cache |
| `tls` | TLS, its modes, channel binding |
| `session` | Hello, Ready, status, notices, cancel, terminate, ping, reset, change-user |
| `statements` | query, prepare, execute, cursors, describe, completions |
| `lobs` | large objects by locator |
| `copy` | copy, import and export |
| `settings` | the URI, options, variables, service and password files, several hosts |
| `pipeline` | pipelines |
| `pool` | pools |
| `api` | the `database/sql` driver and the native API |
| `xtask` | the project's checks, `internal/xtask/` |
| `doc` | `doc/`, README.md, RULES.md, CONFORMANCE.md, TODO.md, AGENTS.md, CLAUDE.md, this file |
| `ci` | `.github/`, `.githooks/` |
| `build` | `go.mod`, `.golangci.yml`, `.gitattributes`, `.gitignore` |

Trailers go at the end, one per line, never wrapped.

| Trailer | Meaning |
|---|---|
| `Fixes: <12+ hex digits> ("<subject>")` | the commit that introduced the defect fixed |
| `Closes: <link>` | the issue this commit resolves |
| `Link: <link>` | background, such as a discussion or an issue fixed only in part |
| `Reported-by:` | who reported the defect fixed, with `Closes:` |
| `Suggested-by:` | who suggested the idea |
| `Co-authored-by:` | a co-author, as GitHub credits one |
| `Reviewed-by:` | a reviewer who checked the change and is satisfied with it |
| `Acked-by:` | a maintainer of the area who approves it without a full review |
| `Tested-by:` | someone who tested it, saying where if that matters |

`Reported-by:` and `Suggested-by:` may be added without asking. The others
name someone who said so on the record. `git config core.abbrev 12` gives
12-digit hashes, and `git log -1 --pretty='Fixes: %h ("%s")' <commit>` writes
the `Fixes:` line.

No commit carries attribution to an AI or agent: no `Co-authored-by:` for
one, no "Generated with", and no `Signed-off-by:`. Whoever submits a
change answers for it.

Stage a commit by an explicit list of paths, `git add -- <path> ...`,
never `git add -A` or `git add .` on the whole tree, so nothing
unintended rides along.

## Pull requests

A pull request is a patch series, and its description is the cover letter.
The template asks for what and why, the series commit by commit, testing,
compatibility and performance. Each commit message still stands alone.

Base it on `main` and keep it rebased, without merging `main` into it. When
revising, rewrite the commits so the series is again what should be merged,
with no "fix review comments" commits. Force-push, and say in a comment what
changed since the last version.

Trailers given in review go into the commits they cover. Drop them from a
commit that later changes substantially, and say so.

Keep a series to about 15 commits. Split a larger one into series that each
stand alone.

## Review

Reviewers check each commit against RULES.md §6's checklist, and that the
manual is updated (R-DOC-6). For
[changes that touch the wire](#changes-that-touch-the-wire), they also
check the bytes, the refusals and the secrets. Review comments are about
the change, never the person. A reviewer who is satisfied gives
`Reviewed-by:`.

## Reporting issues

Use the issue forms, **Bug**, **Conformance**, **Performance** or
**Feature**. Try the latest `main` first, and add to an existing issue that
matches rather than opening another.

Reproduce with the smallest program that shows it, and the statements
that set up the database it runs against. A failure of the server
itself, which turborel-client shows too, is reported to
[turborel](https://github.com/turborel/turborel/issues).

State the impact first. Put it in the title's first word when it is severe.

| Tag | Meaning |
|---|---|
| `[WRONG VALUE]` | a value read or sent is not the one the server holds or was given |
| `[CRASH]` | a panic or a hang |
| `[REGRESSION]` | worked in an earlier commit (give the last good commit and, if you can, the first bad one) |

Give the commit (`git rev-parse --short=12 HEAD`), the Go version
(`go version`), the platform and architecture, the server's commit or
release, and the connection's settings, a password left out.

## Security

Don't open a public issue for a vulnerability. Report it privately, as
[SECURITY.md](SECURITY.md) says.

## License

turborel-go is under the BSD-2-Clause Plus Patent License in
[LICENSE](LICENSE), and a contribution is made under that license.
Contributors keep their copyright and own their changes. Whoever
submits a change answers for all of it, however it was written.
