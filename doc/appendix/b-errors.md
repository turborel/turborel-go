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

| SQLCODE | SQLSTATE | When |
|---|---|---|
| -919 | 08006 | A reply malformed or longer than the longest taken; a wait run out; a capability not granted; the server gone; a TLS handshake failed; a row sent to a copy ended; an export of no query, or whose write fails; input to ReadNative that is not native rows |
| -919 | 28000 | A password or SCRAM refused over plain TCP before connecting; SCRAM's exchange refused, as of a server not proving it knows the user's verifier |
| -301 | 22003 | A number the protocol cannot carry, given to NewDecimal, NewDecFloat or NewInterval; text that is no UUID |
| -301 | 22032 | A JSON value that is not one, given to NewJSON or JSONOf |
| -301 | 2200N | Text that is no XML content, given to an XML value's Nodes |
| -301 | 22001 | A string an import assigns to a character string column, of more than spaces beyond its length |
