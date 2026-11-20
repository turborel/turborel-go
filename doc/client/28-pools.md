# Pools

[Contents](README.md)

## NAME

`Pool`, `Pooled`: connections shared by a program's goroutines.

## SYNOPSIS

```go
const DefaultPoolMax = 10
const DefaultPoolWait = 30 * time.Second
const DefaultIdleTimeout = 10 * time.Minute
const DefaultMaxLifetime = time.Hour
const ResetWithin = 30 * time.Second
const ResetMostStatements = 256

type PoolSource interface {
	Connect(ctx context.Context) (*Conn, error)
}
type PoolSourceFunc func(ctx context.Context) (*Conn, error)
type PoolConfig struct {
	Max, MaxIdle             int
	IdleTimeout, MaxLifetime time.Duration
	Wait                     time.Duration
	Check                    bool
	Reset                    []string
}
func DefaultPoolConfig() PoolConfig

func NewPool(source PoolSource, config PoolConfig) *Pool
func (p *Pool) Get(ctx context.Context) (*Pooled, error)
func (p *Pool) Counts() (int, int)
type Pooled struct {
	*Conn
	// unexported fields
}
func (p *Pooled) Release()
```

## DESCRIPTION

`Get` takes the most recently idle connection that is still within its
lifetime and idle timeout, closing each one that is not, and checks it
if `Check`, closing one that fails; else it makes one while fewer than
`Max` are made; else it waits, its own goroutine, until one comes back,
`Wait` passes or the context is done. A zero `IdleTimeout` or
`MaxLifetime` is no limit. `Counts` gives the connections made and how
many of them are idle.

`Release` gives the connection back: past its lifetime it is closed;
else it is reset, by the protocol's Reset where granted, its statements
gone, then each `Reset` statement; of a server that grants no reset, by
one pipeline of ROLLBACK, a Close and Deallocate of each statement it
prepared, SET SESSION AUTHORIZATION and SET SCHEMA of its login, SET
TIME ZONE LOCAL and the `Reset` statements; a connection of more than
`ResetMostStatements` prepared is closed. Autocommit is off after. A
connection lost or failing its reset, or beyond `MaxIdle`, is closed.
The check is Ping where granted, else ROLLBACK. Each check and reset
waits at most `ResetWithin`, or the connection's own read timeout if
shorter. `Release` again does nothing.

## ERRORS

| SQLSTATE | When |
|---|---|
| 08001 | SQLCODE -919: no connection came back within `Wait`, or the wait was given up as the context was done, which the failure wraps |
| any | The source's failure to make a connection |

## EXAMPLES

The examples of [Pipelines and Pools](../user/08-pipelines-and-pools.md).

## STANDARD

[CLIENT Pools] of [Settings, Pipelines and Pools](https://github.com/turborel/turborel/blob/main/doc/client/02-library-connections.md).

## SEE ALSO

[Settings](26-settings.md), [Pipelines](27-pipelines.md), [Appendix C](../appendix/c-settings.md).
