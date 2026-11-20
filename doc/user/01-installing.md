# Installing

[Contents](README.md)

This chapter will tell what the driver needs, Go 1.26 or later on Linux,
macOS or Windows, and how a program takes the module `github.com/turborel/turborel-go` in from
the Go module proxy, which takes the module's versions from its tags. No package is published until every Turborel driver is
done; until then the driver is built from its source, as the
repository's
[README](../../README.md)
says. The chapter is written with the standard API, at milestone M7, and
again when the packages are published.

Built from source, the driver is cloned and tested:

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
