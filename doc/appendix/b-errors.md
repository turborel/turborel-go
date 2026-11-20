# Appendix B: Errors

[Contents](README.md)

Every failure reaches a program as a `*turborel.Error`, whose SQLCODE
and SQLSTATE say what it is. Turborel's own [Appendix
A](https://github.com/turborel/turborel/blob/main/doc/appendix/a-sqlstate.md)
lists every SQLSTATE the server sends; this one lists the classes as
the driver meets them, and the failures it finds itself.

## The classes

| Class | What | The connection |
|---|---|---|
| 00, 01, 02 | Success, a warning, no data: a completion, not a failure; a warning is a condition or a notice | goes on |
| 08 | A connection exception | lost, but of 08003 |
| 22, 23 | A data exception; a constraint violated | goes on |
| 24, 26 | A cursor's state; a prepared statement of no such number | goes on |
| 28 | Credentials refused | lost, as the connection is made |
| 40 | A transaction rolled back, as a commit that failed | goes on |
| TB | Turborel's own: TB920, a statement cancelled; TBR01, a replica behind | goes on |

## Failures the driver finds itself

The driver fails as turborel-client fails the same case, and, where
that client has no such case, as every turborel driver does.

| SQLCODE | SQLSTATE | When |
|---|---|---|
| -919 | 08006 | A reply malformed or longer than the longest taken; a wait run out; a capability not granted, the connection kept; the server gone; a TLS handshake failed; an export of no query, or whose write fails; input to ReadNative that is not native rows; more than 32 connection or query attributes, or one of a name not 1 to 64 bytes or a value of more than 1024, refused before anything is sent, the connection kept, as the server would fail the connection |
| -919 | 28000 | A password or SCRAM refused over plain TCP before connecting; SCRAM's exchange refused, as of a server not proving it knows the user's verifier |
| -910 | TB910 | The program's misuse that turborel-client has no case of: a row sent to a copy ended, or a copy finished twice; a delimiter that is not one ASCII character other than a quote or a line break; a TLS target with no TLS configuration, or a target of no kind |
| -900 | 0A000 | Past turborel's limits: a JSON value nested deeper than 128, or stored in more than 256 MiB, given to NewJSON |
| -301 | 22000 | Input an import cannot read, or that it refuses before SQL does: the cause of an ImportError of the source's input, and a SourceError |
| -301 | 22003 | A number the protocol cannot carry, given to NewDecimal, NewDecFloat or NewInterval; text that is no UUID |
| -301 | 22032 | A JSON value that is not one, given to JSONOf |
| -301 | 2200N | Text that is no XML content, given to an XML value's Nodes |
| -301 | 22001 | A string an import assigns to a character string column, of more than spaces beyond its length |
