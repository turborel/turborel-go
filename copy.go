package turborel

// Bulk copy in [CLIENT Copying and exporting]: rows sent into a table
// with no round trip but the copy's end. CopyIn begins one, naming the
// table and its columns [PROTOCOL CopyIn 'I']; Send sends rows, in
// messages of about CopyChunkBytes, each with no reply [PROTOCOL CopyData
// 'J']; Finish ends it, its reply the rows inserted, or the copy's
// failure, every row of it undone [PROTOCOL CopyEnd 'N']. The server
// assigns each value to its column as INSERT does, so a string too long
// for its column is refused, not cut; a copy is all or nothing, within
// the transaction, or its own under autocommit.
//
// Until the copy ends the connection sends only its rows, its end and
// Ping; a copy left unfinished is abandoned as the connection's next
// other request is sent, so the connection takes requests again.

import (
	"context"
	"iter"
)

// CopyChunkBytes is about the most bytes of rows a CopyData carries: enough
// that a message's header is little of it, few enough that neither side
// holds much.
const CopyChunkBytes = 256 * 1024

// Copy is a copy under way, over its connection.
type Copy struct {
	conn *Conn
	ctx  context.Context //nolint:containedctx // A copy spans calls, each cancelled as its maker's context is done.
	// rows are the rows waiting to be sent, each encoded, and their
	// count.
	rows  writer
	count uint32
	ended bool
}

// CopyIn begins a copy into table, as SQL names it, of columns, or every
// column if none; under autocommit, the copy is its own transaction. It
// needs copy granted.
func (c *Conn) CopyIn(ctx context.Context, table string, columns []string) (*Copy, error) {
	return c.copyBegin(ctx, table, columns, c.autocommit)
}

// copyBegin begins a copy into table's columns, its transaction ended
// with it if ends.
func (c *Conn) copyBegin(ctx context.Context, table string, columns []string, ends bool) (*Copy, error) {
	if err := c.needs(CapabilityCopy); err != nil {
		return nil, err
	}

	if err := c.send(copyInRequest{table: table, columns: columns, ends: ends}); err != nil {
		return nil, withCause(err, ctx.Err())
	}

	c.copying = &Copy{conn: c, ctx: ctx}

	return c.copying, nil
}

// CopyRows copies each row of rows into table's columns, as CopyIn,
// Send and Finish do: the rows inserted.
func (c *Conn) CopyRows(ctx context.Context, table string, columns []string, rows iter.Seq[Row]) (uint64, error) {
	k, err := c.CopyIn(ctx, table, columns)
	if err != nil {
		return 0, err
	}

	defer func() { _ = k.Abandon() }() // A copy given up midway is abandoned; once finished, Abandon does nothing.

	for row := range rows {
		if err := k.Send(row); err != nil {
			return 0, err
		}
	}

	return k.Finish()
}

// Send sends row, with those before it once they come to CopyChunkBytes. A
// row the server refuses fails Finish.
func (k *Copy) Send(row Row) error {
	if k.ended {
		return misused("the copy has ended")
	}

	encodeValues(&k.rows, row)
	k.count++

	if len(k.rows.buf) >= CopyChunkBytes {
		return k.flush()
	}

	return nil
}

// Finish ends the copy: the rows inserted. It fails with the copy's
// failure, every row of it undone: the table or a column refuses, its
// detail naming the row, as "row 7 of the copy".
func (k *Copy) Finish() (uint64, error) {
	if k.ended {
		return 0, misused("the copy has ended")
	}

	if err := k.flush(); err != nil {
		k.ended = true
		k.conn.copying = nil

		return 0, err
	}

	return k.end(false)
}

// Abandon gives the copy up, every row of it undone. Of a copy already
// finished or abandoned it does nothing.
func (k *Copy) Abandon() error {
	if k.ended {
		return nil
	}

	_, err := k.end(true)

	return err
}

// flush sends the rows waiting.
func (k *Copy) flush() error {
	if k.count == 0 {
		return nil
	}

	req := copyDataRequest{count: k.count, rows: k.rows.buf}
	k.rows, k.count = writer{}, 0

	return withCause(k.conn.send(req), k.ctx.Err())
}

// end sends CopyEnd, abandoned if abandon: the rows inserted.
func (k *Copy) end(abandon bool) (uint64, error) {
	k.ended = true
	k.conn.copying = nil

	defer k.conn.during(k.ctx)()

	if err := k.conn.send(copyEndRequest{abandon: abandon}); err != nil {
		return 0, withCause(err, k.ctx.Err())
	}

	done, err := k.conn.completion()
	if err != nil {
		return 0, withCause(err, k.ctx.Err())
	}

	return done.Rows, nil
}

// copyRequest says whether req may be sent while a copy is under way:
// its rows, its end, or Ping.
func copyRequest(req request) bool {
	switch req.(type) {
	case copyDataRequest, copyEndRequest, pingRequest:
		return true
	default:
		return false
	}
}

// leaveCopy abandons the copy under way, before req is sent, if req may
// not be sent during it. A failure of the abandon has lost the
// connection, which req's send then says.
func (c *Conn) leaveCopy(req request) {
	if c.copying == nil || copyRequest(req) {
		return
	}

	_ = c.copying.Abandon() // A failure has lost the connection, which says so as req is sent.
}
