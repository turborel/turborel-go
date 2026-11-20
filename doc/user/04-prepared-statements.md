# Prepared Statements

[Contents](README.md)

`Prepare` prepares a statement once, its dynamic parameters written `?`;
`Execute` runs it with a value for each, as many times as wanted.
`Parameters` gives the type each `?` takes, `Columns` a query's columns.
A prepared statement stays valid across transactions, until `Deallocate`
lets it go.

```go
byID, _ := conn.Prepare(ctx, "SELECT NAME FROM STAFF WHERE ID = ?")
fmt.Println(len(byID.Parameters()), byID.Columns()[0].Name)
got, _ := conn.Execute(ctx, byID, []turborel.Value{turborel.DecimalOf(2, 0)})
fmt.Println(got.Rows[0][0])
// 1 NAME
// Grace
```

## Cursors

`OpenCursor` runs a prepared query and gives its first rows, at most as
many as asked, all if 0; `Fetch` gives the next batch; each `Batch` says
whether the cursor has `More`. `CloseCursor` ends it before its last
row. Under autocommit, with fetch-ends granted, the cursor's transaction
ends as the cursor does: the fetch of its last row commits, a fetch that
fails rolls back, and closing it commits.

```go
all, _ := conn.Prepare(ctx, "SELECT NAME FROM STAFF ORDER BY ID")
batch, _ := conn.OpenCursor(ctx, all, nil, 1)
fmt.Println(batch.Rows, batch.More)
batch, _ = conn.Fetch(ctx, all, 0)
fmt.Println(batch.Rows, batch.More)
fmt.Println(conn.Deallocate(ctx, all))
// [[Ada]] true
// [[Grace]] false
// <nil>
```

With cursors granted, `OpenCursorNumbered`, `FetchNumbered` and
`CloseCursorNumbered` keep several cursors of one statement open at
once, each by a number of the program's. With describe granted,
`Describe` describes a statement again and `DescribeCursor` gives an open
cursor's columns. A cursor already open of a number fails, as do a fetch
or close of one not open, and deallocating a statement whose cursor is
open (SQLSTATE 24000); a statement deallocated is none such (26000).
