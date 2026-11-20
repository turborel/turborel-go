# Connecting

[Contents](README.md)

A program connects to a server, `turboreld`, as one of its users, and
the connection is then a session of the server's. This chapter tells
how, with the native API; the standard API's way of connecting comes at
milestone M7.

## Where the server is

A server takes connections over TCP, on port 7422 unless its operator
sets another, over a Unix socket, or on Windows a named pipe, and over
TLS, begun on its TCP port. `Connect` connects over TCP, `ConnectLocal`
over the socket or pipe, and `ConnectTo` to a `Target`: `TCP(address)`,
`Local(path)` or `TLS(address, serverName, config)`. An address with no
port reaches port 7422.

## Who the user is

Each way of connecting takes the user and a `Mechanism` that proves it:

- `Trust{}`: the server trusts the user as it names itself, as it does
  only of a client of its own machine, and only with SERVER.TRUST_LOCAL
  on.
- `Password(p)`: the password, sent whole, so only over TLS, the socket
  or the pipe.
- `NewScram(p)`: the password proved by SCRAM-SHA-256, so it never
  travels, and the server proves it knows the user's verifier too. Over
  TLS it is SCRAM-SHA-256-PLUS, bound to the connection. It too goes
  only over TLS, the socket or the pipe, unless `InTheClear` says
  otherwise, for a server whose SERVER.SCRAM_WITHOUT_TLS is on.
- `Certificate{}`: the certificate presented over TLS, whose common name
  names the user.

A password or SCRAM over plain TCP is refused before anything is sent.

```go
ctx := context.Background()

conn, err := turborel.Connect(ctx, "127.0.0.1:17433", "ANN", turborel.Trust{})
if err != nil {
	return err
}
fmt.Printf("%s, as %s\n", conn.Server(), conn.Authorization())
schema, _ := conn.Setting(turborel.StatusSchema)
zone, _ := conn.Setting("SESSION.TIME_ZONE")
fmt.Println(schema, zone)
fmt.Println(conn.Terminate())

config, err := turborel.Trusting(ca)
if err != nil {
	return err
}
target := turborel.TLS("127.0.0.1:17433", "localhost", config)
scram := turborel.NewScram("ann's secret")
secured, err := turborel.ConnectTo(ctx, target, "ANN", scram, nil)
if err != nil {
	return err
}
fmt.Println(scram.Name(), secured.Authorization())
fmt.Println(secured.Terminate())

_, err = turborel.Connect(ctx, "127.0.0.1:17433", "ANN", turborel.Password("ann's secret"))
fmt.Println(err)

_, err = turborel.ConnectLocal(ctx, socket, "ANN", turborel.NewScram("wrong"))
fmt.Println(err)
// turborel 0.0.0 (alpha), as ANN
// ANN +00:00
// <nil>
// scram-sha-256-plus ANN
// <nil>
// SQLCODE -919 (SQLSTATE 28000): password is sent only over TLS, a Unix socket or a named pipe, never over plain TCP
// SQLCODE -919 (SQLSTATE 28000): the user or the password is wrong
```

## TLS

A client's TLS trusts the authorities it is given: `Trusting(file)`,
those of a PEM file; `Roots(file, platform)`, a file's, the platform's,
or both; `Presenting(roots, chain, key)` presents a certificate chain
and key too, to log in by `Certificate{}`. The platform's authorities
are those of the files SSL_CERT_FILE and SSL_CERT_DIR name, if either is
set, else the system's. The server's certificate is checked against the
target's server name; a server not trusted fails the connection
(SQLSTATE 08006), saying why.

## How long a connection waits

`Config` says how a connection is made. Its `Timeouts` bound connecting,
beginning TLS and each step of the login by `Connect`, 30 seconds by
default, and each read or write after by `Read`, no limit by default; a
wait that runs out loses the connection, saying the server did not
answer in time. Its `Offer` says what the connection asks for: by
default every capability but compression; `NoOffer()` asks for none,
`Compressed()` adds compression, `WithAttribute` a connection attribute
the server shows in INFORMATION_SCHEMA.CONNECTION_ATTRIBUTES. A method
that needs a capability the server did not grant fails before it sends
anything.

```go
offer := turborel.DefaultOffer().WithAttribute(turborel.AttributeApplication, "payroll")
config := &turborel.Config{
	Timeouts: turborel.Timeouts{Connect: 5 * time.Second, Read: time.Minute},
	Offer:    &offer,
}
conn, err := turborel.ConnectTo(ctx, turborel.TCP("127.0.0.1:17433"), "ANN", turborel.Trust{}, config)
if err != nil {
	return err
}
fmt.Println(conn.ReadTimeout(), conn.Granted(turborel.CapabilityPing))
fmt.Println(conn.Ping(ctx))

none := turborel.NoOffer()
plain, err := turborel.ConnectTo(ctx, turborel.TCP("127.0.0.1:17433"), "ANN", turborel.Trust{}, &turborel.Config{Offer: &none})
if err != nil {
	return err
}
fmt.Println(plain.GrantedCapabilities(), len(plain.Settings()))
fmt.Println(plain.Ping(ctx))
// 1m0s true
// <nil>
// [] 0
// SQLCODE -919 (SQLSTATE 08006): the server did not grant ping
```

## Ending a connection

`Terminate` ends the session, its transaction as SESSION.COMMIT_AT_END
says, and waits for the server to close, at most the read timeout, else
60 seconds; a commit that fails then, as of a deferred constraint not
satisfied (SQLSTATE 40002), rolls the transaction back and is given.
`Close` closes the connection at once.

A statement a connection runs is cancelled from another goroutine by
its `Canceller`, over a connection of its own: `Cancel` gives up after
10 seconds, `CancelWithin` after a limit of its own. The statement fails
with SQLCODE -920, SQLSTATE TB920.
