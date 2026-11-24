# Errors and Diagnostics

[Contents](README.md)

Every failure the driver returns is a `*turborel.Error`, returned as
Go's `error`, so a program finds it with `errors.As`: the server's, as
the server sent it, or one the driver found itself, as a connection
lost. It carries the failure's `SQLCode` and `SQLState`, its `Message`,
the `Rule` it cites, its `Position` in the statement where `Placed`, and
whether a transaction is `Active` after it.

```go
_, err = conn.Query(ctx, "SELECT * FROM NOPE")
var failure *turborel.Error
if errors.As(err, &failure) {
	fmt.Println(failure.SQLCode, failure.SQLState, failure.Message, failure.Position)
}
// -103 42000 table ANN.NOPE is not defined 14
```

## Diagnostics

With diagnostics granted, a failure's `Diagnostics` say what it is
about: its severity, ERROR, FATAL, WARNING or NOTICE; its detail, hint
and context; and the schema, table, column, data type and constraint,
each if known. A duplicate key names its constraint and table, and its
detail gives the key.

```go
_, err = conn.Query(ctx, "INSERT INTO BADGES VALUES (1)")
var failure *turborel.Error
if errors.As(err, &failure) {
	fmt.Println(failure)
	d := failure.Diagnostics
	fmt.Println(d.Severity, d.Schema, d.Table, d.Constraint)
	fmt.Println(d.Detail)
	fmt.Println(failure.Active)
}
// SQLCODE -201 (SQLSTATE 23000): PRIMARY KEY ANN.BADGE_KEY (ID) of ANN.BADGES would be violated: a row of these values is there [X3.135-1986 §6.4 GR2]
// ERROR ANN BADGES BADGE_KEY
// the key (1) is held by another row
// true
```

## Which failures end the connection

A failure of the connection itself, SQLCODE -919 of SQLSTATE class 08,
loses the connection, and it takes no more: a reply malformed, cut short
or too long, a wait run out, the server gone. So does an Error of class
08 but 08003, and one of class 28 as the connection is made. Any other
failure is its statement's, and the session goes on. A reply whole but
holding a value the driver cannot take, as damaged JSON or a truth value
of 2, fails its statement alone with that value's failure, SQLCODE -916
(SQLSTATE TB916), or -900 (0A000) past turborel's limits; the rest of the
statement's replies are read past, so the next statement takes its own.

## Warnings

A statement's warnings, completion conditions of class 01, come one
way. With conditions granted they come in its completion, and
`Conditions` gives them, and `TakeConditions` takes them, so each is
told once; otherwise, with notices granted, each comes as
a notice before its outcome, handed to the handler `SetNoticeHandler`
set, or with none kept, the last 64, for `TakeNotices`. The server's own
notices always come as notices. `Warning` gives the SQLSTATE of the
warning the last statement completed with.

```go
_, err = conn.Query(ctx, "SELECT SUM(SALARY) FROM STAFF")
fmt.Println(err, len(conn.Conditions()))
for _, c := range conn.Conditions() {
	fmt.Println(c.SQLState, c.Message)
}
// <nil> 1
// 01003 the statement completed with warning: null value eliminated in set function
```

[Appendix B](../appendix/b-errors.md) lists the SQLSTATE classes.
