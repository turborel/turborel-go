# Installing

[Contents](README.md)

The driver needs Go 1.26 or later, on Linux, macOS or Windows, and no
module beyond Go's standard library. It speaks Turborel's native
protocol, version 1, to a server of the same release.

The module is `github.com/turborel/turborel-go`, its package `turborel`.
No package is published until every Turborel driver is done; the Go
module proxy then takes the module's versions from its tags. Until then
the driver is built from its source, as the repository's
[README](../../README.md) says:

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

## A first program

Importing the package registers its `database/sql` driver as
`turborel`, opened by a connection URI. Outside a transaction each
statement is its own; every failure is an `*turborel.Error`.

```go
db, err := sql.Open("turborel", "turborel://ANN@127.0.0.1:17433/?tls_mode=disable")
if err != nil {
	return err
}
defer db.Close()

for _, stmt := range []string{
	"CREATE SCHEMA ANN",
	"CREATE TABLE STAFF (ID INTEGER GENERATED ALWAYS AS IDENTITY, NAME VARCHAR(20), SALARY DECIMAL(9,2))",
} {
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return err
	}
}

result, err := db.ExecContext(ctx, "INSERT INTO STAFF (NAME, SALARY) VALUES (?, ?), (?, ?)",
	"Ada", "61000.00", "Grace", 72000.5)
if err != nil {
	return err
}
rows, _ := result.RowsAffected()
last, _ := result.LastInsertId()
fmt.Println(rows, last)

var name string
var salary turborel.Decimal
err = db.QueryRowContext(ctx, "SELECT NAME, SALARY FROM STAFF WHERE ID = ?", 2).Scan(&name, &salary)
fmt.Println(name, salary, err)

tx, _ := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
tx.ExecContext(ctx, "DELETE FROM STAFF")
tx.Rollback()

var n int64
db.QueryRowContext(ctx, "SELECT COUNT(*) FROM STAFF").Scan(&n)
fmt.Println(n)

_, err = db.ExecContext(ctx, "SELECT * FROM NOPE")
var failure *turborel.Error
fmt.Println(errors.As(err, &failure), failure.SQLState)
// 2 2
// Grace 72000.50 <nil>
// 2
// true 42000
```

Here the server trusts a client of its own machine; over a network a
program proves its user by a password, over TLS ([Connecting](02-connecting.md),
[Settings](09-settings.md)). The rest of this guide uses the native API,
which does all `database/sql` does and more, and which a `database/sql`
connection reaches through `NativeConn`.
