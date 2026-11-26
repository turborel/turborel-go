# Copying, Importing and Exporting

[Contents](README.md)

## Copying

`CopyIn` begins a copy of rows into a table, of the columns named, or
all, the server granting copy. `Send` sends rows, many to a message, and
no reply each; `Finish` ends the copy, giving the rows inserted. Each
value is assigned to its column as INSERT assigns it. A copy is all or
nothing: a row refused undoes every row of it, its failure's detail
naming the row. `Abandon` gives a copy up, every row undone; a copy left
unfinished is abandoned as the connection's next other request is sent.
Until a copy ends, the connection sends only its rows, its end and
`Ping`. Under autocommit a copy is its own transaction. `CopyRows`
copies the rows an iterator gives. `SendText` sends a row of text and
nulls, as a file's record is, by column where the server grants
`columns`, which connecting asks for, the server taking each cell as
the character string of its text, as it does a row `Send` sends; it is
how an import copies.

```go
rows := []turborel.Row{
	{turborel.DecimalOf(7, 0), turborel.Char("Niklaus")},
	{turborel.DecimalOf(8, 0), turborel.Char("Barbara")},
}
inserted, err := conn.CopyRows(ctx, "STAFF", []string{"ID", "NAME"}, slices.Values(rows))
fmt.Println(inserted, err)
conn.Query(ctx, "COMMIT")

k, _ := conn.CopyIn(ctx, "STAFF", nil)
k.Send(turborel.Row{turborel.DecimalOf(9, 0), turborel.Char("Edsger"), turborel.Null{}})
k.Send(turborel.Row{turborel.Null{}, turborel.Char("Nobody"), turborel.Null{}})
_, err = k.Finish()
var failure *turborel.Error
if errors.As(err, &failure) {
	fmt.Println(failure.SQLState, failure.Diagnostics.Detail)
}
// 2 <nil>
// 23000 row 2 of the copy
```

## Importing

`Import` inserts the rows of a `Source` into a table. A `Source` gives
`Headings`, its fields' names, and types if it knows them, or none; then
each `Record`, a value for each field and where it began, its record
number and line, until it gives none. It holds a record at a time, so a
source of any size is read in bounded memory. `NewDelimited` reads CSV
(`CSV()`) and tab-separated text (`TSV()`) from any `io.Reader`, with a
header, a delimiter and a text for null as its `Dialect` says.

The `Import` names the table, how many rows are committed at a time
(`Batch`, `DefaultBatch` 1000, 0 for all at once), and whether a row
that fails is skipped (`Skip`), each given to the function passed. The
columns are the header's names, or else the table's, in order. A string
for a character string column is assigned to it as INSERT assigns, so
one too long is refused, not cut. The rows go by copy where the server
grants it, no row is to be skipped and no column is a large object's;
else by a prepared INSERT. The session must have no transaction active.

```go
csv := "ID,NAME,SALARY\n1,Ada,61000.00\n2,Grace,72000.50\n"
source := turborel.NewDelimited(strings.NewReader(csv), turborel.CSV().WithHeader())
done, err := conn.Import(ctx, source, turborel.ImportInto("STAFF"), nil)
fmt.Println(done.Rows, err)

bad := "3,Edsger,1\n,Nobody,2\n4,Barbara,3\n"
source = turborel.NewDelimited(strings.NewReader(bad), turborel.CSV())
how := turborel.ImportInto("STAFF")
how.Skip = true
done, err = conn.Import(ctx, source, how, func(r turborel.Rejected) {
	fmt.Println(r.Position)
})
fmt.Println(done.Rows, done.Rejected, err)
// 2 <nil>
// record 2, line 2
// 2 1 <nil>
```

A row that fails, not skipped, ends the import with an `*ImportError`:
where its record began, why (`Cause.Input`, the source's, or
`Cause.SQL`, an `*Error`), how many rows were committed and how many of
the batch rolled back.

```go
how.Skip = false
source = turborel.NewDelimited(strings.NewReader("5,Tony,1\n6,Tony,abc\n"), turborel.CSV())
_, err = conn.Import(ctx, source, how, nil)
fmt.Println(err)
// record 2, line 2: SQLCODE -301 (SQLSTATE 22018): 'abc' is not a <signed numeric literal> [ISO/IEC 9075:1992 §6.10 GR3b i, GR4b i]; 0 rows committed, 1 rolled back
```

## Exporting

`Export` runs a query and writes its rows to any `io.Writer` as they
come, as CSV, a header of its columns' names then a record for each row
(`ExportCSV`), or as the protocol's own frames (`ExportNative`), which
`ReadNative` reads back with every value as it was. In CSV a value is
written as turborel-client shows it, a null as an empty field.

```go
n, err := conn.Export(ctx, "SELECT ID, NAME, SALARY FROM STAFF ORDER BY ID", os.Stdout, turborel.ExportCSV)
fmt.Println(n, err)
// ID,NAME,SALARY
// 1,Ada,61000.00
// 2,Grace,72000.50
// 3,Edsger,1.00
// 4,Barbara,3.00
// 7,Niklaus,
// 8,Barbara,
// 6 <nil>
```
