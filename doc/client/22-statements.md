# Statements and Cursors

[Contents](README.md)

## NAME

`Query`, `Prepare`, `Execute`, cursors: running statements on a
connection.

## SYNOPSIS

```go
type Outcome struct {
	Columns    []Column
	Rows       []Row
	Completion Completion
}
func (o *Outcome) IsQuery() bool
type Sink interface {
	Columns(columns []Column)
	Rows(rows []Row)
}
type Streamed struct {
	Rows       uint64
	Completion Completion
}
type Completion struct {
	SQLCode    int32
	SQLState   string
	Active     bool
	Position   uint64
	Committed  bool
	More       bool
	Rows       uint64
	Counted    bool
	Generated  []string
	Conditions []*Error
	Time       *Timing
}
type Timing struct{ Total, Queued, Locks uint64 }
type Ran struct {
	Outcomes []*Outcome
	Failure  *Error
}

func (c *Conn) Query(ctx context.Context, sql string) (*Outcome, error)
func (c *Conn) QueryInto(ctx context.Context, sql string, sink Sink) (*Streamed, error)
func (c *Conn) QueryMany(ctx context.Context, sql string) (*Ran, error)
func (c *Conn) SetAutocommit(on bool)
func (c *Conn) Autocommit() bool
func (c *Conn) SetQueryAttributes(attributes []Attribute) error

func (c *Conn) Prepare(ctx context.Context, sql string) (*Statement, error)
func (s *Statement) Parameters() []Parameter
func (s *Statement) Columns() []Column
type Parameter struct {
	Varying bool
	Type    DataType
}
type Column struct {
	Name     string
	Named    bool
	Type     DataType
	NotNull  bool
	Declared *Declared
}
type Declared struct{ Name, Written string }
func (c Column) TypeWritten() string
func (c *Conn) Execute(ctx context.Context, st *Statement, values []Value) (*Outcome, error)
func (c *Conn) OpenCursor(ctx context.Context, st *Statement, values []Value, rows uint32) (*Batch, error)
func (c *Conn) OpenCursorNumbered(ctx context.Context, st *Statement, cursor uint32, values []Value, rows uint32) (*Batch, error)
func (c *Conn) Fetch(ctx context.Context, st *Statement, rows uint32) (*Batch, error)
func (c *Conn) FetchNumbered(ctx context.Context, st *Statement, cursor, rows uint32) (*Batch, error)
func (c *Conn) CloseCursor(ctx context.Context, st *Statement) error
func (c *Conn) CloseCursorNumbered(ctx context.Context, st *Statement, cursor uint32) error
func (c *Conn) Describe(ctx context.Context, st *Statement) (*Statement, error)
func (c *Conn) DescribeCursor(ctx context.Context, st *Statement, cursor uint32) ([]Column, error)
func (c *Conn) Deallocate(ctx context.Context, st *Statement) error

func (c *Conn) RowsAffected() (uint64, bool)
func (c *Conn) LastGenerated() []string
func (c *Conn) ServerTime() *Timing
func (c *Conn) CommitPosition() (uint64, bool)
func (c *Conn) Conditions() []*Error
func (c *Conn) TakeConditions() []*Error
func (c *Conn) Warning() (string, bool)
func (c *Conn) Reset(ctx context.Context) error
func (c *Conn) ChangeUser(ctx context.Context, user string, mechanism Mechanism) error
```

## DESCRIPTION

`Query` sends a Query; a query is answered by its columns, its rows and
a completion, another statement by a completion. A statement that fails
before its first batch of rows is answered by its failure alone, no
columns given; one that fails once a batch was sent fails as its
failure comes, a `Sink` given the batches before it. Under autocommit the
request ends its transaction: committed as the statement succeeds,
rolled back as it fails. `QueryInto` gives the rows to a `Sink` a batch
at a time. `QueryMany` sends a text of several statements as one
request, with multi-statement granted.

`Prepare` prepares a statement under the connection's next number;
`Execute` runs it, a query's rows read whole, a CALL's output
parameters' values as one row of its columns; `OpenCursor` opens its
cursor and reads at most the rows asked for, `Fetch` more, each `Batch`
saying whether the cursor has more. A cursor other than 0 needs cursors,
`Describe` and `DescribeCursor` need describe.

A `Column` has its name, if it has one, the `Type` its values are read
by, and whether it is NOT NULL. Its `Declared` is the type it was
declared with, granted `declared-types`, which connecting asks for: its
name as the Information Schema's DATA_TYPE gives it, as INTEGER or
DECIMAL, and the type as SQL writes it, as DECIMAL(9,2); else nil.
`TypeWritten` gives the declared type where the server told it, else
the type its tag says, an exact numeric one as NUMERIC(10,0), an
approximate one as FLOAT(53). A `Parameter` is a `?`'s type, `Varying`
where the statement does not decide it.

A completion's tail is kept as the connection's last: `RowsAffected`
with row-counts, `LastGenerated` with generated-values, `ServerTime`
with row-counts, `CommitPosition` with commit-position, `Conditions` with
conditions, which `TakeConditions` takes. `Reset` and `ChangeUser` need reset and change-user.

Each call takes a context; one done while its statement runs cancels it
on the server, by the protocol's Cancel, and the call's failure has the
context's error as its cause. A connection opened over a program's
stream, which cannot reach its server again, gives the call up instead,
the connection lost.

## ERRORS

| SQLSTATE | When |
|---|---|
| 24000 | A cursor already open, or not open; a statement deallocated or replaced while its cursor is open |
| 26000 | A statement deallocated, or of no such number |
| 40002 | A commit, of autocommit, that failed |
| TB920 | SQLCODE -920: the statement was cancelled |
| 08006 | SQLCODE -919, before anything is sent, the connection kept: query attributes past the bounds a server takes |
| 08006 | SQLCODE -919: a capability not granted, a reply out of its place or malformed, declared types not one for each column among them, or the connection lost |
| TB916 | SQLCODE -916: a reply holding a value the driver cannot take, the rest of the statement's replies read past, the connection kept; past turborel's limits, -900 (0A000) |

## EXAMPLES

The examples of [Running Queries](../user/03-queries.md), [Prepared Statements](../user/04-prepared-statements.md) and [Transactions](../user/05-transactions.md).

## STANDARD

[PROTOCOL Query 'Q'], [PROTOCOL Prepare 'P'], [PROTOCOL Execute 'E'], [PROTOCOL Fetch 'F'], [PROTOCOL Close 'C'], [PROTOCOL Deallocate 'D'], [PROTOCOL Describe 'Y'], [PROTOCOL Prepared 'P'], [PROTOCOL Description 'D'], [PROTOCOL Declared types], [PROTOCOL Complete 'K'] and [PROTOCOL The tail] of [The Native Protocol](https://github.com/turborel/turborel/blob/main/doc/client/05-protocol.md); [CLIENT Running statements], [CLIENT Prepared statements] and [CLIENT Controlling the session] of [The Client Library](https://github.com/turborel/turborel/blob/main/doc/client/02-library.md).

## SEE ALSO

[Values](23-values.md), [Pipelines](27-pipelines.md), [Errors](30-errors.md).
