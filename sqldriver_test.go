package turborel

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
)

// sqlScript serves database/sql's statements on one connection.
func sqlScript(t *testing.T, sets *[]string) func(f *fake) {
	t.Helper()

	return func(f *fake) {
		greeted(nil, CapabilityRowCounts, CapabilityGeneratedValues, CapabilityPing)(f)
		f.expect(kindQuery)
		f.counted(0)

		f.sqlOf()
		f.preparedOf([]DataType{ExactType{Precision: 9}}, nil)
		f.expect(kindExecute)
		f.reply(kindComplete, func(w *writer) {
			w.u32(0)
			w.str("00000")
			w.flag(false)
			w.u8(tailRows)
			w.u64(1)
			w.u8(tailGenerated)
			w.strs([]string{"5"})
		})
		f.expect(kindDeallocate)
		f.done(false)

		sqlQueryScript(f)

		for range 2 {
			*sets = append(*sets, newReader(f.expect(kindQuery), "request").str())
			f.done(true)
		}

		f.expect(kindPing)
		f.reply(kindPong, nil)
		f.expect(kindTerminate)
	}
}

// sqlQueryScript serves a query of three rows read in two batches, then
// one closed before its last row.
func sqlQueryScript(f *fake) {
	column := []Column{{Named: true, Name: "N", Type: ExactType{Precision: 9}}}
	rows := func(n int64) {
		f.reply(kindRows, func(w *writer) { w.u32(1); encodeValues(w, []Value{DecimalOf(n, 0)}) })
	}

	f.sqlOf()
	f.preparedOf([]DataType{ExactType{Precision: 9}}, column)
	f.expect(kindExecute)
	rows(1)
	rows(2)
	f.reply(kindSuspended, nil)
	f.expect(kindFetch)
	rows(3)
	f.done(true)
	f.expect(kindDeallocate)
	f.done(false)

	f.sqlOf()
	f.preparedOf(nil, column)
	f.expect(kindExecute)
	rows(1)
	f.reply(kindSuspended, nil)
	f.expect(kindClose)
	f.done(false)
	f.expect(kindDeallocate)
	f.done(false)
}

// TestDatabaseSQLFlow runs database/sql's calls against a scripted
// server.
//
// Verifies: [DESIGN] D2 sql.OpenDB of a connector of
// settings; Exec of no arguments by Query, of arguments by
// a statement prepared, executed and deallocated, its rows affected and
// last identity value; a query read through its cursor, a batch at a
// time, closed if left before its last row; a transaction's isolation
// level and read-only set by SET TRANSACTION, then committed; a named
// argument refused before anything is sent; Ping; Close ends the
// session.
func TestDatabaseSQLFlow(t *testing.T) {
	t.Parallel()

	var sets []string

	settings := localSettings(listen(t, sqlScript(t, &sets)))
	settings.TLSMode = TLSDisable

	db := sql.OpenDB(NewDBConnector(settings))
	db.SetMaxOpenConns(1)

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "CREATE"); err != nil {
		t.Fatal(err)
	}

	result, err := db.ExecContext(ctx, "INSERT ?", 1)
	if err != nil {
		t.Fatal(err)
	}

	if id, err := result.LastInsertId(); err != nil || id != 5 {
		t.Errorf("last insert id %d, %v", id, err)
	}

	if n, err := result.RowsAffected(); err != nil || n != 1 {
		t.Errorf("rows affected %d, %v", n, err)
	}

	assertSQLRows(t, db)

	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil || tx.Commit() != nil {
		t.Fatalf("a transaction: %v", err)
	}

	if _, err := db.ExecContext(ctx, "X", sql.Named("a", 1)); err == nil {
		t.Error("a named argument was taken")
	}

	if err := db.PingContext(ctx); err != nil {
		t.Error(err)
	}

	if err := db.Close(); err != nil {
		t.Error(err)
	}

	if len(sets) != 2 || sets[0] != "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY" || sets[1] != "COMMIT" {
		t.Errorf("the transaction sent %v", sets)
	}
}

// assertSQLRows reads the scripted query's rows.
func assertSQLRows(t *testing.T, db *sql.DB) {
	t.Helper()

	rows, err := db.QueryContext(context.Background(), "SELECT ?", 1)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = rows.Close() }()

	var all []int64

	for rows.Next() {
		var n int64
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}

		all = append(all, n)
	}

	if err := rows.Err(); err != nil || len(all) != 3 || all[2] != 3 {
		t.Errorf("read %v, %v", all, err)
	}

	assertLeftRows(t, db)
}

// assertLeftRows reads the first of a query's rows, and closes it.
func assertLeftRows(t *testing.T, db *sql.DB) {
	t.Helper()

	left, err := db.QueryContext(context.Background(), "SELECT")
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := left.Close(); err != nil {
			t.Error(err)
		}
	}()

	if !left.Next() || left.Err() != nil {
		t.Error("a query left before its last row")
	}
}

// TestDatabaseSQLRefusals refuses what the driver cannot do.
//
// Verifies: [DESIGN] D2 sql.Open of a URI that is no
// turborel URI fails (SQLSTATE 08001); a
// connection lost before a call gives database/sql driver.ErrBadConn, so
// it takes another; a result of no rows counted or no identity value
// says so; the driver's connector gives the driver.
func TestDatabaseSQLRefusals(t *testing.T) {
	t.Parallel()

	_, err := sql.Open(DriverName, "other://x")
	assertUnable(t, "a URI of another scheme", err)

	if _, err := (Driver{}).Open("other://x"); err == nil {
		t.Error("a URI of another scheme was opened")
	}

	lost := &sqlConn{conn: openGranted(t, nil, func(*fake) {})}
	_ = lost.conn.Close()

	if _, err := lost.PrepareContext(context.Background(), "VALUES 1"); !errors.Is(err, driver.ErrBadConn) {
		t.Errorf("a lost connection gave %v", err)
	}

	if lost.IsValid() || !errors.Is(lost.ResetSession(context.Background()), driver.ErrBadConn) || lost.Close() != nil {
		t.Error("a lost connection is valid")
	}

	if _, err := (sqlResult{}).LastInsertId(); err == nil {
		t.Error("a result of no identity value gave one")
	}

	if _, err := (sqlResult{generated: []string{"x"}}).LastInsertId(); err == nil {
		t.Error("an identity value of no integer was taken")
	}

	if _, err := (sqlResult{}).RowsAffected(); err == nil {
		t.Error("a result of no count gave one")
	}

	if _, ok := NewDBConnector(nil).Driver().(Driver); !ok {
		t.Error("the connector's driver is another")
	}
}

// directScript serves TestSQLConnDirect's requests.
func directScript(f *fake, columns []Column) {
	f.sqlOf()
	f.preparedOf([]DataType{ExactType{Precision: 9}}, nil)

	for _, kind := range []byte{kindExecute, kindExecute, kindDeallocate} {
		f.expect(kind)
		f.done(kind == kindExecute)
	}

	f.sqlOf()
	f.preparedOf(nil, columns)
	f.expect(kindExecute)
	f.done(true)

	for range 2 {
		f.expect(kindQuery)
		f.done(false)
	}
}

// TestSQLConnDirect drives the driver's connection as database/sql does.
//
// Verifies: [DESIGN] D2 a statement prepared, its
// parameters counted, executed and queried, a statement of no columns
// queried as executed with no rows, closed by Deallocate; a transaction
// begun with no options and rolled back; a level of no SQL's refused
// (TB910); Ping by ROLLBACK where ping is not granted; the rows' column
// types; the native connection given.
func TestSQLConnDirect(t *testing.T) {
	t.Parallel()

	columns := []Column{{Named: true, Name: "N", Type: ExactType{Precision: 9, Scale: 2}, NotNull: true}}
	c := &sqlConn{conn: openGranted(t, nil, func(f *fake) { directScript(f, columns) })}
	c.conn.SetAutocommit(true)

	st, err := c.Prepare("INSERT ?")
	if err != nil || st.NumInput() != 1 {
		t.Fatalf("Prepare gave %v, %v", st, err)
	}

	if _, err := st.Exec([]driver.Value{int64(1)}); err != nil { //nolint:staticcheck // driver.Stmt's own method.
		t.Error(err)
	}

	rows, err := st.Query([]driver.Value{int64(1)}) //nolint:staticcheck // driver.Stmt's own method.
	if err != nil || rows.Next(make([]driver.Value, 0)) == nil || st.Close() != nil {
		t.Errorf("a query of no columns gave %v, %v", rows, err)
	}

	assertColumnTypes(t, c, columns)

	tx, err := c.Begin()
	if err != nil || tx.Rollback() != nil || !c.conn.Autocommit() {
		t.Errorf("Begin gave %v", err)
	}

	linear := driver.TxOptions{Isolation: driver.IsolationLevel(sql.LevelLinearizable)}
	if _, err := c.BeginTx(context.Background(), linear); err == nil {
		t.Error("a level of no SQL's was taken")
	}

	if err := c.Ping(context.Background()); err != nil {
		t.Errorf("Ping by ROLLBACK: %v", err)
	}

	if native, ok := NativeConn(c); !ok || native != c.conn {
		t.Error("the native connection")
	}
}

// assertColumnTypes queries a statement of columns, and checks the rows'
// column types.
func assertColumnTypes(t *testing.T, c *sqlConn, columns []Column) {
	t.Helper()

	st, err := c.PrepareContext(context.Background(), "SELECT")
	if err != nil {
		t.Fatal(err)
	}

	stmt, ok := st.(*sqlStmt)
	if !ok {
		t.Fatal("no statement")
	}

	rows, err := stmt.rows(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	nullable, _ := rows.ColumnTypeNullable(0)
	precision, scale, exact := rows.ColumnTypePrecisionScale(0)
	_, hasLength := rows.ColumnTypeLength(0)

	if rows.ColumnTypeDatabaseTypeName(0) != "NUMERIC" || nullable || precision != 9 || scale != 2 || !exact ||
		hasLength || rows.ColumnTypeScanType(0) != scanType(columns[0].Type) || rows.Columns()[0] != "N" {
		t.Error("column types")
	}

	assertLengths(t, rows)

	if rows.Close() != nil {
		t.Error("Close")
	}
}

// assertLengths checks the lengths of string columns.
func assertLengths(t *testing.T, rows *sqlRows) {
	t.Helper()

	for _, typ := range []DataType{
		CharacterType{Length: 1}, VaryingType{Length: 2}, BinaryType{Length: 3}, VarBinaryType{Length: 4},
		CLOBType{Length: 5}, BLOBType{Length: 6},
	} {
		rows.st = &Statement{columns: []Column{{Type: typ}}}
		if n, ok := rows.ColumnTypeLength(0); !ok || n == 0 {
			t.Errorf("%#v has no length", typ)
		}

		if _, _, ok := rows.ColumnTypePrecisionScale(0); ok {
			t.Errorf("%#v has a precision", typ)
		}
	}
}
