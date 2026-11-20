# The SQL-agent

[Contents](README.md)

## NAME

`Agent`: a program's named connections, one current.

## SYNOPSIS

```go
type Connector interface {
	Connect(ctx context.Context, server, user *string) (*Conn, error)
}
type ConnectorFunc func(ctx context.Context, server, user *string) (*Conn, error)

func NewAgent(connector Connector) *Agent
func ConnectedAgent(connector Connector, conn *Conn) *Agent
func (a *Agent) Query(ctx context.Context, sql string) (*Outcome, error)
func (a *Agent) QueryInto(ctx context.Context, sql string, sink Sink) (*Streamed, error)
func (a *Agent) ConnectionStatement(ctx context.Context, sql string) (*Completion, bool, error)
func (a *Agent) Current() (*Conn, error)
func (a *Agent) CurrentName() (string, bool)
func (a *Agent) IsLost() bool
func (a *Agent) Terminate() error
```

## DESCRIPTION

A statement whose first word is CONNECT or DISCONNECT, or whose first
two are SET CONNECTION, is a connection statement, which the agent runs
itself [ISO/IEC 9075:1992 §15]; any other goes to the current
connection. Each name is a character string literal, its spaces trimmed.

`CONNECT TO server [AS name] [USER user]` asks the connector for a
connection to server, as user, or nil for none named; the connection is
named name, else server, read as an identifier, and made current.
`CONNECT TO DEFAULT` makes the default connection, of no name.
`ConnectedAgent` begins with conn as the default connection. `SET
CONNECTION name` or `DEFAULT` makes that one current. `DISCONNECT name`,
`DEFAULT`, `ALL` or `CURRENT` ends each one named; none is current after
if the current one was. `Terminate` ends every one left.

## ERRORS

| SQLSTATE | When |
|---|---|
| 0A001 | SQLCODE -900: CONNECT or SET CONNECTION while the current connection's transaction is active |
| 25000 | SQLCODE -917: DISCONNECT of a connection whose transaction is active |
| 28000 | SQLCODE -104: a user that is no authorization identifier |
| 2E000 | SQLCODE -919: a connection name that is no identifier |
| 08002 | SQLCODE -919: a connection of the name is established |
| 08003 | SQLCODE -919: no such connection, or none current |
| 08001 | SQLCODE -919: the connector could not reach the server |
| 08004 | SQLCODE -919: the server refused the connection |
| 2A000 | SQLCODE -101, -102 or -103: a connection statement that does not parse, or names by a literal that is no character string |

A connection that fails to end is a warning: DISCONNECT completes with
SQLSTATE 01002.

## EXAMPLES

The examples of [Pipelines and Pools](../user/08-pipelines-and-pools.md).

## STANDARD

[CLIENT The SQL-agent] of [The Client Library](https://github.com/turborel/turborel/blob/main/doc/client/02-library.md); ISO/IEC 9075:1992 §15, the connection statements.

## SEE ALSO

[Connections](21-connections.md), [Settings](26-settings.md).
