# TODO

Open work, by milestone, in the order it is built. Each item says what
is missing. How the driver is built is in [RULES.md](RULES.md), and the
feature-by-feature checklist is [CONFORMANCE.md](CONFORMANCE.md).
Finished work leaves this file, and git keeps its history.

Code never cites this file (R-DOC-5).

[M1](#m1-codec) · [M2](#m2-connect) · [M3](#m3-statements) ·
[M4](#m4-large-objects-and-bulk) · [M5](#m5-settings) ·
[M6](#m6-throughput) · [M7](#m7-standard-api) ·
[M8](#m8-parity-audit)

---

## M1. Codec

- Fields: integers, flags, strings, bytes, lists and keys, with every
  count checked against what remains before it allocates.
- The tail, its tags in increasing order, and the attributes' limits.
- The 27 data types and the values of each tag, at their edges: i128
  limits, scales, NaN and −0.0, DECFLOAT, each interval qualifier,
  displacements −12:59 to +14:00, nested arrays, rows, multisets and
  structured values, locators, UUID (tag 27), each XML modifier.
- JSON values as D89's binary tree, decoded to a Go value and encoded
  from one.
- Every request and reply, and the composite fields: row, column,
  parameter and failure.
- Compressed frames (`z`), inflated to at most their stated length.
- `Error` given the rest of the failure: the rule cited, the position,
  whether a transaction is active, and the diagnostics of tags 7 to 15.
- The spec's worked example as a test of whole messages.

## M2. Connect

- TCP on 7422, the Unix socket, and the Windows named pipe
  `\\.\pipe\turborel`.
- TLS begun on the same port, by `crypto/tls`.
- The mechanisms trust, password and certificate; SCRAM-SHA-256 and
  -PLUS, by `tls-exporter` on TLS 1.3 and `tls-server-end-point` on 1.2;
  secrets refused over plain TCP (SQLSTATE 28000); the SCRAM key cache.
- Hello asking for every capability but compression; Ready; Status.
- Timeouts: connect 30 s, read unlimited by default; terminate within
  60 s; cancel by key on its own connection within 10 s.
- Live tests against the image `ghcr.io/turborel/turborel:nightly` in
  CI, with a server set up with test certificates, SCRAM users and
  SERVER.TRUST_LOCAL, kept in `test/server/`; whether macOS and Windows
  build `turboreld` for them or run on Linux alone is the owner's call.

## M3. Statements

- Query, and streamed query; Prepare, Execute, Fetch, Close and
  Deallocate; numbered cursors; Describe.
- Errors, diagnostics, notices (the last 64 kept, or a handler) and
  conditions; `in_transaction`; autocommit by `ends`; fetch-ends.
- Completion details: rows affected, generated values, server time and
  commit position.
- Multi-statement, query attributes, Ping, Reset and ChangeUser.

## M4. Large objects and bulk

- Locators, reads of 256 KiB chunks by octets or characters, create,
  append, free and hold; a reader and a writer.
- CopyIn, CopyData, CopyEnd and abandon; CSV and TSV import; CSV and
  native export.

## M5. Settings

- The `turborel://` URI, options, the TURBOREL_* variables, the service
  file and the password file, read as turborel-client reads them.
- Several hosts, `load_balance`, `target` by SERVER_READ_ONLY,
  `tls_mode`, `channel_binding` and `application_name`.

## M6. Throughput

- Pipelines with a 64 KiB window, and skip-on-error by savepoints.
- Pools with PoolConfig's defaults, a Ping check and a Reset return.
- The SQL-agent: CONNECT, SET CONNECTION and DISCONNECT.

## M7. Standard API

- The `database/sql/driver` driver registered as `turborel`, with
  `context` cancellation sent as Cancel (RULES.md D2).
- The manual, `doc/`: every chapter not filled by an earlier milestone
  written, and every example run.

## M8. Parity audit

- Every row of CONFORMANCE.md checked against turborel-client and the
  spec, the coverage floor held, and this file empty.
