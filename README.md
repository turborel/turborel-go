<p align="center">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="artwork/turborel-logo-white.svg">
      <img src="artwork/turborel-logo.svg" alt="turborel" width="360">
    </picture>
</p>

> turborel-go is in alpha, meaning, version 0.0.0. Its version is the
> number of the Turborel release it is made for, and the alpha is made
> for Turborel 0.0.0, which speaks protocol version 1. Anything may
> change from one build to the next, API and behaviour alike. See
> [VERSIONING.md](VERSIONING.md). The latest Turborel alpha is built for
> Linux, macOS and Windows as the pre-release
> [nightly](https://github.com/turborel/turborel/releases/tag/nightly),
> and as the image `ghcr.io/turborel/turborel:nightly`.

turborel-go is the Go driver for
[Turborel](https://github.com/turborel/turborel), a relational database
system. It connects a Go program to a Turborel server,
`turboreld`, over Turborel's [native
protocol](https://github.com/turborel/turborel/blob/main/doc/client/05-protocol.md),
version 1, which it speaks itself, in Go alone, on the standard library, with no cgo.

Its standard interface is a `database/sql` driver, registered as `turborel`, and a native API beside it, cancelled through `context`. Beneath it is a low-level
client built to do all that Turborel's own client library,
turborel-client, does: TCP, Unix sockets and Windows named pipes, TLS,
SCRAM-SHA-256 and SCRAM-SHA-256-PLUS, prepared statements and cursors,
large objects, bulk copy, import and export, settings from a URI, the
environment and service and password files, several servers, pipelines
and pools.

The driver is built in milestones, M0 to M8.
[CONFORMANCE.md](CONFORMANCE.md) shows what is done, row by row, and
[TODO.md](TODO.md) what is open.

## Requirements

- Go 1.26 or later, on Linux, macOS or Windows ([RULES.md](RULES.md) D3).
- Go 1.26 and 1.27 are tested; no module beyond the standard library.
- A Turborel server of the same release, which speaks protocol version 1
  ([running the server](https://github.com/turborel/turborel/blob/main/doc/operator/02-turboreld.md)).

## Installing

No package is published yet. The driver's package, the module `github.com/turborel/turborel-go`, goes to
the Go module proxy, which takes the module's versions from its tags only once every Turborel driver is done, with the others
([VERSIONING.md](VERSIONING.md#publishing)). Until then, build the
driver from source:

```
git clone https://github.com/turborel/turborel-go
cd turborel-go
go test ./...
```

A program's module then takes the clone in by a `replace` directive,
beside a requirement of version 0.0.0:

```
go mod edit -require=github.com/turborel/turborel-go@v0.0.0 -replace=github.com/turborel/turborel-go=../turborel-go
```

## A first query

```go
import (
	"context"
	"database/sql"

	_ "github.com/turborel/turborel-go"
)

func staffName(ctx context.Context) (string, error) {
	db, err := sql.Open("turborel", "turborel://ann@db.example:7422/?tls_mode=verify-full")
	if err != nil {
		return "", err
	}
	defer db.Close()

	var name string
	err = db.QueryRowContext(ctx, "SELECT NAME FROM STAFF WHERE ID = ?", 2).Scan(&name)

	return name, err
}
```

## The manual

[doc/](doc/README.md) is the manual: a User's Guide, an API Reference
and Appendices, published at
https://turborel.com/docs/drivers/go/. Each
chapter is written as the milestone that fills it lands. The SQL a
program sends, and the server, are described in Turborel's own manual,
[The Turborel Documentation Set](https://github.com/turborel/turborel/blob/main/doc/README.md).
[VERSIONING.md](VERSIONING.md) tells how the driver's releases are
numbered and published.

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) before making a change: how a
change is made, its commit message, pull requests and issues. Every
change passes `go run ./internal/xtask verify`. [RULES.md](RULES.md) says how the
driver is designed, written and verified,
[CONFORMANCE.md](CONFORMANCE.md) is the parity matrix every Turborel
driver keeps, and [TODO.md](TODO.md) lists open work.

## Security

Report a vulnerability privately, never in a public issue, as
[SECURITY.md](SECURITY.md) says.

## License and trademark

The code is under the BSD-2-Clause Plus Patent License, in
[LICENSE](LICENSE). The name Turborel and its logos are not:
[TRADEMARK.md](TRADEMARK.md) says how they may be used.
