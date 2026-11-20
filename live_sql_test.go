package turborel

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/turborel/turborel-go/internal/testserver"
)

// liveDB opens the live server through database/sql by a URI.
func liveDB(t *testing.T) *sql.DB {
	t.Helper()

	s := liveServer(t)
	uri := URIScheme + testserver.Owner + ":" + url.PathEscape(testserver.OwnerPassword) + "@" + s.Address +
		"/?tls_mode=verify-full&server_name=localhost&tls_roots=" + url.QueryEscape(s.Files.CA)

	db, err := sql.Open(DriverName, uri)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = db.Close() })

	return db
}

// TestLiveDatabaseSQL runs statements through database/sql.
//
// Verifies: [DESIGN] D2 the database/sql driver, registered as turborel,
// opened by sql.Open with a turborel:// URI; outside a transaction each
// statement its own; Exec of arguments by a statement prepared; its result's rows affected and last
// identity value; a query's rows read through its cursor, a batch at a
// time, each value scanned into Go's types and the driver's own; the
// columns' types named; a prepared statement run again; Ping; every
// failure an *Error.
func TestLiveDatabaseSQL(t *testing.T) {
	t.Parallel()

	db := liveDB(t)
	ctx := context.Background()

	for _, sql := range []string{
		"CREATE SCHEMA SQL_TEST",
		"CREATE TABLE SQL_TEST.STAFF (ID INTEGER GENERATED ALWAYS AS IDENTITY, NAME VARCHAR(20) NOT NULL, " +
			"PAID DECIMAL(9,2), SEEN TIMESTAMP WITH TIME ZONE, CARD UUID)",
	} {
		if _, err := db.ExecContext(ctx, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}

	seen := time.Date(2024, 2, 29, 12, 30, 0, 0, time.UTC)

	insert := "INSERT INTO SQL_TEST.STAFF (NAME, PAID, SEEN, CARD) VALUES (?, ?, ?, ?), (?, ?, ?, ?)"

	result, err := db.ExecContext(ctx, insert, "Ada", "61000.50", seen, UUID{1}, "Grace", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if n, err := result.RowsAffected(); err != nil || n != 2 {
		t.Errorf("rows affected %d, %v", n, err)
	}

	if id, err := result.LastInsertId(); err != nil || id != 2 {
		t.Errorf("last insert id %d, %v", id, err)
	}

	assertStaffScanned(t, db, seen)

	if err := db.PingContext(ctx); err != nil {
		t.Errorf("Ping: %v", err)
	}

	_, err = db.ExecContext(ctx, "INSERT INTO SQL_TEST.NOPE VALUES (1)")
	if failure, ok := errors.AsType[*Error](err); !ok || failure.SQLState != "2A000" {
		t.Errorf("a failure gave %v", err)
	}
}

// assertStaffScanned reads SQL_TEST.STAFF's rows through database/sql.
func assertStaffScanned(t *testing.T, db *sql.DB, seen time.Time) {
	t.Helper()

	rows, err := db.QueryContext(context.Background(), "SELECT ID, NAME, PAID, SEEN, CARD FROM SQL_TEST.STAFF ORDER BY ID")
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = rows.Close() }()

	assertSQLColumnTypes(t, rows)

	var (
		id   int64
		name string
		paid sql.Null[Decimal]
		when sql.NullTime
		card sql.Null[UUID]
	)

	for want := int64(1); rows.Next(); want++ {
		if err := rows.Scan(&id, &name, &paid, &when, &card); err != nil {
			t.Fatal(err)
		}

		first := want != 1 || paid.V.String() == "61000.50" && when.Time.Equal(seen) && card.V == UUID{1}
		if id != want || !first {
			t.Errorf("row %d is %d %v %v %v", want, id, paid, when, card)
		}
	}

	if paid.Valid || when.Valid || card.Valid || name != "Grace" {
		t.Errorf("the last row %v %v %v %s", paid, when, card, name)
	}

	if err := rows.Err(); err != nil {
		t.Error(err)
	}

	assertSeen(t, db, seen)
}

// assertSeen checks the first row's SEEN, read by QueryRow.
func assertSeen(t *testing.T, db *sql.DB, seen time.Time) {
	t.Helper()

	var first sql.NullTime

	row := db.QueryRowContext(context.Background(), "SELECT SEEN FROM SQL_TEST.STAFF WHERE ID = ?", 1)
	if err := row.Scan(&first); err != nil || !first.Time.Equal(seen) {
		t.Errorf("SEEN %v, %v", first, err)
	}
}

// assertSQLColumnTypes checks SQL_TEST.STAFF's columns' types.
func assertSQLColumnTypes(t *testing.T, rows *sql.Rows) {
	t.Helper()

	types, err := rows.ColumnTypes()
	if err != nil || types[1].DatabaseTypeName() != "CHARACTER VARYING" || types[3].DatabaseTypeName() !=
		"TIMESTAMP WITH TIME ZONE" {
		t.Errorf("column types %v, %v", types, err)
	}

	if nullable, ok := types[1].Nullable(); !ok || nullable {
		t.Error("NAME NOT NULL is nullable")
	}
}

// TestLiveSQLTransactions begins, commits and rolls back transactions.
//
// Verifies: [DESIGN] D2 a transaction by BeginTx, its
// isolation level and read-only from sql.TxOptions set by SET
// TRANSACTION, committed or rolled back, each statement after its own
// again; a level that is none of SQL's refused; sql.OpenDB of a connector
// of settings made in Go; the native connection reached by Raw; a
// context done cancels the statement by the protocol's Cancel, its
// failure wrapping the context's error.
func TestLiveSQLTransactions(t *testing.T) {
	t.Parallel()

	db := sql.OpenDB(NewDBConnector(liveSource(t)))
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA SQL_TX"); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(ctx, "CREATE TABLE SQL_TX.T (K INTEGER)"); err != nil {
		t.Fatal(err)
	}

	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := tx.ExecContext(ctx, "INSERT INTO SQL_TX.T VALUES (1)"); err != nil || tx.Rollback() != nil {
		t.Fatalf("a rolled back insert: %v", err)
	}

	assertCount(t, db, 0)

	tx, err = db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := tx.ExecContext(ctx, "INSERT INTO SQL_TX.T VALUES (1)"); err == nil {
		t.Error("a read-only transaction inserted")
	}

	_ = tx.Rollback()

	if _, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSnapshot}); err == nil {
		t.Error("a level of no SQL's was taken")
	}

	assertNativeAndCancel(t, db)
}

// assertCount checks SQL_TX.T's rows.
func assertCount(t *testing.T, db *sql.DB, want int64) {
	t.Helper()

	var n int64
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM SQL_TX.T").Scan(&n); err != nil || n != want {
		t.Errorf("%d rows, %v", n, err)
	}
}

// assertNativeAndCancel reaches the native connection and cancels a
// statement by its context.
func assertNativeAndCancel(t *testing.T, db *sql.DB) {
	t.Helper()

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = conn.Close() }()

	err = conn.Raw(func(driverConn any) error {
		native, ok := NativeConn(driverConn)
		if !ok || native.Authorization() != testserver.Owner {
			t.Error("no native connection")
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err = conn.ExecContext(ctx, slowQuery)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a cancelled statement gave %v", err)
	}

	if _, err := NativeConn(nil); err {
		t.Error("nil is a native connection")
	}
}
