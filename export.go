package turborel

// Export [CLIENT Copying and exporting]: a query's rows streamed from the
// server and written as they come, a batch at a time, so memory holds a
// batch whatever the result's size. As CSV [RFC 4180]: a header of the
// columns' names, then a record for each row, a field quoted where it
// holds a comma, a quote, a line break or white space at either end, or
// is empty, a quote in it written twice; a null an empty field, an empty
// string "". Or as native rows: the protocol's own frames, a
// Description, then Rows, then a Complete, which ReadNative reads back,
// every value as it was, of whatever type.

import (
	"context"
	"errors"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ExportFormat is how Export writes rows.
type ExportFormat uint8

// The formats of an export.
const (
	// ExportCSV writes CSV: a header of the columns' names, then a record
	// for each row, each ended by CR LF.
	ExportCSV ExportFormat = iota
	// ExportNative writes the protocol's own frames: a Description, Rows,
	// then a Complete.
	ExportNative
)

// The SQLSTATE of the Complete that ends native rows.
const nativeComplete = "00000"

// errNotNative is input that is not native rows.
var errNotNative = errors.New("not native rows")

// exportSink writes the rows a query gives as they come, keeping the
// first write that failed.
type exportSink struct {
	out     io.Writer
	format  ExportFormat
	columns bool
	failed  error
}

// Columns writes the header, or the Description.
func (s *exportSink) Columns(columns []Column) {
	s.columns = true

	if s.format == ExportNative {
		s.write(encodeReply(kindDescription, func(w *writer) { encodeColumns(w, columns) }))

		return
	}

	names := make([]string, len(columns))
	for i, c := range columns {
		names[i] = csvField(c.Name)
	}

	s.write(csvRecord(names))
}

// Rows writes a record for each row, or a Rows frame.
func (s *exportSink) Rows(rows []Row) {
	if s.format == ExportNative {
		s.write(encodeReply(kindRows, func(w *writer) {
			w.u32(count32(len(rows)))

			for _, row := range rows {
				encodeValues(w, row)
			}
		}))

		return
	}

	var b []byte

	for _, row := range rows {
		fields := make([]string, len(row))
		for i, v := range row {
			fields[i] = csvValue(v)
		}

		b = append(b, csvRecord(fields)...)
	}

	s.write(b)
}

// write writes b, unless a write has failed.
func (s *exportSink) write(b []byte) {
	if s.failed == nil {
		_, s.failed = s.out.Write(b)
	}
}

// Export runs sql, a query, and writes its rows to out as they come, in
// format: the rows written. A statement that is no query, or a write that
// fails, fails the export with SQLCODE -919, the connection kept.
func (c *Conn) Export(ctx context.Context, sql string, out io.Writer, format ExportFormat) (uint64, error) {
	sink := &exportSink{out: out, format: format}

	streamed, err := c.QueryInto(ctx, sql, sink)
	if err != nil {
		return 0, err
	}

	if !sink.columns {
		return 0, broken("an export runs a query")
	}

	if format == ExportNative {
		sink.write(encodeReply(kindComplete, func(w *writer) {
			w.u32(0)
			w.str(nativeComplete)
			w.flag(false)
		}))
	}

	if sink.failed != nil {
		return 0, &Error{
			SQLCode: sqlcodeConnection, SQLState: sqlstateConnectionFailure,
			Message: "cannot write the export: " + sink.failed.Error(), cause: sink.failed,
		}
	}

	return streamed.Rows, nil
}

// encodeReply gives the frame of a reply of kind, its body written by
// body.
func encodeReply(kind byte, body func(w *writer)) []byte {
	var w writer

	w.frame(kind, func() { body(&w) })

	return w.buf
}

// encodeColumns writes a list of columns.
func encodeColumns(w *writer, columns []Column) {
	w.u32(count32(len(columns)))

	for _, c := range columns {
		c.encode(w)
	}
}

// csvField gives field as a CSV field, quoted if it is empty or holds a
// comma, a quote, a line break, or white space at either end.
func csvField(field string) string {
	first, _ := utf8.DecodeRuneInString(field)
	last, _ := utf8.DecodeLastRuneInString(field)
	quoted := field == "" || strings.ContainsAny(field, ",\"\n\r") || unicode.IsSpace(first) || unicode.IsSpace(last)

	if !quoted {
		return field
	}

	return `"` + strings.ReplaceAll(field, `"`, `""`) + `"`
}

// csvValue gives v as a CSV field: a null empty, else its text.
func csvValue(v Value) string {
	if _, null := v.(Null); null {
		return ""
	}

	return csvField(valueText(v))
}

// csvRecord gives fields as a CSV record, ended by CR LF.
func csvRecord(fields []string) []byte {
	return []byte(strings.Join(fields, ",") + "\r\n")
}

// ReadNative reads native rows an export wrote: the columns, and every
// row, each value as it was. It fails with SQLCODE -919 if in is not
// native rows.
func ReadNative(in io.Reader) ([]Column, []Row, error) {
	first, err := nativeReply(in)
	if err != nil {
		return nil, nil, err
	}

	described, ok := first.(descriptionReply)
	if !ok {
		return nil, nil, notNative("no Description first", nil)
	}

	rows := []Row{}

	for {
		rep, err := nativeReply(in)
		if err != nil {
			return nil, nil, err
		}

		switch r := rep.(type) {
		case rowsReply:
			rows = append(rows, r.rows...)
		case completeReply:
			return described.columns, rows, nil
		default:
			return nil, nil, notNative("a frame other than Rows before the Complete", nil)
		}
	}
}

// nativeReply reads the next frame of native rows.
func nativeReply(in io.Reader) (reply, error) {
	var header [frameHeaderBytes]byte
	if _, err := io.ReadFull(in, header[:]); err != nil {
		return nil, notNative(err.Error(), err)
	}

	h, err := decodeFrameHeader(header, maxBody)
	if err != nil {
		return nil, err
	}

	body := make([]byte, h.length)
	if _, err := io.ReadFull(in, body); err != nil {
		return nil, notNative(err.Error(), err)
	}

	return decodeReply(h.kind, body)
}

// notNative is the failure of input that is not native rows, why.
func notNative(why string, cause error) *Error {
	if cause == nil {
		cause = errNotNative
	}

	return &Error{
		SQLCode: sqlcodeConnection, SQLState: sqlstateConnectionFailure, Message: "not native rows: " + why, cause: cause,
	}
}
