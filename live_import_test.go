package turborel

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// importText imports text, CSV of dialect, into how.Table: what it did,
// the rows skipped, and its failure.
func importText(t *testing.T, c *Conn, text string, dialect Dialect, how Import) (Imported, []Rejected, error) {
	t.Helper()

	var skipped []Rejected

	done, err := c.Import(context.Background(), NewDelimited(strings.NewReader(text), dialect), how,
		func(r Rejected) { skipped = append(skipped, r) })

	return done, skipped, err
}

// importFailure gives an import's failure.
func importFailure(t *testing.T, err error) *ImportError {
	t.Helper()

	failure, ok := errors.AsType[*ImportError](err)
	if !ok {
		t.Fatalf("the import gave %v", err)
	}

	return failure
}

// assertStopped checks that err is an import's failure where want says,
// SQL's of sqlstate.
func assertStopped(t *testing.T, err error, want ImportError, sqlstate string) {
	t.Helper()

	failure := importFailure(t, err)
	if failure.Cause.SQL == nil || failure.Cause.SQL.SQLState != sqlstate {
		t.Errorf("the import stopped for %v", failure)
	}

	want.Cause = failure.Cause
	if *failure != want {
		t.Errorf("the import stopped as %+v, want %+v", *failure, want)
	}
}

// TestLiveImportByCopy imports CSV by bulk copy.
//
// Verifies: [CLIENT Importing] a source's rows are imported into a table
// over a connection, by copy where the server grants it and no column is
// a large object's [CLIENT Copying and exporting]; a header's names name
// the columns, else the table's columns are taken in order; rows are
// committed a batch at a time; a string too long for its column is
// refused; a row the table refuses fails its batch, which is rolled
// back, the failure saying where its record began, the rows committed
// and rolled back.
func TestLiveImportByCopy(t *testing.T) {
	t.Parallel()

	c := liveSchema(t, "IMPORT_COPY")
	mustRun(t, c, "CREATE TABLE IMPORT_COPY.STAFF (ID INT NOT NULL, NAME CHAR(5), NOTE VARCHAR(9))")
	mustRun(t, c, "COMMIT")

	how := Import{Table: "import_copy.staff", Batch: 2}

	done, _, err := importText(t, c, "NOTE,id,\"NAME\"\nfirst,1,Ada\n,2,Bob   \nthird,3,\n", CSV().WithHeader(), how)
	if err != nil || done != (Imported{Rows: 3}) {
		t.Fatalf("the import gave %+v, %v", done, err)
	}

	rows := mustRun(t, c, "SELECT ID, NAME, NOTE FROM IMPORT_COPY.STAFF ORDER BY ID").Rows
	if valueText(rows[0]) != "ROW(1, 'Ada  ', 'first')" || valueText(rows[1]) != "ROW(2, 'Bob  ', NULL)" {
		t.Errorf("imported %v", rows)
	}

	mustRun(t, c, "COMMIT")

	_, _, err = importText(t, c, "4,Cy,x\n5,Dee,y\n6,,z\n7,Toolong,w\n", CSV(), how)

	assertStopped(t, err, ImportError{Position: Position{Record: 4, Line: 4, HasLine: true}, Placed: true, Committed: 2,
		RolledBack: 1}, sqlstateRightTruncation)

	_, _, err = importText(t, c, "8,Ed,x\n,Flo,y\n", CSV(), how)
	assertStopped(t, err, ImportError{Position: Position{Record: 2, Line: 2, HasLine: true}, Placed: true, RolledBack: 1},
		"23000")

	if got := count(t, c, "IMPORT_COPY.STAFF"); got != "5" {
		t.Errorf("the table holds %s rows", got)
	}

	mustRun(t, c, "COMMIT")
}

// TestLiveImportRefused refuses an import before its first row.
//
// Verifies: [CLIENT Importing] a record of too few fields is refused,
// saying so; an import begins with no transaction active; a table's name
// that is no <table name> is refused.
func TestLiveImportRefused(t *testing.T) {
	t.Parallel()

	c := liveSchema(t, "IMPORT_REFUSED")
	mustRun(t, c, "CREATE TABLE IMPORT_REFUSED.STAFF (ID INT NOT NULL, NAME CHAR(5), NOTE VARCHAR(9))")
	mustRun(t, c, "COMMIT")

	how := Import{Table: "IMPORT_REFUSED.STAFF", Batch: 2}

	_, _, err := importText(t, c, "9,Gus\n", CSV(), how)
	if failure := importFailure(t, err); failure.Cause.Input != "[DESIGN] 2 fields, for 3 columns" {
		t.Errorf("a short record gave %v", failure)
	}

	mustRun(t, c, "INSERT INTO IMPORT_REFUSED.STAFF VALUES (10, 'Hal', NULL)")

	_, _, err = importText(t, c, "11,Ida,x\n", CSV(), how)
	if failure := importFailure(t, err); !strings.Contains(failure.Error(), "no transaction active") {
		t.Errorf("an import within a transaction gave %v", failure)
	}

	mustRun(t, c, "ROLLBACK")

	_, _, err = importText(t, c, "1\n", CSV(), ImportInto("select"))
	notTable := "[ISO/IEC 9075:1992 §5.4] select is not a <table name>; 0 rows committed, 0 rolled back"
	if failure := importFailure(t, err); failure.Error() != notTable {
		t.Errorf("a reserved word for a table gave %q", failure.Error())
	}
}

// TestLiveImportByInsert imports rows one INSERT at a time, skipping bad
// rows.
//
// Verifies: [CLIENT Importing] an import that skips bad rows inserts by a
// prepared INSERT, each failing row skipped and given to the program,
// where its record began and why, the input's or SQL's; the rows
// committed and skipped counted; a field longer than 64 KiB for a CLOB
// column is sent by creating a large object [CLIENT Large objects]; an
// import of no table refused before its first row.
func TestLiveImportByInsert(t *testing.T) {
	t.Parallel()

	c := liveSchema(t, "IMPORT_INSERT")
	mustRun(t, c, "CREATE TABLE IMPORT_INSERT.DOCS (ID INT NOT NULL, BODY CLOB(1M))")
	mustRun(t, c, "COMMIT")

	long := strings.Repeat("é", LOBInlineOctets)
	text := "1," + long + "\n,orphan\n2\n3,short\n"

	how := Import{Table: "IMPORT_INSERT.DOCS", Skip: true}

	done, skipped, err := importText(t, c, text, CSV().WithMaxRecord(1<<20), how)
	if err != nil || done != (Imported{Rows: 2, Rejected: 2}) || len(skipped) != 2 {
		t.Fatalf("the import gave %+v, %v, %v", done, skipped, err)
	}

	if skipped[0].Position.Record != 2 || skipped[0].Cause.SQL == nil ||
		skipped[1].String() != "record 3, line 3: [DESIGN] 1 fields, for 2 columns; skipped" {
		t.Errorf("skipped %v", skipped)
	}

	got := mustRun(t, c, "SELECT ID, CHAR_LENGTH(BODY) FROM IMPORT_INSERT.DOCS ORDER BY ID").Rows
	if valueText(got[0]) != "ROW(1, 65536)" || valueText(got[1]) != "ROW(3, 5)" {
		t.Errorf("imported %v", got)
	}

	body, ok := mustRun(t, c, "SELECT BODY FROM IMPORT_INSERT.DOCS WHERE ID = 1").Rows[0][0].(Locator)
	if !ok || body.Characters != LOBInlineOctets {
		t.Errorf("the long field came back as %#v", body)
	}

	mustRun(t, c, "COMMIT")

	_, _, err = importText(t, c, "1,x\n", CSV(), ImportInto("IMPORT_INSERT.NOPE"))
	if failure := importFailure(t, err); failure.Placed || failure.Cause.SQL == nil || failure.Cause.SQL.Placed {
		t.Errorf("an import of no table gave %v", failure)
	}
}

// scriptedSource is a source that gives its records, then fails if told.
type scriptedSource struct {
	headings []Heading
	failure  error
	records  []*Record
	at       int
}

func (s *scriptedSource) Headings() ([]Heading, error) {
	if s.at < 0 {
		return nil, s.failure
	}

	return s.headings, nil
}

func (s *scriptedSource) Record() (*Record, error) {
	if s.at == len(s.records) {
		return nil, s.failure
	}

	s.at++

	return s.records[s.at-1], nil
}

// sourceOf is a source of records of values, failing after them with
// failure.
func sourceOf(failure error, values ...[]Value) *scriptedSource {
	s := &scriptedSource{failure: failure}
	for i, v := range values {
		s.records = append(s.records, &Record{Values: v, Position: Position{Record: uint64(i + 1)}})
	}

	return s
}

// TestLiveImportSourceFails stops an import whose source fails.
//
// Verifies: [CLIENT Importing] a source that cannot give its next record
// stops the import, by insert or by copy, its batch rolled back, the
// failure saying where the record began and why, the rows committed and
// rolled back; a source's values other than strings go as they are, a
// binary string longer than 64 KiB for a BLOB column by creating a large
// object [CLIENT Large objects].
func TestLiveImportSourceFails(t *testing.T) {
	t.Parallel()

	c := liveSchema(t, "IMPORT_SOURCE")
	mustRun(t, c, "CREATE TABLE IMPORT_SOURCE.T (ID INT, SCAN BLOB(1M))")
	mustRun(t, c, "CREATE TABLE IMPORT_SOURCE.U (ID INT, NAME VARCHAR(9))")
	mustRun(t, c, "COMMIT")

	broken := &SourceError{Position: Position{Record: 3, Line: 7, HasLine: true}, Message: "the disk is gone"}
	scan := bytes.Repeat([]byte{9}, LOBInlineOctets+1)
	source := sourceOf(broken, []Value{DecimalOf(1, 0), Binary(scan)}, []Value{DecimalOf(2, 0), Null{}})

	_, err := c.Import(context.Background(), source, ImportInto("IMPORT_SOURCE.T"), nil)
	if failure := importFailure(t, err); failure.Error() != broken.Error()+"; 0 rows committed, 2 rolled back" {
		t.Errorf("by insert: %v", failure)
	}

	done, err := c.Import(context.Background(), sourceOf(nil, []Value{DecimalOf(1, 0), Binary(scan)}),
		ImportInto("IMPORT_SOURCE.T"), nil)
	if err != nil || done.Rows != 1 {
		t.Fatalf("a long binary string gave %+v, %v", done, err)
	}

	if got := mustRun(t, c, "SELECT OCTET_LENGTH(SCAN) FROM IMPORT_SOURCE.T").Rows[0][0]; valueText(got) != "65537" {
		t.Errorf("the BLOB holds %v octets", got)
	}

	mustRun(t, c, "COMMIT")

	source = sourceOf(broken, []Value{DecimalOf(1, 0), Char("a")}, []Value{DecimalOf(2, 0), Char("b")})

	_, err = c.Import(context.Background(), source, Import{Table: "IMPORT_SOURCE.U", Batch: 1}, nil)
	if failure := importFailure(t, err); failure.Error() != broken.Error()+"; 2 rows committed, 0 rolled back" {
		t.Errorf("by copy: %v", failure)
	}
}

// TestLiveImportFailures stops an import before its first row, or at a
// value refused.
//
// Verifies: [CLIENT Importing] a source that cannot give its headings
// stops the import before its first row; a value SQL refuses stops it,
// its failure SQL's, which the import's failure wraps; the failure says
// the rows committed and rolled back in words.
func TestLiveImportFailures(t *testing.T) {
	t.Parallel()

	c := liveSchema(t, "IMPORT_FAILURES")
	mustRun(t, c, "CREATE TABLE IMPORT_FAILURES.U (ID INT, NAME VARCHAR(9))")
	mustRun(t, c, "COMMIT")

	headless := &scriptedSource{at: -1, failure: errDiskGone}

	_, err := c.Import(context.Background(), headless, ImportInto("IMPORT_FAILURES.U"), nil)
	failure := importFailure(t, err)
	if failure.Placed || failure.Error() != "the disk is gone; 0 rows committed, 0 rolled back" {
		t.Errorf("headings that fail: %v", failure)
	}

	refusedSource := sourceOf(nil, []Value{Char("x"), Null{}})
	_, err = c.Import(context.Background(), refusedSource, ImportInto("IMPORT_FAILURES.U"), nil)

	var sqlFailure *Error
	if refused := importFailure(t, err); !errors.As(err, &sqlFailure) || !errors.Is(refused.Unwrap(), sqlFailure) ||
		!strings.HasSuffix(failure.Error(), "; 0 rows committed, 0 rolled back") {
		t.Errorf("a value refused: %v", err)
	}

	if (&ImportError{Cause: Cause{Input: "x"}, Committed: 1}).Error() != "x; 1 row committed, 0 rolled back" ||
		(&ImportError{}).Unwrap() != nil {
		t.Error("an import's failure in words")
	}
}
