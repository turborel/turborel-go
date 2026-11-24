package turborel

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// liveSchema connects by trust over TLS and makes a schema of name for
// the test, committed.
func liveSchema(t *testing.T, name string) *Conn {
	t.Helper()

	c := liveConnect(t, liveTLS(t, 0, false), Trust{}, nil)
	mustRun(t, c, "CREATE SCHEMA "+name)
	mustRun(t, c, "COMMIT")

	return c
}

// mustRun runs sql by Query, failing the test if it fails.
func mustRun(t *testing.T, c *Conn, sql string) *Outcome {
	t.Helper()

	o, err := c.Query(context.Background(), sql)
	if err != nil {
		t.Fatalf("%s: %v", sql, err)
	}

	return o
}

// TestLiveQuery runs a query, a statement, a query of no rows and a
// failure.
//
// Verifies: [PROTOCOL Query 'Q'] a query is answered by a Description,
// its Rows and a Complete; another statement by a Complete; a query of no
// row completes with SQLCODE 100 and SQLSTATE 02000; a failure by an
// Error, its SQLCODE, SQLSTATE, message, rule and position apart
// [CLIENT Running statements]
// [PROTOCOL Description 'D'] [PROTOCOL Rows 'B'].
func TestLiveQuery(t *testing.T) {
	t.Parallel()

	c := liveSchema(t, "QUERY_TEST")
	mustRun(t, c, "CREATE TABLE QUERY_TEST.STAFF (ID INT, NAME VARCHAR(20))")
	done := mustRun(t, c, "INSERT INTO QUERY_TEST.STAFF VALUES (1, 'Ada'), (2, 'Grace')")

	if done.IsQuery() || done.Completion.SQLCode != 0 || done.Completion.SQLState != "00000" {
		t.Errorf("INSERT gave %+v", done)
	}

	assertStaff(t, mustRun(t, c, "SELECT ID, NAME FROM QUERY_TEST.STAFF ORDER BY ID"))

	none := mustRun(t, c, "SELECT ID FROM QUERY_TEST.STAFF WHERE ID > 5")
	if len(none.Rows) != 0 || none.Completion.SQLCode != 100 || none.Completion.SQLState != "02000" {
		t.Errorf("no rows gave %+v", none.Completion)
	}

	_, err := c.Query(context.Background(), "SELECT ID FROM QUERY_TEST.NOPE")

	var failure *Error
	if !errors.As(err, &failure) || failure.SQLCode != -103 || failure.SQLState != "42000" || !failure.Placed {
		t.Errorf("a missing table gave %#v", err)
	}
}

// assertStaff checks the rows of STAFF, as TestLiveQuery made them.
func assertStaff(t *testing.T, rows *Outcome) {
	t.Helper()

	if !rows.IsQuery() || len(rows.Columns) != 2 || rows.Columns[1].Name != "NAME" || len(rows.Rows) != 2 {
		t.Fatalf("SELECT gave %+v", rows)
	}

	if rows.Rows[1][1] != Char("Grace") || rows.Rows[0][0] != DecimalOf(1, 0) {
		t.Errorf("rows %v", rows.Rows)
	}
}

// collector is a sink that keeps what it is given.
type collector struct {
	columns []Column
	batches int
	rows    []Row
}

func (s *collector) Columns(columns []Column) { s.columns = columns }

func (s *collector) Rows(rows []Row) {
	s.batches++
	s.rows = append(s.rows, rows...)
}

// TestLiveQueryInto gives a query's rows to a sink a batch at a time.
//
// Verifies: [CLIENT Running statements] a query's rows are given to a sink
// a batch at a time, as they come, its columns first; another
// statement's completion is given as it is.
func TestLiveQueryInto(t *testing.T) {
	t.Parallel()

	c := liveConnect(t, liveTLS(t, 0, false), Trust{}, nil)
	sink := &collector{}

	streamed, err := c.QueryInto(context.Background(),
		"SELECT * FROM INFORMATION_SCHEMA.COLUMNS A, INFORMATION_SCHEMA.TABLES B", sink)
	if err != nil {
		t.Fatal(err)
	}

	if streamed.Rows == 0 || streamed.Rows != uint64(len(sink.rows)) || sink.batches < 2 || len(sink.columns) == 0 {
		t.Errorf("streamed %d rows in %d batches", streamed.Rows, sink.batches)
	}

	done, err := c.QueryInto(context.Background(), "SET SCHEMA 'ANN'", sink)
	if err != nil || done.Rows != 0 {
		t.Errorf("a statement streamed %+v, %v", done, err)
	}
}

// TestLiveTransactions begins a transaction with the first statement,
// and makes each statement its own under autocommit.
//
// Verifies: [CLIENT Running statements] a transaction begins with the
// first statement and lasts until COMMIT or ROLLBACK, the session telling
// whether it is active; under autocommit each statement is its own
// transaction, committed by the request's `ends` with no COMMIT sent
// [PROTOCOL Query 'Q'], a failing statement rolled back, and a failing
// commit's Error given in place of the Complete; a statement under
// autocommit is one Query with ends set [PROTOCOL Conversations].
func TestLiveTransactions(t *testing.T) {
	t.Parallel()

	c := liveSchema(t, "TXN_TEST")
	mustRun(t, c, "CREATE TABLE TXN_TEST.T (A INT CONSTRAINT TXN_TEST.POSITIVE CHECK (A > 0) INITIALLY DEFERRED)")
	mustRun(t, c, "COMMIT")
	mustRun(t, c, "INSERT INTO TXN_TEST.T VALUES (1)")

	if !c.InTransaction() {
		t.Error("no transaction after an INSERT")
	}

	mustRun(t, c, "ROLLBACK")
	c.SetAutocommit(true)

	if !c.Autocommit() {
		t.Error("autocommit not set")
	}

	done := mustRun(t, c, "INSERT INTO TXN_TEST.T VALUES (2)")
	if c.InTransaction() || done.Completion.Active {
		t.Error("a transaction is active after an autocommitted statement")
	}

	_, err := c.Query(context.Background(), "INSERT INTO TXN_TEST.T VALUES (0)")
	assertState(t, err, "40002")

	if c.InTransaction() {
		t.Error("a failed commit left its transaction active")
	}

	if rows := mustRun(t, c, "SELECT A FROM TXN_TEST.T"); len(rows.Rows) != 1 {
		t.Errorf("%d rows, want the autocommitted one", len(rows.Rows))
	}
}

// TestLiveValues reads and writes a value of each kind.
//
// Verifies: [CLIENT Running statements] every value comes as the
// driver's: exact, approximate, character, binary, datetime, interval,
// boolean and null; and as a parameter each is written back as it came
// [CLIENT Prepared statements].
func TestLiveValues(t *testing.T) {
	t.Parallel()

	c := liveConnect(t, liveTLS(t, 0, false), Trust{}, nil)
	sql := "VALUES (12.50, CAST(1.5 AS DOUBLE PRECISION), 'text', X'0aff', DATE '2026-11-20', " +
		"TIMESTAMP '2026-11-20 12:34:56.123456', INTERVAL '3' DAY, TRUE, CAST(NULL AS INTEGER))"
	row := mustRun(t, c, sql).Rows[0]
	assertValues(t, row)

	st, err := c.Prepare(context.Background(), "VALUES (CAST(? AS DECIMAL(4,2)), CAST(? AS VARCHAR(10)), "+
		"CAST(? AS DATE), CAST(? AS TIMESTAMP(6)), CAST(? AS INTERVAL DAY), CAST(? AS BOOLEAN))")
	if err != nil {
		t.Fatal(err)
	}

	back, err := c.Execute(context.Background(), st, []Value{row[0], row[2], row[4], row[5], row[6], row[7]})
	if err != nil {
		t.Fatal(err)
	}

	want := Row{row[0], row[2], row[4], row[5], row[6], row[7]}
	if got := back.Rows[0]; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[5] != want[5] {
		t.Errorf("parameters came back as %#v", got)
	}
}

// assertValues checks the values TestLiveValues reads.
func assertValues(t *testing.T, row Row) {
	t.Helper()

	binary, _ := row[3].(Binary)
	if row[0] != DecimalOf(1250, 2) || row[1] != Approx(1.5) || row[2] != Char("text") || string(binary) != "\x0a\xff" {
		t.Errorf("read %#v", row)
	}

	if row[7] != Bool(true) || row[8] != (Null{}) {
		t.Errorf("read %#v", row)
	}

	if d, ok := row[4].(Datetime); !ok || d.Moment != MomentDate || d.Time().Format("2006-01-02") != "2026-11-20" {
		t.Errorf("DATE read as %#v", row[4])
	}

	if i, ok := row[6].(Interval); !ok || i.Qualifier.Start != Day || i.Units().Int64() != 3*microsPerDay {
		t.Errorf("INTERVAL read as %#v", row[6])
	}
}

// TestLivePrepared prepares, executes, describes and deallocates.
//
// Verifies: [CLIENT Prepared statements] a statement is prepared once, its
// `?` parameters' types and a query's columns given, executed with values
// as many times as wanted, across transactions, and deallocated
// [PROTOCOL Prepare 'P'] [PROTOCOL Deallocate 'D']; one deallocated is
// none such (SQLSTATE 26000); describe describes it again [PROTOCOL
// Describe 'Y'] [PROTOCOL Capabilities].
func TestLivePrepared(t *testing.T) {
	t.Parallel()

	c := liveSchema(t, "PREPARED_TEST")
	mustRun(t, c, "CREATE TABLE PREPARED_TEST.T (ID INT, NAME VARCHAR(20))")

	insert, err := c.Prepare(context.Background(), "INSERT INTO PREPARED_TEST.T VALUES (?, ?)")
	if err != nil {
		t.Fatal(err)
	}

	if len(insert.Parameters()) != 2 || len(insert.Columns()) != 0 {
		t.Errorf("parameters %v, columns %v", insert.Parameters(), insert.Columns())
	}

	for i, name := range []string{"Ada", "Grace", "Edsger"} {
		if _, err := c.Execute(context.Background(), insert, []Value{DecimalOf(int64(i), 0), Char(name)}); err != nil {
			t.Fatal(err)
		}

		mustRun(t, c, "COMMIT")
	}

	query, err := c.Prepare(context.Background(), "SELECT NAME FROM PREPARED_TEST.T WHERE ID = ?")
	if err != nil {
		t.Fatal(err)
	}

	got, err := c.Execute(context.Background(), query, []Value{DecimalOf(1, 0)})
	if err != nil || len(got.Rows) != 1 || got.Rows[0][0] != Char("Grace") || query.Columns()[0].Name != "NAME" {
		t.Errorf("SELECT gave %+v, %v", got, err)
	}

	described, err := c.Describe(context.Background(), query)
	if err != nil || len(described.Parameters()) != 1 || len(described.Columns()) != 1 {
		t.Errorf("Describe gave %+v, %v", described, err)
	}

	if err := c.Deallocate(context.Background(), query); err != nil {
		t.Fatal(err)
	}

	_, err = c.Execute(context.Background(), query, []Value{DecimalOf(1, 0)})
	assertState(t, err, "26000")
	assertState(t, c.Deallocate(context.Background(), query), "26000")
}

// TestLiveCursors reads a query in parts by cursors, numbered too.
//
// Verifies: [PROTOCOL Execute 'E'] a query's cursor sends at most the rows
// asked for, Suspended if it has more, else Complete, the cursor closed;
// Fetch reads more [PROTOCOL Fetch 'F'] and Close closes it [PROTOCOL
// Close 'C']; with cursors, several numbered cursors of one statement
// are open at once, one already open fails (SQLSTATE 24000), as do a
// fetch or close of one not open, and a statement whose cursor is open is
// not replaced or deallocated [CLIENT Controlling the session]
// [PROTOCOL Capabilities]
// [PROTOCOL Suspended 'S'].
func TestLiveCursors(t *testing.T) {
	t.Parallel()

	c := liveConnect(t, liveTLS(t, 0, false), Trust{}, nil)

	st, err := c.Prepare(context.Background(), "SELECT TABLE_NAME FROM INFORMATION_SCHEMA.TABLES")
	if err != nil {
		t.Fatal(err)
	}

	first, err := c.OpenCursor(context.Background(), st, nil, 2)
	if err != nil || len(first.Rows) != 2 || !first.More {
		t.Fatalf("OpenCursor gave %+v, %v", first, err)
	}

	second, err := c.OpenCursorNumbered(context.Background(), st, 1, nil, 1)
	if err != nil || len(second.Rows) != 1 {
		t.Fatalf("a second cursor gave %+v, %v", second, err)
	}

	columns, err := c.DescribeCursor(context.Background(), st, 1)
	if err != nil || len(columns) != 1 {
		t.Errorf("DescribeCursor gave %v, %v", columns, err)
	}

	_, err = c.OpenCursorNumbered(context.Background(), st, 1, nil, 1)
	assertState(t, err, "24000")

	assertState(t, c.Deallocate(context.Background(), st), "24000")
	assertNotReplaced(t, c, st)

	rest, err := c.Fetch(context.Background(), st, 0)
	if err != nil || rest.More || len(rest.Rows) == 0 {
		t.Errorf("Fetch gave %+v, %v", rest, err)
	}

	if err := c.CloseCursorNumbered(context.Background(), st, 1); err != nil {
		t.Error(err)
	}

	_, err = c.Fetch(context.Background(), st, 1)
	assertState(t, err, "24000")

	if err := c.CloseCursor(context.Background(), st); err == nil {
		t.Error("a cursor not open was closed")
	}
}

// assertNotReplaced checks that st, its cursor open, is not replaced by a
// Prepare of its number (SQLSTATE 24000).
func assertNotReplaced(t *testing.T, c *Conn, st *Statement) {
	t.Helper()

	if err := c.send(prepareRequest{statement: st.number, sql: "VALUES 1"}); err != nil {
		t.Fatal(err)
	}

	_, err := c.prepared()
	assertState(t, err, "24000")
}

// TestLiveFetchEnds ends a cursor's transaction as the cursor ends, under
// autocommit.
//
// Verifies: [CLIENT What the server tells] under autocommit, with
// fetch-ends granted, a cursor's transaction ends as it ends: the fetch
// of its last row commits, and so does closing it [PROTOCOL Execute 'E']
// [PROTOCOL Conversations] [PROTOCOL Capabilities].
func TestLiveFetchEnds(t *testing.T) {
	t.Parallel()

	c := liveConnect(t, liveTLS(t, 0, false), Trust{}, nil)
	c.SetAutocommit(true)

	st, err := c.Prepare(context.Background(), "SELECT TABLE_NAME FROM INFORMATION_SCHEMA.TABLES")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := c.OpenCursor(context.Background(), st, nil, 1); err != nil || !c.InTransaction() {
		t.Fatalf("the open cursor's transaction: %v, active %v", err, c.InTransaction())
	}

	if _, err := c.Fetch(context.Background(), st, 0); err != nil || c.InTransaction() {
		t.Errorf("the last fetch left its transaction: %v, active %v", err, c.InTransaction())
	}

	if _, err := c.OpenCursor(context.Background(), st, nil, 1); err != nil {
		t.Fatal(err)
	}

	if err := c.CloseCursor(context.Background(), st); err != nil || c.InTransaction() {
		t.Errorf("closing left its transaction: %v, active %v", err, c.InTransaction())
	}
}

// TestLiveCompletions reads what a statement's completion tells.
//
// Verifies: [CLIENT What the server tells] the rows a statement changed,
// or a query returned; the identity values an INSERT generated, in the
// order of its rows; the server's time; the log position of the last
// commit [PROTOCOL Complete 'K'], with row-counts, generated-values and
// commit-position [PROTOCOL Capabilities].
func TestLiveCompletions(t *testing.T) {
	t.Parallel()

	c := liveSchema(t, "COMPLETION_TEST")
	mustRun(t, c, "CREATE TABLE COMPLETION_TEST.T (ID INT GENERATED ALWAYS AS IDENTITY, NAME VARCHAR(20))")
	c.SetAutocommit(true)
	mustRun(t, c, "INSERT INTO COMPLETION_TEST.T (NAME) VALUES ('Edsger'), ('Barbara')")

	if n, ok := c.RowsAffected(); !ok || n != 2 {
		t.Errorf("rows affected %d, %v", n, ok)
	}

	if got := c.LastGenerated(); !slices.Equal(got, []string{"1", "2"}) {
		t.Errorf("generated %q", got)
	}

	if c.ServerTime() == nil {
		t.Error("no server time")
	}

	if _, ok := c.CommitPosition(); !ok {
		t.Error("no commit position")
	}

	mustRun(t, c, "SELECT ID FROM COMPLETION_TEST.T")

	if n, _ := c.RowsAffected(); n != 2 {
		t.Errorf("a query's rows %d", n)
	}
}
