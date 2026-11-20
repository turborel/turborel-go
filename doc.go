// Package turborel is the Go driver for turborel, a relational database
// system. It speaks turborel's native protocol, version 1, itself, in Go
// alone, with nothing but the standard library (RULES.md D1).
//
// The driver is in alpha, version 0.0.0, and is being built. This
// release holds the frame header's codec and nothing a program can
// call yet. When it is done it gives two interfaces over one connection:
//
//   - a database/sql driver registered as "turborel", so a program
//     opens a server with sql.Open("turborel", "turborel://...");
//   - a native API beside it, with every feature of turborel-client,
//     turborel's reference client in Rust.
//
// A context canceled while a statement runs cancels it on the server,
// by the protocol's Cancel request (RULES.md D2).
//
// Every failure the driver returns is an [*Error], which carries the
// failure's SQLCODE and SQLSTATE as well as its message.
//
// The driver's manual is at https://turborel.com/docs/drivers/go/. The
// protocol is described in The Native Protocol, Volume IV of the
// turborel manuals:
// https://github.com/turborel/turborel/blob/main/doc/client/05-protocol.md
package turborel
