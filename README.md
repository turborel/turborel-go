<p align="center">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="artwork/turborel-logo-white.svg">
      <img src="artwork/turborel-logo.svg" alt="turborel" width="360">
    </picture>
</p>

> turborel-go is in alpha, meaning, version 0.0.0. Anything may change
> from one build to the next, API and behaviour alike. Its version is
> the number of the turborel release it is made for, so the alpha
> speaks to turborel 0.0.0, over protocol version 1. See
> [VERSIONING.md](VERSIONING.md).

turborel-go is the Go driver for [turborel](https://github.com/turborel/turborel),
a relational database system. It speaks turborel's native protocol
itself, in Go alone, with nothing but the standard library: no cgo, no
native library, and no other module.

It is being built. This release holds the repository's checks and the
codec of the protocol's frame header, and nothing a program can call
yet. When it is done it gives:

- a `database/sql` driver, registered as `turborel`;
- a native API beside it, with every feature of turborel-client,
  turborel's reference client: TLS, SCRAM-SHA-256 and -PLUS, prepared
  statements and cursors, large objects, copy, import and export,
  settings by URI, service and password files, several servers,
  pipelines and pools;
- cancellation by `context`, sent to the server as the protocol's
  Cancel.

[CONFORMANCE.md](CONFORMANCE.md) is the checklist of each, and
[TODO.md](TODO.md) the order they are built in.

## Requirements

Go 1.26 or later. The driver is tested on Go 1.26 and 1.27, on Linux,
macOS and Windows (RULES.md D3).

## Installing

No release is published yet. Build it from source:

```
git clone https://github.com/turborel/turborel-go
cd turborel-go
go test ./...                         # the tests
go run ./internal/xtask verify        # every check a change must pass
```

## A first query

This is what the standard API will be once it is built (milestone M7);
none of it runs yet.

```go
import (
	"database/sql"

	_ "github.com/turborel/turborel-go"
)

db, err := sql.Open("turborel", "turborel://ann@db.example:7422/?tls_mode=verify-full")
if err != nil {
	return err
}
defer db.Close()

var name string
err = db.QueryRowContext(ctx, "SELECT NAME FROM STAFF WHERE ID = ?", 2).Scan(&name)
```

## Documents

- [doc/](doc/README.md): the manuals, a User's Guide, an API Reference
  and the Appendices, which turborel's docs site publishes too. Each
  chapter is written as the part it describes is finished.
- [The Native Protocol](https://github.com/turborel/turborel/blob/main/doc/client/05-protocol.md):
  the protocol it speaks.
- [CONFORMANCE.md](CONFORMANCE.md): what is implemented of the protocol
  and of turborel-client.
- [RULES.md](RULES.md): how the driver is designed, written and verified.
- [TODO.md](TODO.md): open work.
- [VERSIONING.md](VERSIONING.md): the version, and how a release is
  published.
- [CONTRIBUTING.md](CONTRIBUTING.md) and [SECURITY.md](SECURITY.md):
  contributing, and reporting an issue or a vulnerability.
- [LICENSE](LICENSE) and [TRADEMARK.md](TRADEMARK.md): the code is
  under the BSD-2-Clause Plus Patent License; the name Turborel and its
  logos are not, and TRADEMARK.md says how they may be used.
