# Settings

[Contents](README.md)

A program need not name a server in its code. `Settings` say where the
servers are, as whom to connect, how, and what a server must be, and
`Settings.Connect` connects as they say. They are read from layers, each
setting taken from the first layer that has it: the program's own
`Options`, or a `turborel://` URI; then a section of the service file;
then the environment's TURBOREL_ variables; then the defaults.
[Appendix C](../appendix/c-settings.md) lists every setting.

```go
uri := "turborel://ANN:ann%27s%20secret@127.0.0.1:17433/?tls_mode=verify-full" +
	"&tls_roots=" + dir + "/certs/ca.pem&server_name=localhost&application_name=payroll"
settings, err := turborel.SettingsFromURI(uri)
if err != nil {
	return err
}
fmt.Println(settings)
conn, err := settings.Connect(ctx)
if err != nil {
	return err
}
role, _ := conn.Role(ctx)
fmt.Println(conn.Authorization(), role)
// Settings{hosts: 127.0.0.1:17433, user: ANN, tls_mode: verify-full, target: any, ..}
// ANN read-write
```

A URI's user, password, hosts and values are percent-encoded, so a host
`%2Frun%2Fturborel.sock` names a Unix socket, and an IPv6 address is
written in brackets. Settings never show the password.

## The service file and the password file

A service file holds sections, each `[name]` on a line of its own, then
lines `setting = value`; a line beginning with `#` is a comment. The
setting `service` names the section; `service_file` the file, else
`~/.turborel_service.conf`.

A password file, `~/.turborel_pass` unless `passfile` names another,
holds lines `host:port:user:password`, `*` matching anything and `\`
taking the character after it as it is. The password of the first line
that matches the server and user is proved, unless the settings give
one. The driver reads it only if it is a regular file; it should be
readable by its owner alone.

```go
services := dir + "/services.conf"
os.WriteFile(services, []byte("[payroll]\nhost = 127.0.0.1:1, 127.0.0.1:17433\ntls_mode = require\n"), 0o600)
passfile := dir + "/pass"
os.WriteFile(passfile, []byte("127.0.0.1:17433:ANN:ann's secret\n"), 0o600)
var options turborel.Options
options.Set(turborel.OptionService, "payroll")
options.Set(turborel.OptionServiceFile, services)
options.Set(turborel.OptionPassfile, passfile)
options.Set(turborel.OptionUser, "ANN")
settings, err = turborel.Resolve(options)
if err != nil {
	return err
}
fmt.Println(settings.Hosts)
conn, err = settings.Connect(ctx)
fmt.Println(err)
// [127.0.0.1:1 127.0.0.1:17433]
// <nil>
```

## Several servers

The hosts are tried in the order given, or with `load_balance=random`
in an order chosen at random for each connection, and each address a
name resolves to in turn, each step within the connect timeout. A
server that cannot be reached, refuses the user, or is not of the role
`target` asks is passed for the next: `read-write` takes a server alone
or its group's primary, `read-only` a replica following its group. With
none kept, the failure names each address and why.

```go
options.Set(turborel.OptionTarget, "read-only")
settings, _ = turborel.Resolve(options)
_, err = settings.Connect(ctx)
fmt.Println(err)
// SQLCODE -919 (SQLSTATE 08001): no server was kept: 127.0.0.1 (127.0.0.1:1): cannot connect: dial tcp 127.0.0.1:1: connect: connection refused; 127.0.0.1 (127.0.0.1:17433): the server is read-write, and read-only is asked
```

## TLS and channel binding

`tls_mode` says how a connection over TCP uses TLS: `disable`, never;
`prefer`, the default, TLS if the server takes it, else plain TCP;
`require`, TLS or no connection; `verify-ca`, the server's certificate
signed by an authority `tls_roots` or `tls_system_roots` trusts;
`verify-full`, of the server's name too, `server_name`'s if given. Under
`prefer` and `require` the certificate is not checked, so they keep out
an eavesdropper but not a server in the middle. A Unix socket or named
pipe is private, and uses no TLS.

A password is proved by SCRAM-SHA-256, bound to the TLS channel unless
`channel_binding=disable`; `channel_binding=require` refuses, before it
proves anything, a connection that cannot bind. A password over plain
TCP is refused before it is sent. With no password, a client certificate
named by `tls_certificate` and `tls_key` proves the user, else trust.
Every connection the same settings make shares one SCRAM key cache, so
a connection again skips deriving the password's keys.
