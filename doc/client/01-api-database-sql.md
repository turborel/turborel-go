# The database/sql Driver

[Contents](README.md)

## NAME

`Driver`, `DBConnector`, `NativeConn`: the driver `database/sql` loads
as `turborel`.

## SYNOPSIS

```go
const DriverName = "turborel"

type Driver struct{}
func (Driver) Open(name string) (driver.Conn, error)
func (Driver) OpenConnector(name string) (driver.Connector, error)

type DBConnector struct{ /* unexported */ }
func NewDBConnector(settings *Settings) *DBConnector
func (c *DBConnector) Connect(ctx context.Context) (driver.Conn, error)
func (*DBConnector) Driver() driver.Driver

func NativeConn(driverConn any) (*Conn, bool)
```

## DESCRIPTION

Importing the package registers `Driver` as `turborel`. `sql.Open`
reads its name as a connection URI, by `SettingsFromURI`; `sql.OpenDB`
takes a `DBConnector` of `Settings` made in Go. Each connection is made
by `Settings.Connect`.

A connection is a `driver.Conn` that is also a `ConnBeginTx`,
`ConnPrepareContext`, `ExecerContext`, `QueryerContext`, `Pinger`,
`SessionResetter`, `Validator` and `NamedValueChecker`:

- Outside a transaction each statement is its own, by autocommit.
  `BeginTx` turns autocommit off, and sets the isolation level and
  read-only of `sql.TxOptions` by SET TRANSACTION: READ UNCOMMITTED, READ
  COMMITTED, REPEATABLE READ or SERIALIZABLE; another level is refused.
  `Commit` and `Rollback` send COMMIT and ROLLBACK, autocommit on again.
- `ExecContext` of no arguments runs the statement by Query; of
  arguments it prepares, executes and deallocates it. Its result's
  `RowsAffected` is the server's count; `LastInsertId` the last identity
  value the statement generated, if an integer.
- `QueryContext` prepares the query and reads its rows through its
  cursor, 1000 at a time; `Rows.Close` closes the cursor if it has more,
  and deallocates the statement.
- A statement prepared is a `StmtExecContext` and `StmtQueryContext`.
- `Ping` sends Ping where granted, else ROLLBACK. `ResetSession` and
  `IsValid` refuse a connection lost.
- Parameters are each a `?`; a named argument is refused.

`CheckNamedValue` sends each Go value as the driver's value it is: nil a
null; each integer an exact number; a float an approximate number; a
bool, a string and bytes as they are; a `time.Time` a TIMESTAMP WITH
TIME ZONE to the microsecond; any of the driver's values as it is; a
`driver.Valuer` as what it gives.

`Rows.Next` gives each value as the Go value it scans most simply:

| Value | Go value |
|---|---|
| null | nil |
| exact number of scale 0 an int64 holds | `int64` |
| other exact number, DECFLOAT | its text, a `string` |
| approximate number | `float64` |
| character string, CLOB | `string` |
| binary string, BLOB | `[]byte` |
| truth value | `bool` |
| datetime | `time.Time` |
| any other | the driver's own value |

An exact number keeps every digit, up to Turborel's 38: one an int64
cannot hold comes as its text, which scans whole into a `Decimal` or a
string. Scanned into an int64, such a value fails as out of range;
scanned into a float64, `database/sql` rounds it to the nearest float64,
as the program asked. The driver itself never rounds an exact number.

A large object that came by its locator is read whole first. The rows'
column types give their names as the Information Schema's DATA_TYPE
does, of the type each was declared with: INTEGER of an INTEGER
column, DECIMAL of a DECIMAL(9,2) one. The connection asks for
`declared-types`; where the server did not grant it, the name is that
of the type the value's tag says, NUMERIC of both. They give whether
each may hold a null, the scan type of the table above, a string type's
length, and an exact number's precision and scale.

Each of the driver's value types is a `sql.Scanner`, taking its own type
and the Go value above, and a `driver.Valuer`, giving that Go value, or
its text or literal, for any driver.

A context done while a statement runs cancels it by the protocol's
Cancel; the failure wraps the context's error. `NativeConn` gives the
native `*Conn` of a connection, as `sql.Conn`'s `Raw` hands it over.

## ERRORS

Every failure is an `*Error`, found by `errors.As`. One of a connection
lost before the call wraps `driver.ErrBadConn` too, so `database/sql`
takes another connection.

| SQLSTATE | When |
|---|---|
| 08001 | SQLCODE -919: `sql.Open` of a URI that is no `turborel://` URI, or no server kept |
| TB910 | SQLCODE -910: an isolation level none of SQL's; a named argument; a Go value of no type the driver sends; a value scanned into a type it is not; a result of no identity value, or of no rows counted |

## EXAMPLES

The examples of [Installing](../user/01-installing.md).

## STANDARD

The Go standard library's [database/sql/driver](https://pkg.go.dev/database/sql/driver) interfaces; the driver's own decision RULES.md D2.

## SEE ALSO

[Settings](26-settings.md), [Connections](21-connections.md), [Values](23-values.md), [Errors](30-errors.md).
