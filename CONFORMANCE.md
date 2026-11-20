# Conformance

The parity matrix: every part of the native protocol, version 1, and
every feature of turborel-client, the reference client, with what
turborel-go implements of each. The matrix is the same in every turborel
driver. Its rows follow The Native Protocol (`doc/client/05-protocol.md`),
The Client Library (`doc/client/02-library.md`) and Settings, Pipelines
and Pools (`doc/client/02-library-connections.md`), in their order, with
the three places the reference code goes beyond the spec marked as such.
A row is "done" once implemented and tested, with the tests' citation
named; until then it is "not yet".

[Frames](#frames) · [Fields](#fields) · [Data types](#data-types) ·
[Values](#values) · [Composite fields](#composite-fields) ·
[The tail](#the-tail) · [Opening a connection](#opening-a-connection) ·
[Capabilities](#capabilities) · [Requests](#requests) ·
[Replies](#replies) · [Session status](#session-status) ·
[Errors and notices](#errors-and-notices) ·
[The Client Library](#the-client-library) ·
[Settings, Pipelines and Pools](#settings-pipelines-and-pools)

---

## Frames

| Item | Spec | Status |
|---|---|---|
| Frame header: kind `u8`, length `u32` little-endian | [PROTOCOL Frames] | done: `frame.go`, `frame_test.go` |
| A kind not known, a body cut short, or bytes left over fail the connection (-919, 08006) | [PROTOCOL Frames] | not yet |
| A body over the longest taken, 16 MiB by default, fails the connection | [PROTOCOL Frames] | done: `frame.go`, `frame_test.go` |
| The longest reply taken, set by the program | [CLIENT Timeouts] | not yet |
| Compressed frame `z`, a zlib stream, inflated to at most its length | [PROTOCOL Compressed frames] | not yet |
| A `z` in a `z`, a wrong inflated length, or `z` not granted fail the connection | [PROTOCOL Compressed frames] | not yet |
| Compression threshold of the client's own | [PROTOCOL Compressed frames] | not yet |

## Fields

| Item | Spec | Status |
|---|---|---|
| `u8`, `u32`, `u64`, little-endian | [PROTOCOL Values and fields] | not yet |
| `i128`, two's complement | [PROTOCOL Values and fields] | not yet |
| flag, any byte but 0 or 1 refused | [PROTOCOL Values and fields] | not yet |
| string, UTF-8 with a `u32` length | [PROTOCOL Values and fields] | not yet |
| bytes | [PROTOCOL Values and fields] | not yet |
| list and strings, a count larger than what follows failing with no reservation | [PROTOCOL Values and fields] | not yet |
| key, 16 bytes | [PROTOCOL Values and fields] | not yet |

## Data types

| Tag | Type | Spec | Status |
|---|---|---|---|
| 1 | CHARACTER, length and units | [PROTOCOL Data types] | not yet |
| 2 | exact numeric, precision and scale | [PROTOCOL Data types] | not yet |
| 3 | approximate numeric | [PROTOCOL Data types] | not yet |
| 4 | a plug-in's type, by name and parameters | [PROTOCOL Data types] | not yet |
| 5 | CHARACTER VARYING | [PROTOCOL Data types] | not yet |
| 6 | BINARY | [PROTOCOL Data types] | not yet |
| 7 | BINARY VARYING | [PROTOCOL Data types] | not yet |
| 8 | DATE | [PROTOCOL Data types] | not yet |
| 9 | TIME | [PROTOCOL Data types] | not yet |
| 10 | TIME WITH TIME ZONE | [PROTOCOL Data types] | not yet |
| 11 | TIMESTAMP | [PROTOCOL Data types] | not yet |
| 12 | TIMESTAMP WITH TIME ZONE | [PROTOCOL Data types] | not yet |
| 13 | INTERVAL, its fields 0 YEAR to 5 SECOND and precisions | [PROTOCOL Data types] | not yet |
| 14 | BOOLEAN | [PROTOCOL Data types] | not yet |
| 15 | DECFLOAT | [PROTOCOL Data types] | not yet |
| 16 | array, cardinality and element type | [PROTOCOL Data types] | not yet |
| 17 | row, named fields | [PROTOCOL Data types] | not yet |
| 18 | CHARACTER LARGE OBJECT | [PROTOCOL Data types] | not yet |
| 19 | BINARY LARGE OBJECT | [PROTOCOL Data types] | not yet |
| 22 | multiset | [PROTOCOL Data types] | not yet |
| 23 | structured type and its supertypes | [PROTOCOL Data types] | not yet |
| 24 | reference type, scoped or not | [PROTOCOL Data types] | not yet |
| 25 | JSON | [PROTOCOL Data types] | not yet |
| 26 | XML, its modifiers and XMLSCHEMA | [PROTOCOL Data types] | not yet |
| 27 | UUID, no parameters; beyond the spec (turborel RULES.md D184, `protocol/src/fields.rs`) | [PROTOCOL Data types] | not yet |
| | A distinct type, written as its source type | [PROTOCOL Data types] | not yet |
| | A plug-in's type the client does not have, read by name and shown by its text | [PROTOCOL Data types] | not yet |

## Values

| Tag | Value | Spec | Status |
|---|---|---|---|
| 0 | null | [PROTOCOL Values] | not yet |
| 1 | character string, CHARACTER VARYING's too | [PROTOCOL Values] | not yet |
| 2 | exact number, `i128` units and scale, to `math/big` | [PROTOCOL Values] | not yet |
| 3 | approximate number, NaN and −0.0 kept | [PROTOCOL Values] | not yet |
| 4 | a plug-in's value: name, text and bytes | [PROTOCOL Values] | not yet |
| 6 | binary string | [PROTOCOL Values] | not yet |
| 8 | date, as a timestamp | [PROTOCOL Values] | not yet |
| 9 | time, as a timestamp | [PROTOCOL Values] | not yet |
| 11 | timestamp: zoned, precision, displacement, microseconds in UTC | [PROTOCOL Values] | not yet |
| 13 | interval: qualifier, then months or microseconds | [PROTOCOL Values] | not yet |
| 14 | truth value; unknown as null | [PROTOCOL Values] | not yet |
| 15 | decimal floating point, coefficient and exponent | [PROTOCOL Values] | not yet |
| 16 | array, nested | [PROTOCOL Values] | not yet |
| 17 | row | [PROTOCOL Values] | not yet |
| 20 | large object, whole | [PROTOCOL Values] | not yet |
| 21 | large object, by locator, with its lengths | [PROTOCOL Values] | not yet |
| 22 | multiset | [PROTOCOL Values] | not yet |
| 23 | structured value, its most specific type and attributes | [PROTOCOL Values] | not yet |
| 24 | reference | [PROTOCOL Values] | not yet |
| 25 | JSON, D89's binary tree (turborel RULES.md D89, `src/json/binary.rs`), decoded and encoded; beyond the spec | [PROTOCOL Values] | not yet |
| 26 | XML, its serialization as CONTENT | [PROTOCOL Values] | not yet |
| 27 | UUID, bytes of 16 octets; beyond the spec (turborel RULES.md D184) | [PROTOCOL Values] | not yet |

## Composite fields

| Item | Spec | Status |
|---|---|---|
| row | [PROTOCOL Composite fields] | not yet |
| column: name, data type, not null | [PROTOCOL Composite fields] | not yet |
| parameter: varying flag and data type | [PROTOCOL Composite fields] | not yet |
| failure: SQLCODE, SQLSTATE, message, rule, position, active, tail 7 to 15 | [PROTOCOL Composite fields] | not yet |

## The tail

| Tag | Field | Spec | Status |
|---|---|---|---|
| | Tags in increasing order; a tag not taken, or not past the last, fails the connection | [PROTOCOL The tail] | not yet |
| 1 | attributes, at most 32, names 1 to 64 bytes, values at most 1024 | [PROTOCOL The tail] | not yet |
| 2 | multi | [PROTOCOL The tail] | not yet |
| 3 | cursor | [PROTOCOL The tail] | not yet |
| 4 | position | [PROTOCOL The tail] | not yet |
| 5 | more | [PROTOCOL The tail] | not yet |
| 6 | rows | [PROTOCOL The tail] | not yet |
| 7 | severity | [PROTOCOL The tail] | not yet |
| 8 | detail | [PROTOCOL The tail] | not yet |
| 9 | hint | [PROTOCOL The tail] | not yet |
| 10 | context | [PROTOCOL The tail] | not yet |
| 11 | schema | [PROTOCOL The tail] | not yet |
| 12 | table | [PROTOCOL The tail] | not yet |
| 13 | column | [PROTOCOL The tail] | not yet |
| 14 | data type | [PROTOCOL The tail] | not yet |
| 15 | constraint | [PROTOCOL The tail] | not yet |
| 16 | generated | [PROTOCOL The tail] | not yet |
| 17 | conditions, each a failure as bytes | [PROTOCOL The tail] | not yet |
| 18 | time: total, queued, locks | [PROTOCOL The tail] | not yet |

## Opening a connection

| Item | Spec | Status |
|---|---|---|
| Hello, Challenge and Answer steps, Status, Ready | [PROTOCOL Opening a connection] | not yet |
| TCP on port 7422 | [PROTOCOL Opening a connection] | not yet |
| Unix socket | [CLIENT Connecting] | not yet |
| Windows named pipe `\\.\pipe\turborel` | [CLIENT Connecting] | not yet |
| TLS begun on the same port | [PROTOCOL Opening a connection] | not yet |
| Mechanism `trust` | [PROTOCOL Mechanisms] | not yet |
| Mechanism `password` | [PROTOCOL Mechanisms] | not yet |
| Mechanism `certificate` | [PROTOCOL Mechanisms] | not yet |
| Mechanism `scram-sha-256` (RFC 5802, RFC 7677) | [PROTOCOL Mechanisms] | not yet |
| Mechanism `scram-sha-256-plus` by `tls-exporter` on TLS 1.3 | [PROTOCOL Mechanisms] | not yet |
| Mechanism `scram-sha-256-plus` by `tls-server-end-point` on TLS 1.2 | [PROTOCOL Mechanisms] | not yet |

## Capabilities

| Capability | Spec | Status |
|---|---|---|
| `notices` | [PROTOCOL Capabilities] | not yet |
| `status` | [PROTOCOL Capabilities] | not yet |
| `diagnostics` | [PROTOCOL Capabilities] | not yet |
| `describe` | [PROTOCOL Capabilities] | not yet |
| `cursors` | [PROTOCOL Capabilities] | not yet |
| `ping` | [PROTOCOL Capabilities] | not yet |
| `reset` | [PROTOCOL Capabilities] | not yet |
| `change-user` | [PROTOCOL Capabilities] | not yet |
| `query-attributes` | [PROTOCOL Capabilities] | not yet |
| `connection-attributes` | [PROTOCOL Capabilities] | not yet |
| `multi-statement` | [PROTOCOL Capabilities] | not yet |
| `compression` | [PROTOCOL Capabilities] | not yet |
| `commit-position` | [PROTOCOL Capabilities] | not yet |
| `copy` | [PROTOCOL Capabilities] | not yet |
| `row-counts` | [PROTOCOL Capabilities] | not yet |
| `generated-values` | [PROTOCOL Capabilities] | not yet |
| `fetch-ends` | [PROTOCOL Capabilities] | not yet |
| `conditions` | [PROTOCOL Capabilities] | not yet |
| | No request or tail field of a capability not granted is sent | [PROTOCOL Capabilities] | not yet |

## Requests

| Request | Spec | Status |
|---|---|---|
| Hello `H` | [PROTOCOL Hello 'H'] | not yet |
| Answer `A` | [PROTOCOL Answer 'A'] | not yet |
| Query `Q` | [PROTOCOL Query 'Q'] | not yet |
| Prepare `P` | [PROTOCOL Prepare 'P'] | not yet |
| Execute `E` | [PROTOCOL Execute 'E'] | not yet |
| Fetch `F` | [PROTOCOL Fetch 'F'] | not yet |
| Close `C` | [PROTOCOL Close 'C'] | not yet |
| Deallocate `D` | [PROTOCOL Deallocate 'D'] | not yet |
| Lob `L`: read, create, append, free, hold | [PROTOCOL Lob 'L'] | not yet |
| Cancel `Z` | [PROTOCOL Cancel 'Z'] | not yet |
| Terminate `X` | [PROTOCOL Terminate 'X'] | not yet |
| Describe `Y` | [PROTOCOL Describe 'Y'] | not yet |
| Ping `G` | [PROTOCOL Ping 'G'] | not yet |
| Reset `W` | [PROTOCOL Reset 'W'] | not yet |
| ChangeUser `U` | [PROTOCOL ChangeUser 'U'] | not yet |
| CopyIn `I` | [PROTOCOL CopyIn 'I'] | not yet |
| CopyData `J` | [PROTOCOL CopyData 'J'] | not yet |
| CopyEnd `N` | [PROTOCOL CopyEnd 'N'] | not yet |

## Replies

| Reply | Spec | Status |
|---|---|---|
| Challenge `C` | [PROTOCOL Challenge 'C'] | not yet |
| Ready `R` | [PROTOCOL Ready 'R'] | not yet |
| Prepared `P` | [PROTOCOL Prepared 'P'] | not yet |
| Description `D` | [PROTOCOL Description 'D'] | not yet |
| Rows `B` | [PROTOCOL Rows 'B'] | not yet |
| Suspended `S` | [PROTOCOL Suspended 'S'] | not yet |
| Complete `K` | [PROTOCOL Complete 'K'] | not yet |
| Error `E` | [PROTOCOL Error 'E'] | not yet |
| Lob `L`: data, locator | [PROTOCOL Lob 'L'] | not yet |
| Notice `N` | [PROTOCOL Notice 'N'] | not yet |
| Status `T` | [PROTOCOL Status 'T'] | not yet |
| Pong `G` | [PROTOCOL Pong 'G'] | not yet |
| Replies in their fixed order: rows, notices, statuses, then the last | [PROTOCOL Conversations] | not yet |

## Session status

| Name | Spec | Status |
|---|---|---|
| AUTHORIZATION | [PROTOCOL Status 'T'] | not yet |
| SCHEMA | [PROTOCOL Status 'T'] | not yet |
| READ_ONLY | [PROTOCOL Status 'T'] | not yet |
| SERVER_VERSION | [PROTOCOL Status 'T'] | not yet |
| Each setting a session may set for itself | [PROTOCOL Status 'T'] | not yet |
| SERVER_READ_ONLY, YES or NO; beyond the spec (turborel `server/src/native/status.rs`) | [PROTOCOL Status 'T'] | not yet |
| CATALOG, the database's catalog name; beyond the spec | [PROTOCOL Status 'T'] | not yet |
| OWNER, YES or NO; beyond the spec | [PROTOCOL Status 'T'] | not yet |

## Errors and notices

| Item | Spec | Status |
|---|---|---|
| Severity ERROR, FATAL, WARNING, NOTICE | [PROTOCOL Errors and notices] | not yet |
| Conditions in Complete with `conditions`, else as Notices | [PROTOCOL Errors and notices] | not yet |
| An Error of class 08 but 08003, or of class 28 at login, ends the connection | [PROTOCOL Error 'E'] | not yet |
| 01T01, Reset or ChangeUser rolled back a transaction | [PROTOCOL Errors and notices] | not yet |
| TBR01, a replica behind REPLICATION.READ_AFTER | [PROTOCOL Errors and notices] | not yet |
| The worked example, byte for byte | [PROTOCOL A worked example] | not yet; its frame headers done: `frame_test.go` |

## The Client Library

| Feature | Client | Status |
|---|---|---|
| Connect over TCP, a Unix socket or a named pipe, to a target, with TLS or not | [CLIENT Connecting] | not yet |
| Mechanisms Scram, Password, Certificate and Trust | [CLIENT Connecting] | not yet |
| SCRAM in the clear only when asked, for SERVER.SCRAM_WITHOUT_TLS | [CLIENT Connecting] | not yet |
| Password and SCRAM refused over plain TCP before connecting (28000) | [CLIENT Connecting] | not yet |
| A stream of the program's own, its channel binding given | [CLIENT Connecting] | not yet |
| TLS roots: a file, the platform's, or both; a client certificate presented | [CLIENT Connecting] | not yet |
| `terminate`, as SESSION.COMMIT_AT_END says, within 60 s | [CLIENT Connecting] | not yet |
| Timeouts: connect 30 s, read unlimited; a wait run out loses the connection (08006) | [CLIENT Timeouts] | not yet |
| Replies of at most 16 MiB, or the program's limit | [CLIENT Timeouts] | not yet |
| `query`: rows or completion; a failure with rule and position | [CLIENT Running statements] | not yet |
| `query_into`: rows to a sink, a batch at a time | [CLIENT Running statements] | not yet |
| Values: exact, approximate, character, binary, datetime, interval, plug-in, null | [CLIENT Running statements] | not yet |
| `in_transaction` | [CLIENT Running statements] | not yet |
| Cancel from another goroutine, within 10 s or a limit of its own | [CLIENT Running statements] | not yet |
| Autocommit by `ends` | [CLIENT Running statements] | not yet |
| `prepare`, `execute`, `parameters`, `columns` | [CLIENT Prepared statements] | not yet |
| `open_cursor`, `fetch`, `close`, `deallocate` | [CLIENT Prepared statements] | not yet |
| Large objects whole, or by locator | [CLIENT Large objects] | not yet |
| `read_lob` by octets or characters, 256 KiB at a time; a reader | [CLIENT Large objects] | not yet |
| `free_lob`, `hold_lob` | [CLIENT Large objects] | not yet |
| `create_lob` writer, its `finish` a parameter; a character split between writes | [CLIENT Large objects] | not yet |
| XML values: text, parameters | [CLIENT XML values] | not yet |
| UUIDs: octets, text, parameters | [CLIENT UUIDs] | not yet |
| Import of a source: CSV and TSV dialects, batches, skip | [CLIENT Importing] | not yet |
| An import's failure: where, why, rows committed and rolled back | [CLIENT Importing] | not yet |
| Every capability asked for but compression; an offer of the program's own; connection attributes; `granted` | [CLIENT What a connection asks for] | not yet |
| A method needing a capability not granted fails before sending | [CLIENT What a connection asks for] | not yet |
| `setting` and `settings`, as the server told them | [CLIENT What the server tells] | not yet |
| Notices to a handler, or the last 64 kept | [CLIENT What the server tells] | not yet |
| `conditions` of the last statement | [CLIENT What the server tells] | not yet |
| `rows_affected`, `last_generated`, `server_time`, `commit_position` | [CLIENT What the server tells] | not yet |
| Diagnostics of a failure | [CLIENT What the server tells] | not yet |
| A cursor under autocommit ending its transaction with `fetch-ends` | [CLIENT What the server tells] | not yet |
| `ping`, `reset`, `change_user` | [CLIENT Controlling the session] | not yet |
| Query attributes on every later statement | [CLIENT Controlling the session] | not yet |
| `query_many` and its `Ran` | [CLIENT Controlling the session] | not yet |
| `describe`, numbered cursors, `describe_cursor` | [CLIENT Controlling the session] | not yet |
| `copy_in`, `send`, `finish`, abandon on drop; `copy_rows` | [CLIENT Copying and exporting] | not yet |
| Import by copy where granted and no column is a large object | [CLIENT Copying and exporting] | not yet |
| Export as CSV, or native frames, read back by `read_native` | [CLIENT Copying and exporting] | not yet |
| The SQL-agent: CONNECT, SET CONNECTION, DISCONNECT, by a connector | [CLIENT The SQL-agent] | not yet |

## Settings, Pipelines and Pools

| Feature | Client | Status |
|---|---|---|
| Settings connect as they say; password by SCRAM, else certificate, else trust | [CLIENT Connecting by settings] | not yet |
| `host`, `port`, `user`, `password`, with their defaults | [CLIENT Where settings come from] | not yet |
| `tls_mode`, `channel_binding`, `application_name`, `connect_timeout` | [CLIENT Where settings come from] | not yet |
| `service`, `service_file`, `passfile`, `target`, `load_balance` | [CLIENT Where settings come from] | not yet |
| `tls_roots`, `tls_system_roots`, `tls_certificate`, `tls_key`, `server_name` | [CLIENT Where settings come from] | not yet |
| Layers: the program's, the service file's, the environment's, the defaults | [CLIENT Where settings come from] | not yet |
| TURBOREL_* variables; empty gives nothing; an environment of the program's | [CLIENT Where settings come from] | not yet |
| Refusals with 08001; `application_name` sent as the attribute `application` | [CLIENT Where settings come from] | not yet |
| The `turborel://` URI, percent-encoded, IPv6 in brackets, refusals | [CLIENT The connection URI] | not yet |
| The service file: sections, comments, refusals naming file and line | [CLIENT The service file] | not yet |
| The password file: fields, `*`, `\`, comments, first match, sockets by path | [CLIENT The password file] | not yet |
| Several hosts, in order or `random`; each address tried; failures named (08001, 28000) | [CLIENT Several servers] | not yet |
| `target` `read-write` or `read-only` by SERVER_READ_ONLY, else INFORMATION_SCHEMA.REPLICAS; `role` | [CLIENT Several servers] | not yet |
| `tls_mode` `disable`, `prefer`, `require`, `verify-ca`, `verify-full`; `prefer` retried in plain TCP | [CLIENT TLS by mode] | not yet |
| `channel_binding` `disable`, `prefer`, `require` | [CLIENT TLS by mode] | not yet |
| Pipelines: queue, run, an outcome each, 64 KiB windows | [CLIENT Pipelines] | not yet |
| Pipelines: skip on error by savepoints, one transaction | [CLIENT Pipelines] | not yet |
| Pools: `get`, return on release, PoolConfig's defaults | [CLIENT Pools] | not yet |
| Pools: Ping check, Reset return, else ROLLBACK and a pipeline | [CLIENT Pools] | not yet |
| Pools: a source of the program's; no goroutine started | [CLIENT Pools] | not yet |
| SCRAM key cache: salt, iterations and a keyed tag; 64 entries; dropped on failure | [CLIENT The SCRAM key cache] | not yet |
