# Copy, Import and Export

[Contents](README.md)

## NAME

`Copy`, `Import`, `Export`: rows in bulk, delimited text, and a
query's rows written out.

## SYNOPSIS

```go
const CopyChunkBytes = 256 * 1024

func (c *Conn) CopyIn(ctx context.Context, table string, columns []string) (*Copy, error)
func (k *Copy) Send(row Row) error
func (k *Copy) Finish() (uint64, error)
func (k *Copy) Abandon() error
func (c *Conn) CopyRows(ctx context.Context, table string, columns []string, rows iter.Seq[Row]) (uint64, error)

type Source interface {
	Headings() ([]Heading, error)
	Record() (*Record, error)
}
type Heading struct {
	Name string
	Type DataType
}
type Record struct {
	Values   []Value
	Position Position
}
type Position struct {
	Record, Line uint64
	HasLine      bool
}
type SourceError struct {
	Position Position
	Message  string
}

const DefaultBatch = 1000
type Import struct {
	Table string
	Batch uint64
	Skip  bool
}
func ImportInto(table string) Import
type Imported struct{ Rows, Rejected uint64 }
type Cause struct {
	Input string
	SQL   *Error
}
type Rejected struct {
	Position Position
	Cause    Cause
}
type ImportError struct {
	Position              Position
	Placed                bool
	Cause                 Cause
	Committed, RolledBack uint64
}
func (c *Conn) Import(ctx context.Context, source Source, how Import, rejected func(Rejected)) (Imported, error)

const DefaultMaxRecord = 16 * 1024 * 1024
func CSV() Dialect
func TSV() Dialect
func (d Dialect) WithDelimiter(delimiter rune) (Dialect, error)
func (d Dialect) WithHeader() Dialect
func (d Dialect) WithNull(null string) Dialect
func (d Dialect) WithMaxRecord(bytes int) Dialect
func NewDelimited(input io.Reader, dialect Dialect) *Delimited

type ExportFormat uint8 // ExportCSV or ExportNative
func (c *Conn) Export(ctx context.Context, sql string, out io.Writer, format ExportFormat) (uint64, error)
func ReadNative(in io.Reader) ([]Column, []Row, error)
```

## DESCRIPTION

`CopyIn` sends CopyIn, its `ends` set under autocommit, and needs copy
granted. `Send` holds rows until about `CopyChunkBytes` wait, then sends
them in one CopyData; `Finish` sends the rest and CopyEnd, whose
Complete counts the rows inserted; `Abandon` sends CopyEnd abandoning
the copy, and of a copy ended does nothing. Until CopyEnd the connection
sends only CopyData, CopyEnd and Ping; any other request first abandons
the copy.

`Import` prepares `INSERT INTO table (columns) VALUES (?, ...)`, its
columns the source's headings, each read as an identifier if it is one,
or else the table's, as a prepared `SELECT *` describes them; the
table's name is read as SQL reads a table name and each part written
delimited. A string for a character string column is assigned to its
type as INSERT assigns. Rows go a batch to a copy, which commits as it
ends, where copy is granted, `Skip` is false and no column is a large
object's; else a row to an Execute, a COMMIT after each batch, a field
longer than `LOBInlineOctets` for a CLOB or BLOB column sent by
`CreateLOB`. A failing row is skipped only if it alone was undone and
the connection goes on.

A `Delimited` reads RFC 4180 CSV, or tab-separated text with no
quoting, a line at a time, a record at most `DefaultMaxRecord` bytes; a
field written as the dialect's text for null, not quoted, is `Null`,
any other a `Char`; a byte order mark beginning the input is passed
over.

`Export` runs the query by `QueryInto`. CSV records end with CR LF; a
field is quoted if it is empty or holds a comma, a quote, a line break
or white space at either end. Native rows are a Description, Rows, then
a Complete.

## ERRORS

| SQLSTATE | When |
|---|---|
| 23000, 22001, ... | A row a copy refuses, at `Finish`: its Diagnostics' detail names the row, as `row 7 of the copy` |
| 08006 | SQLCODE -919: copy not granted; an export of no query, or whose write fails; input that is not native rows |
| TB910 | SQLCODE -910: a row sent to a copy ended, or a copy finished twice; a delimiter that is not one ASCII character other than a quote or a line break |

An import's failure is an `*ImportError`: where its record began, if
`Placed`; its `Cause`, the source's text or SQL's `*Error`, its position
dropped; `Unwrap` gives SQL's, or of the source's text an `*Error` of
SQLCODE -301 and SQLSTATE 22000; the rows committed, and those of the
batch rolled back. A source's failure is a `*SourceError`, its message
as turborel-client gives it, as `[DESIGN] the record is longer than
1024 bytes`, which `Unwrap` gives as an `*Error` of SQLSTATE 22000.
