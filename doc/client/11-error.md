# The Error Type

[Contents](README.md)

Every failure the driver returns is an `error` whose dynamic type is
`*turborel.Error`, so a program finds it with `errors.As`. Today it
carries the failure's SQLCODE, SQLSTATE and message, and its `Error`
method gives them on one line, as `SQLCODE -919 (SQLSTATE 08006): ...`.
This page will describe the rest of a failure, the rule it cites, its
position, whether a transaction is active and its diagnostics, as the
codec and statements add them, milestones M1 and M3.
