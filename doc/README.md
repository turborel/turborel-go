# The turborel-go Documentation Set

turborel-go is the Go driver for turborel, a relational database system.
A Go program uses it to connect to a turborel server, `turboreld`, and to
run SQL there, through Go's standard `database/sql` package or through
the driver's native API. It speaks turborel's native protocol itself, in
Go alone. This set of manuals describes the driver for those who write
such programs.

turborel-go is in alpha: what these manuals describe may change in any
release until 1.0.0 ([Versions](appendix/d-versions.md)). Each change
updates the pages it touches, so the manuals describe the release they
come with. The driver is being built, and a chapter is written as the
part it describes is finished; until then it says what it will cover.
What is implemented, feature by feature, is in
[CONFORMANCE.md](../CONFORMANCE.md).

## The volumes

| Volume | For | |
|---|---|---|
| [I. User's Guide](user/README.md) | Anyone who writes a Go program against turborel | Installing, connecting, and running SQL, chapter by chapter |
| [II. API Reference](client/README.md) | The same, looking something up | The `database/sql` driver and the native API, a page for each part |
| [Appendices](appendix/README.md) | Everyone | Types, errors, connection settings, versions |

Read the User's Guide from the start to learn the driver; turn to the
API Reference and the appendices to look a thing up. SQL itself, and the
server, are described in turborel's own manuals.

## Conventions

**Examples** are Go, set in a fixed-width font. What an example prints
follows it, as a comment:

```go
fmt.Println(turborel.Version)
// 0.0.0
```

**SQL** in examples is written as turborel's manuals write it, key words
and names in upper case:

```sql
SELECT NAME, SALARY FROM STAFF WHERE SALARY > 50000;
```

**Errors** are given by SQLSTATE, the code the SQL standard defines, and
SQLCODE, the older number. Every failure the driver returns is a
`*turborel.Error` that carries both ([Appendix B](appendix/b-errors.md)).

**References** to turborel's manuals name the page and its volume, as
The Native Protocol, in turborel's Volume IV, Client Reference.
