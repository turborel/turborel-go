# Running Queries

[Contents](README.md)

`Query` runs one statement, a direct SQL statement, and gives its
`Outcome`: a query's `Columns` and `Rows`, or for another statement no
columns; its `Completion` either way, with its SQLCODE and SQLSTATE. A
query that returns no row completes with SQLCODE 100, SQLSTATE 02000. A
statement that fails gives an `*Error` ([Errors and
Diagnostics](10-errors.md)).

```go
for _, sql := range []string{
	"CREATE SCHEMA ANN",
	"CREATE TABLE STAFF (ID INTEGER GENERATED ALWAYS AS IDENTITY, NAME VARCHAR(20), SALARY DECIMAL(9,2))",
	"INSERT INTO STAFF (NAME, SALARY) VALUES ('Ada', 61000.00), ('Grace', 72000.50)",
	"COMMIT",
} {
	if _, err := conn.Query(ctx, sql); err != nil {
		return err
	}
}
out, _ := conn.Query(ctx, "SELECT ID, NAME, SALARY FROM STAFF ORDER BY ID")
fmt.Println(out.Columns[1].Name, len(out.Rows))
for _, row := range out.Rows {
	fmt.Println(row[0], row[1], row[2])
}
none, _ := conn.Query(ctx, "SELECT ID FROM STAFF WHERE ID > 9")
fmt.Println(none.Completion.SQLCode, none.Completion.SQLState)
// NAME 2
// 1 Ada 61000.00
// 2 Grace 72000.50
// 100 02000
```

A row is a `Row`, a value for each column, each one of the package's
value types: an exact number a `Decimal`, which `String` writes as SQL
does; a character string a `Char`; and so on, as [Appendix
A](../appendix/a-types.md) lists them.

`QueryInto` gives a query's columns, then its rows a batch at a time, to
a `Sink` as they come, so a program holds no more than a batch.

## What a completion tells

Each statement's completion tells more, each part as the server granted
it, and the connection keeps the last: `RowsAffected` gives the rows an
INSERT, UPDATE, DELETE or MERGE changed, or a query returned;
`LastGenerated` the identity values an INSERT generated, in the order of
its rows, as SQL writes them; `ServerTime` the server's time for the
statement; `CommitPosition` the log position of the last commit, for a
replica's session to read after; `Conditions` the warnings it raised.

```go
conn.SetAutocommit(true)
if _, err := conn.Query(ctx, "INSERT INTO STAFF (NAME) VALUES ('Edsger'), ('Barbara')"); err != nil {
	return err
}
rows, _ := conn.RowsAffected()
fmt.Println(rows, conn.LastGenerated(), conn.InTransaction())
// 2 [3 4] false
```

## Several statements, and attributes

`QueryMany` runs a text of several statements as one request, each in
turn until one fails, and gives each outcome and the failure that
stopped the rest. `SetQueryAttributes` sets attributes every later
statement carries, read in SQL by QUERY_ATTRIBUTE, until set again.
