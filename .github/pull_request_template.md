<!--
This description is the series' cover letter (CONTRIBUTING.md, Pull
requests). Each commit message still stands alone, in Linux's form.
Base the series on main, keep it rebased, and on revising say what
changed in a comment, not here.
-->

## What and why

<!-- The problem and its impact, then the approach and the choices made.
Link the issue: "Closes #N". -->

## The series

<!-- One line per commit, in order: what it does. Each passes
`go run ./internal/xtask verify` on its own. -->

1.

## Testing

<!-- The tests added and what each verifies. For a change that touches the
wire, the bytes checked, the malformed forms refused, and the server
commit run against. -->

- [ ] `go run ./internal/xtask verify` passes

## Compatibility

<!-- What a program using the driver sees change, and what a server of an
earlier commit sees; "none" if nothing changes. -->

## Performance

<!-- If the change claims any: `go test -bench NAME -count 10` before and
after, compared by `benchstat`, with the numbers. Otherwise "none
claimed". -->

## Checklist (RULES.md §6)

- [ ] One logical change per commit, each passing `go run ./internal/xtask verify`
- [ ] The spec cited in the code (R-DOC-4) and in the tests (`Verifies:`, R-TRACE-1)
- [ ] Normal and robustness tests (R-TEST-2), a failing-first test for a fix (R-TEST-6)
- [ ] Every failure an `*Error` (R-ERR-1), no server length trusted (R-SAFE-1), no secret disclosed (R-SAFE-4)
- [ ] No `unsafe`, no panics, no new dependency without a decision (R-GO-1, R-ERR-1, R-DEP-1)
- [ ] CONFORMANCE.md, RULES.md, TODO.md and `doc/` updated where they change
