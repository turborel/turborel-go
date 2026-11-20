# Errors

[Contents](README.md)

## NAME

`Error`, `Diagnostics`, `Notice`: a failure, what it is about, and a
notice.

## SYNOPSIS

```go
type Error struct {
	SQLCode     int32
	SQLState    string
	Message     string
	Rule        string
	Position    uint64
	Placed      bool
	Active      bool
	Diagnostics *Diagnostics
}
func (e *Error) Error() string
func (e *Error) Unwrap() error

type Diagnostics struct {
	Severity, Detail, Hint, Context string
	Schema, Table, Column, DataType, Constraint string
}

type Notice struct {
	SQLState    string
	Message     string
	Diagnostics Diagnostics
}
func (c *Conn) SetNoticeHandler(handler func(Notice))
func (c *Conn) TakeNotices() []Notice
```

## DESCRIPTION

Every failure the driver returns is an `error` whose dynamic type is
`*Error`; a program finds it with `errors.As`. A server's failure
carries what the server sent: its SQLCODE and SQLSTATE, its message,
the rule of a standard it cites, its position in characters where
`Placed`, whether the session's transaction is `Active` after it, and,
with diagnostics granted, its `Diagnostics`. A failure the driver finds
itself carries the SQLCODE and SQLSTATE turborel-client gives the same
failure; `Unwrap` gives what made the driver give up, such as a context
done, so `errors.Is(err, context.Canceled)` holds. `Error` gives the
failure on one line, as turborel-client does: its SQLCODE, SQLSTATE and
message, then the rule it cites and where, if known.

A failure of SQLSTATE class 08 but 08003, or of class 28 as the
connection is made, loses the connection. A notice, a warning of a
statement or one of the server's own, is handed to the handler
`SetNoticeHandler` set as it comes, or with none kept, the last 64, for
`TakeNotices`.

## ERRORS

[Appendix B](../appendix/b-errors.md) lists the SQLSTATE classes, and the
failures the driver finds itself.
