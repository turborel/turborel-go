# Pipelines

[Contents](README.md)

## NAME

`Pipeline`: requests written at once, one outcome each.

## SYNOPSIS

```go
const PipelineWindow = 64 * 1024

type OnError uint8 // OnErrorContinue, OnErrorSkip
type Piped struct {
	Outcome *Outcome
	Failure *Error
	Skipped bool
}

func (c *Conn) Pipeline() *Pipeline
func (p *Pipeline) Query(sql string) *Pipeline
func (p *Pipeline) Execute(st *Statement, values []Value) *Pipeline
func (p *Pipeline) OnError(onError OnError) *Pipeline
func (p *Pipeline) Len() int
func (p *Pipeline) Run(ctx context.Context) ([]Piped, error)
```

## DESCRIPTION

`Run` encodes the requests and groups them into windows of at most
`PipelineWindow` bytes, a request longer than that alone in its own;
it writes each window at once and reads its replies, one `Piped` for
each request in order, before writing the next. Under autocommit each
request's `ends` is set, of `OnErrorContinue`.

Under `OnErrorSkip` a `SAVEPOINT TURBOREL_PIPELINE_n` is sent before
each request but the first, and no request ends its transaction. After
the first failure, a window not yet written is not sent; then
`ROLLBACK TO SAVEPOINT` of the savepoint after the failure undoes the
requests run after it, each marked `Skipped`; `RELEASE SAVEPOINT` of the
first ends the savepoints; and under autocommit of a batch that began no
transaction, COMMIT ends it. A pipeline run during a copy abandons the
copy first.

## ERRORS

| SQLSTATE | When |
|---|---|
| 08006 | SQLCODE -919: the connection was lost; what the requests answered before it is not given |
| any | Under `OnErrorSkip`, the failure of a statement undoing the requests after a failed one |

A request's own failure is its `Piped.Failure`, not `Run`'s.

## EXAMPLES

The examples of [Pipelines and Pools](../user/08-pipelines-and-pools.md).

## STANDARD

[PROTOCOL Requests] of [The Native Protocol](https://github.com/turborel/turborel/blob/main/doc/client/05-protocol.md); [CLIENT Pipelines] of [Settings, Pipelines and Pools](https://github.com/turborel/turborel/blob/main/doc/client/02-library-connections.md).

## SEE ALSO

[Statements and Cursors](22-statements.md), [Pools](28-pools.md).
