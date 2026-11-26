package turborel

// Rows of a copy by column [PROTOCOL CopyColumns 'O'], with `copy` and
// `columns`: each column's cells as text, one after another, with a bit
// for each null and where each cell ends, in place of CopyData. The
// server parses each column's cells straight into the column's type, as
// it would the character string of the cell's text in CopyData, so a
// cell too long fails at the copy's end, never cut. A copy mixes the two
// freely; rows go in the order they were given.

// kindCopyColumns is CopyColumns' kind.
const kindCopyColumns byte = 'O'

// bitsPerByte is the rows one byte of a column's null bits holds, and
// endBytes the bytes of a cell's end.
const (
	bitsPerByte = 8
	endBytes    = 4
)

// lane is one column's cells of the rows waiting: a bit for each null, row
// 0 the low bit of the first byte; where each cell ends in text; and the
// cells' text.
type lane struct {
	nulls []byte
	ends  []uint32
	text  []byte
}

// copyColumnsRequest is a CopyColumns: its rows, and a lane for each of
// the copy's columns.
type copyColumnsRequest struct {
	rows  uint32
	lanes []lane
}

func (q copyColumnsRequest) encode(w *writer) {
	w.frame(kindCopyColumns, func() {
		w.u32(q.rows)
		w.u32(count32(len(q.lanes)))

		for _, l := range q.lanes {
			w.bytes(l.nulls)
			w.u32(count32(len(l.ends)))

			for _, end := range l.ends {
				w.u32(end)
			}

			w.bytes(l.text)
		}
	})
}

// textRow says whether every value of row is text or null, as a row of a
// file is.
func textRow(row Row) bool {
	for _, v := range row {
		switch v.(type) {
		case Char, Null:
		default:
			return false
		}
	}

	return true
}

// SendText sends row, each value a character string, Char, or null, as a
// field of a file is: by column, in a CopyColumns, with `columns`
// granted, else as Send sends it, the server taking each cell as the
// character string of its text either way. Any other value is refused
// (SQLSTATE TB910).
func (k *Copy) SendText(row Row) error {
	if k.ended {
		return misused("the copy has ended")
	}

	if !textRow(row) {
		return misused("SendText takes text and nulls alone; Send sends other values")
	}

	if !k.conn.granted[CapabilityColumns] || (k.textRows > 0 && len(row) != len(k.lanes)) || len(row) == 0 {
		return k.Send(row)
	}

	if err := k.flushRows(); err != nil {
		return err
	}

	k.addText(row)

	if k.textBytes >= CopyChunkBytes {
		return k.flushText()
	}

	return nil
}

// addText adds row's cells to the lanes waiting.
func (k *Copy) addText(row Row) {
	if k.textRows == 0 {
		k.lanes = make([]lane, len(row))
	}

	at, bit := k.textRows/bitsPerByte, byte(1)<<(k.textRows%bitsPerByte)

	for i, v := range row {
		l := &k.lanes[i]
		if bit == 1 {
			l.nulls = append(l.nulls, 0)
		}

		if s, ok := v.(Char); ok {
			l.text = append(l.text, s...)
			k.textBytes += len(s)
		} else {
			l.nulls[at] |= bit
		}

		l.ends = append(l.ends, count32(len(l.text)))
		k.textBytes += endBytes
	}

	k.textRows++
}

// flushText sends the rows waiting by column.
func (k *Copy) flushText() error {
	if k.textRows == 0 {
		return nil
	}

	req := copyColumnsRequest{rows: k.textRows, lanes: k.lanes}
	k.lanes, k.textRows, k.textBytes = nil, 0, 0

	return withCause(k.conn.send(req), k.ctx.Err())
}

// sendRow sends row of an import: by SendText if it is all text, else by
// Send.
func (k *Copy) sendRow(row Row) error {
	if textRow(row) {
		return k.SendText(row)
	}

	return k.Send(row)
}
