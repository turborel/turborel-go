package turborel

import (
	"bytes"
	"context"
	"io"
	"slices"
	"strings"
	"testing"
)

// TestLiveRowsOfLOBMarks inserts large objects bound beside shorter
// literals in another row.
//
// Verifies: [CLIENT Large objects] a `?` in a row of an INSERT of several
// rows takes its object column's type, so a 4 MiB BLOB and an 80 KB CLOB
// bound in the first row, beside X'0102' and 'short' in the second, are
// stored whole and read back octet for octet [PROTOCOL Execute 'E'].
func TestLiveRowsOfLOBMarks(t *testing.T) {
	t.Parallel()

	c := liveSchema(t, "MARKS_TEST")
	mustRun(t, c, "CREATE TABLE MARKS_TEST.SCANS (ID INTEGER, B BLOB(10M), C CLOB(1M))")

	ctx := context.Background()
	blob := make([]byte, 4<<20)

	for i := range blob {
		blob[i] = byte(i % 251)
	}

	clob := strings.Repeat("abcdé", 16000)

	insert, err := c.Prepare(ctx, "INSERT INTO MARKS_TEST.SCANS VALUES (1, ?, ?), (2, X'0102', 'short')")
	if err != nil {
		t.Fatal(err)
	}

	values := []Value{LOB{Kind: BinaryLOB, Octets: blob}, LOB{Kind: CharacterLOB, Octets: []byte(clob)}}

	done, err := c.Execute(ctx, insert, values)
	if err != nil || done.Completion.SQLState != "00000" {
		t.Fatalf("INSERT gave %+v, %v", done, err)
	}

	var read [][]byte

	for _, row := range mustRun(t, c, "SELECT B, C FROM MARKS_TEST.SCANS ORDER BY ID").Rows {
		for _, v := range row {
			lob, _ := v.(LargeObject)

			all, err := io.ReadAll(c.NewLOBReader(ctx, lob))
			if err != nil {
				t.Fatal(err)
			}

			read = append(read, all)
		}
	}

	want := [][]byte{blob, []byte(clob), {1, 2}, []byte("short")}
	if !slices.EqualFunc(read, want, bytes.Equal) {
		t.Errorf("read back %d values, not as stored", len(read))
	}
}

// TestLiveMarkCut warns of a `?`'s value cut to its type.
//
// Verifies: [CLIENT Running statements] a `?`'s value longer than the type
// its place gives it is cut to that type as it is cast, and the statement
// completes with the warning 01004, executed again too: an INSERT's and
// an UPDATE's value, of the column's type, and a comparison operand, of
// the other operand's, whose cut value is compared, with ORDER BY or
// without; a value that fits
// warns of nothing; a literal too long refused, 22001 [CLIENT Prepared
// statements].
func TestLiveMarkCut(t *testing.T) {
	t.Parallel()

	c := liveSchema(t, "CUT_TEST")
	mustRun(t, c, "CREATE TABLE CUT_TEST.V (K INTEGER, S VARCHAR(5))")

	cut := func(sql string, values ...Value) *Outcome {
		t.Helper()

		st, err := c.Prepare(context.Background(), sql)
		if err != nil {
			t.Fatal(err)
		}

		o, err := c.Execute(context.Background(), st, values)
		if err != nil {
			t.Fatalf("%s: %v", sql, err)
		}

		return o
	}

	for k := range int64(2) {
		o := cut("INSERT INTO CUT_TEST.V VALUES (?, ?)", DecimalOf(k+1, 0), Char("abcdefgh"))
		if o.Completion.SQLState != "01004" {
			t.Errorf("insert %d completed %s", k+1, o.Completion.SQLState)
		}
	}

	if o := cut("INSERT INTO CUT_TEST.V VALUES (3, ?)", Char("abc")); o.Completion.SQLState != "00000" {
		t.Errorf("a value that fits completed %s", o.Completion.SQLState)
	}

	_, err := c.Query(context.Background(), "INSERT INTO CUT_TEST.V VALUES (4, 'abcdefgh')")
	assertState(t, err, "22001")

	if o := cut("UPDATE CUT_TEST.V SET S = ? WHERE K = 3", Char("uvwxyz")); o.Completion.SQLState != "01004" {
		t.Errorf("update completed %s", o.Completion.SQLState)
	}

	for _, order := range []string{" ORDER BY K", ""} {
		compared := cut("SELECT K FROM CUT_TEST.V WHERE S = ?"+order, Char("abcdefgh"))
		if state, _ := c.Warning(); state != "01004" || len(compared.Rows) != 2 {
			t.Errorf("a comparison%s gave %v, %s", order, compared.Rows, state)
		}
	}

	if got := mustRun(t, c, "SELECT S FROM CUT_TEST.V ORDER BY K").Rows; len(got) != 3 || got[0][0] != Char("abcde") ||
		got[2][0] != Char("uvwxy") {
		t.Errorf("stored %v", got)
	}
}
