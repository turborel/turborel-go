# Large Objects

[Contents](README.md)

## NAME

`LargeObject`, `ReadLOB`, `LOBReader`, `LOBWriter`: large objects whole
or by locator.

## SYNOPSIS

```go
const LOBInlineOctets = 64 * 1024
const LOBChunkOctets = 256 * 1024

type LargeObject interface{ Value } // LOB or Locator

func (c *Conn) ReadLOB(ctx context.Context, lob LargeObject, from, count uint64, characters bool) ([]byte, error)
func (c *Conn) NewLOBReader(ctx context.Context, lob LargeObject) *LOBReader
func (r *LOBReader) Read(out []byte) (int, error)

func (c *Conn) CreateLOB(ctx context.Context, kind LOBKind) (*LOBWriter, error)
func (w *LOBWriter) Write(octets []byte) (int, error)
func (w *LOBWriter) Flush() error
func (w *LOBWriter) Finish() (Locator, error)

func (c *Conn) FreeLOB(ctx context.Context, locator Locator) error
func (c *Conn) HoldLOB(ctx context.Context, locator Locator) error
```

## DESCRIPTION

A large object of at most SERVER.LOB_INLINE_BYTES comes whole, a `LOB`;
a longer one by its locator, a `Locator` with its lengths, valid until
its transaction ends. `ReadLOB` sends a Lob read: at most `count` octets
from octet `from`, counted from 0, or of a CLOB if `characters`, whole
characters; the server gives at most a chunk, fewer only at the value's
end or a chunk's. Of a `LOB` it reads the value itself. A `LOBReader`
reads the value whole a chunk at a time, `io.EOF` at its length.

`CreateLOB` sends a Lob create; the `LOBWriter` sends what is written in
appends of at most `LOBChunkOctets`, once that much waits, on `Flush`
and on `Finish`, which gives the `Locator`, its lengths counted as
written, a CLOB's characters counted once each though split between
writes. The server checks a CLOB's octets as UTF-8 as each append
comes, a character split between appends taken; the driver does not
check them itself. `FreeLOB` and `HoldLOB` send a Lob free and hold.

A reader's and a writer's requests are cancelled on the server as the
context they were made with is done.

## ERRORS

| SQLSTATE | When |
|---|---|
| 0F001 | A locator that is not valid: freed, or its transaction ended |
| 22021 | An append of a CLOB whose octets are not UTF-8, checked as each append comes, or any append after one refused; the use of such a CLOB, or of one that ends within a character |
| 22001 | An append that makes the value longer than the longest |
| 08006 | SQLCODE -919: a reply of the wrong kind, or none |

A `LOBReader` whose server sends less than the value's length fails with
an error saying so.

## EXAMPLES

The examples of [Large Objects](../user/06-large-objects.md).

## STANDARD

[PROTOCOL Lob 'L'] and [PROTOCOL Values] of [The Native Protocol](https://github.com/turborel/turborel/blob/main/doc/client/05-protocol.md); [CLIENT Large objects] of [The Client Library](https://github.com/turborel/turborel/blob/main/doc/client/02-library.md).

## SEE ALSO

[Values](23-values.md), [Copy, Import and Export](25-copy.md).
