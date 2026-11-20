# Errors

[Contents](README.md)

This page will describe the driver's error: its SQLCODE, SQLSTATE and
message, the rule it cites, its position, whether a transaction is still
active, its diagnostics and its cause; and the failures the driver
raises itself, each with the SQLCODE and SQLSTATE turborel-client gives
it. It is filled at milestone M3.

Today the error is `*turborel.Error`, returned as Go's `error`, so a
program finds it with `errors.As`. It carries the failure's SQLCODE,
SQLSTATE and message, and its `Error` method gives them on one line, as
`SQLCODE -919 (SQLSTATE 08006): ...`.
