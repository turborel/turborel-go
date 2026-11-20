# Prepared Statements

[Contents](README.md)

`Prepare` prepares a statement once, its dynamic parameters written `?`;
`Execute` runs it with a value for each, as many times as wanted.
`Parameters` gives the type each `?` takes, `Columns` a query's columns.
A prepared statement stays valid across transactions, until `Deallocate`
lets it go. A CALL takes a `?` for an OUT or INOUT parameter too: its
`Parameters` are its inputs alone, its `Columns` one for each output, in
the procedure's order, and `Execute` gives the outputs' values as one
row of those columns.

```go
byID, _ := conn.Prepare(ctx, "SELECT NAME FROM STAFF WHERE ID = ?")
fmt.Println(len(byID.Parameters()), byID.Columns()[0].Name)
got, _ := conn.Execute(ctx, byID, []turborel.Value{turborel.DecimalOf(2, 0)})
fmt.Println(got.Rows[0][0])
// 1 NAME
// Grace
```

A column's `Type` is what its values are read by, and one type's tag
stands for several declared types: an INTEGER column's `Type` is an
exact numeric type of precision 10 and scale 0, as a DECIMAL(10,0)
one's is. The connection asks for `declared-types`; granted it, a
column's `Declared` holds the type it was declared with, its `Name` as
the Information Schema's DATA_TYPE gives it and its type `Written` as
SQL writes it. `TypeWritten` gives the declared type where the server
told it, else the type its tag says, NUMERIC(10,0) of that INTEGER
column.

```go
typed, _ := conn.Prepare(ctx, "SELECT ID, NAME, SALARY FROM STAFF")
for _, c := range typed.Columns() {
	fmt.Println(c.Name, c.Declared.Name, c.TypeWritten())
}
// ID INTEGER INTEGER
// NAME CHARACTER VARYING CHARACTER VARYING(20)
// SALARY DECIMAL DECIMAL(9,2)
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
