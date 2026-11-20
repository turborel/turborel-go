# Transactions

[Contents](README.md)

A transaction begins with the first statement and lasts until COMMIT or
ROLLBACK, as SQL-92 has it; `InTransaction` says whether the session's
transaction is active, as the server said after the last statement.

`SetAutocommit(true)` makes each statement `Query`, `QueryInto` and
`Execute` run its own transaction: the server commits it as the
statement succeeds, or rolls it back as it fails, within the same
request, so no COMMIT is sent. A commit that fails gives its failure in
place of the statement's completion, the transaction rolled back. A
cursor keeps its transaction while it is open, and a result that sent a
large object by its locator keeps it too, since the locator lasts only
as long as the transaction does.

## The session

`Setting` gives a session setting as the server last told it, with no
round trip: SESSION.TIME_ZONE, TRANSACTION.ISOLATION and every setting
a session may set, and the server's status, AUTHORIZATION, SCHEMA,
READ_ONLY, SERVER_READ_ONLY, CATALOG, OWNER and SERVER_VERSION. A SET is
seen at once.

`Reset` puts the session back as it was at login: its transaction rolled
back, its prepared statements, cursors, temporary tables and settings
gone, its user kept. `ChangeUser` authenticates the connection again as
another user, whose new session it then is; refused, the connection
fails. A transaction either rolls back is told by the notice 01T01
([Errors and Diagnostics](10-errors.md)).

```go
fmt.Println(conn.Reset(ctx))
for _, n := range conn.TakeNotices() {
	fmt.Println(n.SQLState, n.Diagnostics.Severity, n.Message)
}
// <nil>
// 01T01 NOTICE [DESIGN] Reset rolled back the transaction that was active
```

A context done while a statement runs cancels it on the server, by the
protocol's Cancel over a connection of its own; the call's failure,
SQLSTATE TB920, has the context's error as its cause, so
`errors.Is(err, context.Canceled)` holds, and the connection goes on.
