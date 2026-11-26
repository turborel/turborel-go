package turborel

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// copyColumns reads a CopyColumns: its rows and its columns' count.
func (f *fake) copyColumns() (uint32, uint32) {
	r := newReader(f.expect(kindCopyColumns), "request")

	return r.u32(), r.u32()
}

// TestCopyColumns sends rows of text by column.
//
// Verifies: [PROTOCOL CopyColumns 'O'] granted `columns`, rows of text
// and nulls go by column, in order with rows sent by row: a typed row
// sends the text rows before it, a text row the typed ones; a row of
// another count of columns, and every row without `columns` granted, by
// row; a chunk of about CopyChunkBytes sent as it fills; a value other
// than text or null refused (TB910) [PROTOCOL CopyData 'J'] [CLIENT
// Copying and exporting].
func TestCopyColumns(t *testing.T) {
	t.Parallel()

	var got []string

	c := openGranted(t, []string{CapabilityCopy, CapabilityColumns}, func(f *fake) {
		f.copyIn()

		for _, kind := range []byte{kindCopyColumns, kindCopyData, kindCopyColumns, kindCopyData, kindCopyColumns} {
			if kind == kindCopyColumns {
				rows, columns := f.copyColumns()
				got = append(got, "columns "+valueText(DecimalOf(int64(rows), 0))+" "+valueText(DecimalOf(int64(columns), 0)))
			} else {
				got = append(got, "data "+valueText(DecimalOf(int64(f.copyData()), 0)))
			}
		}

		f.copyEnd()
		f.done(false)
	})

	k, err := c.CopyIn(context.Background(), "T", nil)
	if err != nil {
		t.Fatal(err)
	}

	sends := []func() error{
		func() error { return k.SendText(Row{Char("a"), Null{}}) },
		func() error { return k.SendText(Row{Char(""), Char("b")}) },
		func() error { return k.Send(Row{DecimalOf(1, 0), Char("c")}) },
		func() error { return k.SendText(Row{Char("d"), Char("e")}) },
		func() error { return k.SendText(Row{Char("f")}) },
		func() error { return k.SendText(Row{Char(strings.Repeat("x", CopyChunkBytes)), Null{}}) },
	}

	for _, send := range sends {
		if err := send(); err != nil {
			t.Fatal(err)
		}
	}

	assertState(t, k.SendText(Row{DecimalOf(1, 0)}), sqlstateMisuse)

	if _, err := k.Finish(); err != nil {
		t.Fatal(err)
	}

	want := []string{"columns 2 2", "data 1", "columns 1 2", "data 1", "columns 1 2"}
	if !slices.Equal(got, want) {
		t.Errorf("sent %q", got)
	}
}

// TestCopyTextByRow sends text by row where `columns` is not granted.
//
// Verifies: [PROTOCOL CopyColumns 'O'] without `columns` granted, rows of
// text go by row, in CopyData, as Send sends them [PROTOCOL Capabilities]
// [CLIENT Copying and exporting].
func TestCopyTextByRow(t *testing.T) {
	t.Parallel()

	var rows uint32

	c := openGranted(t, []string{CapabilityCopy}, func(f *fake) {
		f.copyIn()
		rows = f.copyData()
		f.copyEnd()
		f.done(false)
	})

	k, err := c.CopyIn(context.Background(), "T", nil)
	if err != nil || k.SendText(Row{Char("a")}) != nil || k.SendText(Row{Null{}}) != nil {
		t.Fatal(err)
	}

	if _, err := k.Finish(); err != nil || rows != 2 {
		t.Errorf("Finish gave %v, %d rows by row", err, rows)
	}
}
