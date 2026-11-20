# Large Objects

[Contents](README.md)

A CLOB or BLOB of at most SERVER.LOB_INLINE_BYTES, 64 KiB unless the
operator sets otherwise, comes in its row whole, as a `LOB`: its kind and
its octets, a CLOB's UTF-8. A longer one comes as a `Locator`: its kind,
the locator the server gave it, and its lengths in octets and
characters, not its octets. Either is a `LargeObject`.

`ReadLOB` reads a range of one, by octets or by a CLOB's characters, at
most a chunk of 256 KiB at a time; of a `LOB` it reads the value itself,
with no request. `NewLOBReader` gives an `io.Reader` that reads it whole,
a chunk a request.

`CreateLOB` makes a new large object, written a piece at a time through
the `LOBWriter` it gives, an `io.Writer`; `Finish` gives it as a
`Locator`, to pass as a parameter of a CLOB or BLOB. A CLOB's octets must
be UTF-8, and a character may be split between writes.

```go
writer, _ := conn.CreateLOB(ctx, turborel.BinaryLOB)
chunk := bytes.Repeat([]byte{7}, 4096)
for range 1024 {
	writer.Write(chunk)
}
scan, _ := writer.Finish()
fmt.Println(scan.Octets)

insert, _ := conn.Prepare(ctx, "INSERT INTO SCANS VALUES (?, ?, ?)")
note := turborel.Char("a scan of page 1")
conn.Execute(ctx, insert, []turborel.Value{turborel.DecimalOf(1, 0), scan, note})
conn.Query(ctx, "COMMIT")

out, _ := conn.Query(ctx, "SELECT IMAGE, NOTE FROM SCANS")
image := out.Rows[0][0].(turborel.Locator)
fmt.Println(image.Octets, conn.InTransaction())
octets, err := io.ReadAll(conn.NewLOBReader(ctx, image))
fmt.Println(len(octets), err)

whole := out.Rows[0][1].(turborel.LOB)
page, _ := conn.ReadLOB(ctx, whole, 2, 4, true)
fmt.Printf("%s\n", page)
// 4194304
// 4194304 true
// 4194304 <nil>
// scan
```

## How long a locator lasts

A locator is valid until the transaction that read it ends. A statement
whose reply sent one keeps its transaction open, under autocommit too,
so commit once the value is read. `FreeLOB` ends the locator sooner, and
`HoldLOB` keeps it past the commit, until it is freed or a rollback ends
it. A locator no longer valid is refused with SQLSTATE 0F001.

```go
fmt.Println(conn.FreeLOB(ctx, image))
_, err = conn.ReadLOB(ctx, image, 0, 1, false)
fmt.Println(err)
// <nil>
// SQLCODE -301 (SQLSTATE 0F001): 2 is not a valid locator [ISO/IEC 9075-2:2023 §4.37.5]
```

An import sends a field longer than 64 KiB for a CLOB or BLOB column by
`CreateLOB`, so no message carries more ([Copying, Importing and
Exporting](07-copying.md)).
