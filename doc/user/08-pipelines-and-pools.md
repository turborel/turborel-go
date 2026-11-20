# Pipelines and Pools

[Contents](README.md)

## Pipelines

A `Pipeline` queues queries and executions, writes them at once, and
reads one outcome for each, in order: a batch costs one round trip, not
one a request. The requests go a window of 64 KiB at a time, the
window's replies read before the next is written. Each request is its
own: by default a failure is that request's outcome alone, and the rest
run; under autocommit each request is its own transaction.

```go
conn.SetAutocommit(true)
insert, _ := conn.Prepare(ctx, "INSERT INTO T VALUES (?)")
outcomes, err := conn.Pipeline().
	Execute(insert, []turborel.Value{turborel.DecimalOf(1, 0)}).
	Execute(insert, []turborel.Value{turborel.DecimalOf(1, 0)}).
	Query("SELECT COUNT(*) FROM T").
	Run(ctx)
fmt.Println(err)
for _, o := range outcomes {
	if o.Failure != nil {
		fmt.Println("failed", o.Failure.SQLState)
	} else {
		fmt.Println("done", o.Outcome.Rows)
	}
}
// <nil>
// done []
// failed 23000
// done [[1]]
```

`OnError(OnErrorSkip)` makes the batch stop at its first failure. A
savepoint is set before each request but the first, and the requests
after a failure are rolled back and marked `Skipped`, or never sent if
their window was not yet written. Such a batch is one transaction,
committed as it ends under autocommit where it began none.

```go
outcomes, _ = conn.Pipeline().OnError(turborel.OnErrorSkip).
	Query("INSERT INTO T VALUES (2)").
	Query("INSERT INTO T VALUES (1)").
	Query("INSERT INTO T VALUES (3)").
	Run(ctx)
for _, o := range outcomes {
	fmt.Println(o.Outcome != nil, o.Failure != nil, o.Skipped)
}
// true false false
// false true false
// false false true
```

## Pools

A `Pool` is a bounded set of connections a program's goroutines share.
`Get` hands out an idle connection that answers, or makes one while
fewer than `Max` are made, or waits for one to come back, at most
`Wait`. `Release` gives it back, reset to the state it logged in with,
so no borrower sees another's session. A pool is made of `Settings`, or
of any `PoolSource`, as a function. [Appendix C](../appendix/c-settings.md)
lists its settings.

```go
source := turborel.PoolSourceFunc(func(ctx context.Context) (*turborel.Conn, error) {
	return turborel.Connect(ctx, "127.0.0.1:17433", "ANN", turborel.Trust{})
})
config := turborel.DefaultPoolConfig()
config.Max = 2
pool := turborel.NewPool(source, config)
pooled, err := pool.Get(ctx)
if err != nil {
	return err
}
out, _ := pooled.Query(ctx, "SELECT COUNT(*) FROM T")
fmt.Println(out.Rows)
pooled.Release()
fmt.Println(pool.Counts())
// [[2]]
// 1 1
```

A connection comes back reset by the protocol's Reset, autocommit off,
then the `Reset` statements run; it is handed out again checked by Ping.
Of a server that takes neither, the check is ROLLBACK and the reset one
pipeline. A connection lost, failing its reset, past its lifetime or
beyond `MaxIdle` is closed as it comes back; one idle past `IdleTimeout`,
or failing its check, as `Get` meets it. The pool starts no goroutine.

## The SQL-agent

An `Agent` holds a program's connections, each named, one current. It
runs CONNECT, SET CONNECTION and DISCONNECT itself, making connections
through the `Connector` the program gives, and sends every other
statement to the current connection.

```go
agent := turborel.NewAgent(turborel.ConnectorFunc(func(ctx context.Context, server, user *string) (*turborel.Conn, error) {
	return turborel.Connect(ctx, *server, "ANN", turborel.Trust{})
}))
for _, sql := range []string{
	"CONNECT TO '127.0.0.1:17433' AS 'Payroll'",
	"SELECT COUNT(*) FROM T",
	"COMMIT",
	"CONNECT TO '127.0.0.1:17433' AS 'Audit'",
	"SET CONNECTION 'Payroll'",
	"DISCONNECT ALL",
} {
	out, err := agent.Query(ctx, sql)
	name, _ := agent.CurrentName()
	if err != nil {
		fmt.Println(err)
		continue
	}
	fmt.Println(name, out.Completion.SQLState, out.Rows)
}
// PAYROLL 00000 []
// PAYROLL 00000 [[2]]
// PAYROLL 00000 []
// AUDIT 00000 []
// PAYROLL 00000 []
//  00000 []
```

A connection's name is read as an identifier, so 'Payroll' is PAYROLL.
No connection is left, or ended, while its transaction is active.
