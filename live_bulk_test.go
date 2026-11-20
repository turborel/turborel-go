package turborel

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// staffRows gives n rows of an ID and a name.
func staffRows(n int) []Row {
	rows := make([]Row, n)
	for i := range rows {
		rows[i] = Row{DecimalOf(int64(i+1), 0), Char("name " + strings.Repeat("x", i%7))}
	}

	return rows
}

// sendAll sends rows to the copy k.
func sendAll(t *testing.T, k *Copy, rows []Row) {
	t.Helper()

	for _, row := range rows {
		if err := k.Send(row); err != nil {
			t.Fatal(err)
		}
	}
}

// count gives the rows of table.
func count(t *testing.T, c *Conn, table string) string {
	t.Helper()

	return valueText(mustRun(t, c, "SELECT COUNT(*) FROM "+table).Rows[0][0])
}

// TestLiveCopy copies rows into a table.
//
// Verifies: [CLIENT Copying and exporting] a copy into a table of the
// columns named sends rows, many to a message, with no reply each, and
// finishes giving the rows inserted [PROTOCOL CopyIn 'I'] [PROTOCOL
// CopyData 'J'] [PROTOCOL CopyEnd 'N'] [PROTOCOL Conversations]; Ping is
// answered during a copy [PROTOCOL Ping 'G']; the rows an iterator gives
// are copied; a copy abandoned undoes every row, its Complete counting
// none; under autocommit a copy is its own transaction, committed as it
// succeeds; copy granted [PROTOCOL Capabilities].
func TestLiveCopy(t *testing.T) {
	t.Parallel()

	c := liveSchema(t, "COPY_TEST")
	mustRun(t, c, "CREATE TABLE COPY_TEST.STAFF (ID INT, NAME VARCHAR(20), NOTE VARCHAR(5) DEFAULT 'none')")
	mustRun(t, c, "COMMIT")

	k, err := c.CopyIn(context.Background(), "COPY_TEST.STAFF", []string{"ID", "NAME"})
	if err != nil {
		t.Fatal(err)
	}

	rows := staffRows(30000) // More than one CopyData's bytes.
	sendAll(t, k, rows[:15000])

	if err := c.Ping(context.Background()); err != nil {
		t.Errorf("Ping during a copy: %v", err)
	}

	sendAll(t, k, rows[15000:])

	if n, err := k.Finish(); err != nil || n != 30000 || !c.InTransaction() {
		t.Fatalf("Finish gave %d, %v, active %v", n, err, c.InTransaction())
	}

	if got := mustRun(t, c, "SELECT NOTE FROM COPY_TEST.STAFF WHERE ID = 1").Rows[0][0]; got != Char("none") {
		t.Errorf("a column not named took %#v", got)
	}

	mustRun(t, c, "ROLLBACK")
	c.SetAutocommit(true)

	if n, err := c.CopyRows(context.Background(), "COPY_TEST.STAFF", nil, slices.Values([]Row{
		{DecimalOf(1, 0), Char("Ada"), Char("first")}, {DecimalOf(2, 0), Char("Grace"), Null{}},
	})); err != nil || n != 2 || c.InTransaction() {
		t.Errorf("CopyRows gave %d, %v, active %v", n, err, c.InTransaction())
	}

	abandoned, err := c.CopyIn(context.Background(), "COPY_TEST.STAFF", nil)
	if err != nil {
		t.Fatal(err)
	}

	sendAll(t, abandoned, []Row{{DecimalOf(3, 0), Char("Edsger"), Null{}}})

	if err := abandoned.Abandon(); err != nil || abandoned.Abandon() != nil || count(t, c, "COPY_TEST.STAFF") != "2" {
		t.Errorf("Abandon: %v", err)
	}
}

// TestLiveCopyAllOrNothing undoes a copy a row of which the table
// refuses.
//
// Verifies: [CLIENT Copying and exporting] a copy is all or nothing: a
// row refused undoes every row, its failure kept until CopyEnd, its
// detail naming the row, as "row 2 of the copy", the copy run under a
// savepoint of its own, so the transaction's work before it is kept
// [PROTOCOL CopyEnd 'N'] [PROTOCOL Conversations].
func TestLiveCopyAllOrNothing(t *testing.T) {
	t.Parallel()

	c := liveSchema(t, "REFUSED_TEST")
	mustRun(t, c, "CREATE TABLE REFUSED_TEST.STAFF (ID INT NOT NULL, NAME VARCHAR(3))")
	mustRun(t, c, "COMMIT")

	mustRun(t, c, "INSERT INTO REFUSED_TEST.STAFF VALUES (99, 'Kay')")

	k, err := c.CopyIn(context.Background(), "REFUSED_TEST.STAFF", nil)
	if err != nil {
		t.Fatal(err)
	}

	sendAll(t, k, []Row{{DecimalOf(1, 0), Char("Ada")}, {Null{}, Char("Bob")}, {DecimalOf(3, 0), Char("Cy")}})

	_, err = k.Finish()

	failure, ok := errors.AsType[*Error](err)
	if !ok || failure.Diagnostics == nil || failure.Diagnostics.Detail != "row 2 of the copy" {
		t.Errorf("a refused row gave %#v", err)
	}

	if _, err := k.Finish(); err == nil {
		t.Error("a copy finished twice")
	}

	if got := count(t, c, "REFUSED_TEST.STAFF"); got != "1" || !c.InTransaction() {
		t.Errorf("the copy undid the transaction's work, %s rows left", got)
	}

	mustRun(t, c, "ROLLBACK")

	if got := count(t, c, "REFUSED_TEST.STAFF"); got != "0" {
		t.Errorf("the table holds %s rows", got)
	}
}

// TestLiveCopyLeft abandons a copy left unfinished.
//
// Verifies: [PROTOCOL CopyIn 'I'] a copy left unfinished is abandoned as
// the next other request is sent, every row of it undone, so the
// connection takes requests again; the copy then takes no more rows
// [CLIENT Copying and exporting].
func TestLiveCopyLeft(t *testing.T) {
	t.Parallel()

	c := liveSchema(t, "LEFT_TEST")
	mustRun(t, c, "CREATE TABLE LEFT_TEST.STAFF (ID INT NOT NULL, NAME VARCHAR(3))")
	mustRun(t, c, "COMMIT")

	left, err := c.CopyIn(context.Background(), "LEFT_TEST.STAFF", nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := left.Send(Row{DecimalOf(1, 0), Char("Ada")}); err != nil {
		t.Fatal(err)
	}

	if got := count(t, c, "LEFT_TEST.STAFF"); got != "0" {
		t.Errorf("a copy left unfinished inserted, the table holding %s rows", got)
	}

	if err := left.Send(Row{DecimalOf(2, 0), Char("Bob")}); err == nil {
		t.Error("a row was sent to a copy abandoned")
	}
}

// TestLiveExport exports a query's rows as CSV and as native rows.
//
// Verifies: [CLIENT Copying and exporting] an export as CSV writes a
// header of the columns' names, then a record for each row as it comes,
// each value as turborel-client shows it, a null an empty field; as
// native rows, the protocol's own frames, read back with every value as
// it was; a statement that is no query is refused.
func TestLiveExport(t *testing.T) {
	t.Parallel()

	c := liveSchema(t, "EXPORT_TEST")
	mustRun(t, c, "CREATE TABLE EXPORT_TEST.T (ID INT, NAME VARCHAR(20), PAID DECIMAL(5,2), SEEN DATE)")
	mustRun(t, c, `INSERT INTO EXPORT_TEST.T VALUES (1, 'Ada, Countess', 1.5, DATE '1843-07-01'), `+
		`(2, 'say "hi"', NULL, NULL), (3, '', 0, DATE '2024-02-29')`)

	query := "SELECT ID, NAME, PAID, SEEN FROM EXPORT_TEST.T ORDER BY ID"

	var csv bytes.Buffer

	n, err := c.Export(context.Background(), query, &csv, ExportCSV)
	want := "ID,NAME,PAID,SEEN\r\n1,\"Ada, Countess\",1.50,1843-07-01\r\n" +
		"2,\"say \"\"hi\"\"\",,\r\n3,\"\",0.00,2024-02-29\r\n"

	if err != nil || n != 3 || csv.String() != want {
		t.Errorf("CSV gave %d, %v:\n%q", n, err, csv.String())
	}

	var native bytes.Buffer
	if n, err := c.Export(context.Background(), query, &native, ExportNative); err != nil || n != 3 {
		t.Fatalf("native gave %d, %v", n, err)
	}

	columns, rows, err := ReadNative(&native)
	expected := mustRun(t, c, query)

	if err != nil || !reflect.DeepEqual(columns, expected.Columns) || !reflect.DeepEqual(rows, expected.Rows) {
		t.Errorf("read back %v, %v, %v", columns, rows, err)
	}

	_, err = c.Export(context.Background(), "INSERT INTO EXPORT_TEST.T VALUES (4, 'x', 1, NULL)", &csv, ExportCSV)
	if failure, ok := errors.AsType[*Error](err); !ok || failure.Message != "an export runs a query" || c.IsLost() {
		t.Errorf("an export of no query gave %v", err)
	}
}
