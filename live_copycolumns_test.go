package turborel

import (
	"context"
	"errors"
	"testing"
)

// TestLiveCopyColumns copies rows of text by column.
//
// Verifies: [PROTOCOL CopyColumns 'O'] granted `columns`, which
// connecting asks for, rows of text go by column, each cell taken as the
// character string of its text: a null as null, an empty string as
// empty; a copy mixes rows by column and rows by row, kept in order; a
// cell too long for its column fails the copy at its end, never cut, its
// detail naming the row, every row undone; a copy abandoned keeps none
// [PROTOCOL CopyEnd 'N'] [CLIENT Copying and exporting].
func TestLiveCopyColumns(t *testing.T) {
	t.Parallel()

	c := liveSchema(t, "COLUMNS_TEST")
	mustRun(t, c, "CREATE TABLE COLUMNS_TEST.T (ID INTEGER, NAME VARCHAR(3))")
	mustRun(t, c, "COMMIT")

	if !c.Granted(CapabilityColumns) {
		t.Fatal("columns not granted")
	}

	ctx := context.Background()

	k, err := c.CopyIn(ctx, "COLUMNS_TEST.T", nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, send := range []func() error{
		func() error { return k.SendText(Row{Char("1"), Null{}}) },
		func() error { return k.SendText(Row{Char("2"), Char("")}) },
		func() error { return k.Send(Row{DecimalOf(3, 0), Char("abc")}) },
		func() error { return k.SendText(Row{Char("4"), Char("d")}) },
	} {
		if err := send(); err != nil {
			t.Fatal(err)
		}
	}

	if n, err := k.Finish(); err != nil || n != 4 {
		t.Fatalf("Finish gave %d, %v", n, err)
	}

	mustRun(t, c, "COMMIT")

	got := mustRun(t, c, "SELECT ID, NAME FROM COLUMNS_TEST.T ORDER BY ID").Rows
	if valueLiteral(Array{got[0], got[1], got[2], got[3]}) !=
		"ARRAY[ROW(1, NULL), ROW(2, ''), ROW(3, 'abc'), ROW(4, 'd')]" {
		t.Errorf("copied %v", got)
	}

	assertColumnsRefused(t, c)
}

// assertColumnsRefused copies a cell too long, which fails the copy at its
// end, naming its row, and abandons a copy; neither keeps a row.
func assertColumnsRefused(t *testing.T, c *Conn) {
	t.Helper()

	ctx := context.Background()

	long, err := c.CopyIn(ctx, "COLUMNS_TEST.T", nil)
	if err != nil || long.SendText(Row{Char("5"), Char("e")}) != nil ||
		long.SendText(Row{Char("6"), Char("toolong")}) != nil {
		t.Fatalf("CopyIn gave %v", err)
	}

	_, err = long.Finish()
	assertState(t, err, "22001")

	if failure, ok := errors.AsType[*Error](err); !ok || copyRowOf(failure) != 2 {
		t.Errorf("the failure %v names no row 2", err)
	}

	mustRun(t, c, "ROLLBACK")

	dropped, err := c.CopyIn(ctx, "COLUMNS_TEST.T", nil)
	if err != nil || dropped.SendText(Row{Char("7"), Char("f")}) != nil || dropped.Abandon() != nil {
		t.Fatalf("an abandoned copy gave %v", err)
	}

	mustRun(t, c, "COMMIT")

	if n := mustRun(t, c, "SELECT COUNT(*) FROM COLUMNS_TEST.T").Rows[0][0]; valueText(n) != "4" {
		t.Errorf("%v rows after the refused copies", n)
	}
}
