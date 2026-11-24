package turborel

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// copyIn reads a CopyIn: its table, columns and ends flag.
func (f *fake) copyIn() (string, []string, bool) {
	r := newReader(f.expect(kindCopyIn), "request")

	return r.str(), r.strs(), r.flag()
}

// copyData reads a CopyData: the count of its rows.
func (f *fake) copyData() uint32 {
	return newReader(f.expect(kindCopyData), "request").u32()
}

// copyEnd reads a CopyEnd: whether it abandons the copy.
func (f *fake) copyEnd() bool {
	return newReader(f.expect(kindCopyEnd), "request").flag()
}

// copies serves TestCopyFlow's two copies, noting each CopyIn's columns,
// table and ends flag and each CopyEnd's abandon flag in begun, each
// CopyData's count of rows in counts.
func (f *fake) copies(begun *[]string, counts *[]uint32) {
	table, columns, ends := f.copyIn()
	*begun = append(slices.Clone(columns), table, strconv.FormatBool(ends))

	f.expect(kindPing)
	f.reply(kindPong, nil)
	*counts = append(*counts, f.copyData(), f.copyData())
	*begun = append(*begun, strconv.FormatBool(f.copyEnd()))
	f.counted(4001)

	_, _, ends = f.copyIn()
	*begun = append(*begun, strconv.FormatBool(ends))
	*counts = append(*counts, f.copyData())
	f.copyEnd()
	f.failure(-201, "23000")
}

// TestCopyFlow copies rows: CopyIn, CopyData, CopyEnd.
//
// Verifies: [PROTOCOL CopyIn 'I'] a copy begins with CopyIn, its table,
// columns and ends flag, autocommit's, with no reply; rows go in
// CopyData, many to a message, once about CopyChunkBytes wait, and the
// rest at CopyEnd [PROTOCOL CopyData 'J'], whose Complete counts the
// rows inserted [PROTOCOL CopyEnd 'N']; until then only CopyData, CopyEnd
// and Ping are sent, Ping answered [PROTOCOL Ping 'G']; a copy needs copy
// granted [CLIENT Copying and exporting]
// [PROTOCOL Pong 'G'].
func TestCopyFlow(t *testing.T) {
	t.Parallel()

	var (
		begun  []string
		counts []uint32
	)

	c := openGranted(t, []string{CapabilityCopy, CapabilityPing}, func(f *fake) { f.copies(&begun, &counts) })

	ctx := context.Background()

	k, err := c.CopyIn(ctx, "S.T", []string{"ID", "NOTE"})
	if err != nil {
		t.Fatal(err)
	}

	sendAll(t, k, []Row{{DecimalOf(0, 0), Null{}}})

	if err := c.Ping(ctx); err != nil {
		t.Error(err)
	}

	sendAll(t, k, slices.Repeat([]Row{{DecimalOf(1, 0), Char(strings.Repeat("n", 100))}}, 4000))

	if n, err := k.Finish(); err != nil || n != 4001 {
		t.Errorf("Finish gave %d, %v", n, err)
	}

	c.SetAutocommit(true)

	_, err = c.CopyRows(ctx, "S.T", nil, slices.Values([]Row{{DecimalOf(1, 0), Null{}}}))
	assertState(t, err, "23000")

	if !slices.Equal(begun, []string{"ID", "NOTE", "S.T", "false", "false", "true"}) || counts[0] < 1000 ||
		counts[0]+counts[1] != 4001 || counts[2] != 1 {
		t.Errorf("began %v, sent %v", begun, counts)
	}
}

// TestCopyLeft abandons a copy the program leaves.
//
// Verifies: [PROTOCOL CopyIn 'I'] a copy left unfinished is abandoned,
// CopyEnd's abandon flag set, before the connection's next other
// request, which is then sent [PROTOCOL CopyEnd 'N']; a copy ended takes
// no more rows and is not finished again, and Abandon of it does nothing
// [CLIENT Copying and exporting].
func TestCopyLeft(t *testing.T) {
	t.Parallel()

	c := openGranted(t, []string{CapabilityCopy}, func(f *fake) {
		f.copyIn()
		f.copyData()

		if !f.copyEnd() {
			t.Error("a copy left was not abandoned")
		}

		f.counted(0)
		f.expect(kindQuery)
		f.complete("00000", false)
	})

	ctx := context.Background()

	k, err := c.CopyIn(ctx, "T", nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := k.Send(Row{DecimalOf(1, 0)}); err != nil {
		t.Fatal(err)
	}

	if err := k.flush(); err != nil {
		t.Fatal(err)
	}

	if _, err := c.Query(ctx, "COMMIT"); err != nil {
		t.Fatal(err)
	}

	if err := k.Send(Row{}); err == nil {
		t.Error("a row was sent to a copy ended")
	}

	if _, err := k.Finish(); err == nil {
		t.Error("a copy ended was finished")
	}

	if err := k.Abandon(); err != nil {
		t.Errorf("Abandon of a copy ended: %v", err)
	}

	refused := openGranted(t, nil, func(*fake) {})
	if _, err := refused.CopyRows(ctx, "T", nil, slices.Values([]Row{})); err == nil {
		t.Error("a copy not granted began")
	}
}

// failingWriter is a writer that fails.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errDiskGone }

// queryReplies replies a query of one column N and its rows.
func (f *fake) queryReplies(rows ...Row) {
	f.expect(kindQuery)
	f.reply(kindDescription, func(w *writer) {
		encodeColumns(w, []Column{{Named: true, Name: "N", Type: VaryingType{Length: 9}}})
	})
	f.reply(kindRows, func(w *writer) {
		w.u32(count32(len(rows)))

		for _, row := range rows {
			encodeValues(w, row)
		}
	})
	f.complete("00000", true)
}

// TestExportFlow exports rows as CSV and as native rows.
//
// Verifies: [CLIENT Copying and exporting] an export writes CSV, a field
// quoted where it holds a comma, a quote, a line break or white space at
// either end, or is empty, a quote twice, a null empty; or native rows,
// read back as they were; a statement that is no query, and a write that
// fails, fail the export with SQLCODE -919, the connection kept; the
// query's failure is the export's.
func TestExportFlow(t *testing.T) {
	t.Parallel()

	rows := []Row{
		{Char("plain")}, {Char("a,b")}, {Char(`say "hi"`)}, {Char(" pad")}, {Char("pad\t")}, {Char("")}, {Null{}},
	}
	c := openGranted(t, nil, func(f *fake) {
		f.queryReplies(rows...)
		f.queryReplies(rows...)
		f.expect(kindQuery)
		f.complete("00000", false)
		f.queryReplies(rows[0])
		f.expect(kindQuery)
		f.failure(-103, "42000")
	})

	ctx := context.Background()

	var csv bytes.Buffer
	if n, err := c.Export(ctx, "SELECT", &csv, ExportCSV); err != nil || n != 7 ||
		csv.String() != "N\r\nplain\r\n\"a,b\"\r\n\"say \"\"hi\"\"\"\r\n\" pad\"\r\n\"pad\t\"\r\n\"\"\r\n\r\n" {
		t.Errorf("CSV gave %d, %v: %q", n, err, csv.String())
	}

	var native bytes.Buffer
	if _, err := c.Export(ctx, "SELECT", &native, ExportNative); err != nil {
		t.Fatal(err)
	}

	columns, back, err := ReadNative(&native)
	if err != nil || len(columns) != 1 || columns[0].Name != "N" || !slices.EqualFunc(back, rows, slices.Equal) {
		t.Errorf("read back %v, %v, %v", columns, back, err)
	}

	_, err = c.Export(ctx, "COMMIT", &csv, ExportCSV)
	if failure, ok := errors.AsType[*Error](err); !ok || failure.SQLCode != sqlcodeConnection || c.IsLost() {
		t.Errorf("an export of no query gave %v", err)
	}

	_, err = c.Export(ctx, "SELECT", failingWriter{}, ExportCSV)
	if !errors.Is(err, errDiskGone) || c.IsLost() {
		t.Errorf("a write that failed gave %v", err)
	}

	_, err = c.Export(ctx, "SELECT", &csv, ExportCSV)
	assertState(t, err, "42000")
}

// TestReadNativeRefuses refuses input that is not native rows.
//
// Verifies: [CLIENT Copying and exporting] native rows are a
// Description, Rows, then a Complete; input that ends early, begins
// otherwise, holds another frame, or a malformed one, is refused with
// SQLCODE -919.
func TestReadNativeRefuses(t *testing.T) {
	t.Parallel()

	description := encodeReply(kindDescription, func(w *writer) { encodeColumns(w, nil) })
	cases := [][]byte{
		nil,
		encodeReply(kindSuspended, func(*writer) {}),
		append(slices.Clone(description), encodeReply(kindSuspended, func(*writer) {})...),
		append(slices.Clone(description), kindRows, 0, 0, 0, 2),
		append(slices.Clone(description), kindRows, 2, 0, 0, 0, 1),
		{kindRows, 0xff, 0xff, 0xff, 0xff},
		{kindRows, 1, 0, 0, 0, 0},
	}

	for _, input := range cases {
		_, _, err := ReadNative(bytes.NewReader(input))
		if failure, ok := errors.AsType[*Error](err); !ok || failure.SQLCode != sqlcodeConnection {
			t.Errorf("%v gave %v", input, err)
		}
	}
}
