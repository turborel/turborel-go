package turborel

import (
	"context"
	"math/big"
	"strings"
	"testing"
)

// thirtyEight is the greatest exact number of 38 digits, and its text.
func thirtyEight(t *testing.T) (Decimal, string) {
	t.Helper()

	text := strings.Repeat("9", 38)

	d, err := NewDecimal(bigOf(t, text), 0)
	if err != nil {
		t.Fatal(err)
	}

	return d, text
}

// TestLiveExactDigits sends and reads exact numbers of 38 digits.
//
// Verifies: [PROTOCOL Values] an exact number of 38 digits round-trips
// with no loss, as a parameter and as a result, natively and through
// database/sql, which gives it as its text, an int64 refusing it and a
// float64 rounding it as database/sql does; one of 39 digits is refused
// by the server (SQLSTATE 22003); a quotient takes the scale turborel
// gives it, 1/3 as 0.333333 [CLIENT Prepared statements] [DESIGN] D2.
func TestLiveExactDigits(t *testing.T) {
	t.Parallel()

	c := liveSchema(t, "EXACT_TEST")
	mustRun(t, c, "CREATE TABLE EXACT_TEST.N (K INTEGER, X NUMERIC(38,0), Y NUMERIC(38,2))")

	ctx := context.Background()
	greatest, text := thirtyEight(t)
	cents, err := NewDecimal(bigOf(t, "-"+text), 2)
	if err != nil {
		t.Fatal(err)
	}

	insert, err := c.Prepare(ctx, "INSERT INTO EXACT_TEST.N VALUES (?, ?, ?)")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := c.Execute(ctx, insert, []Value{DecimalOf(1, 0), greatest, cents}); err != nil {
		t.Fatal(err)
	}

	row := mustRun(t, c, "SELECT X, Y FROM EXACT_TEST.N WHERE K = 1").Rows[0]
	if x, ok := row[0].(Decimal); !ok || x.String() != text || valueText(row[1]) != cents.String() {
		t.Errorf("read back %v", row)
	}

	past, err := NewDecimal(new(big.Int).Exp(big.NewInt(10), big.NewInt(38), nil), 0)
	if err != nil {
		t.Fatal(err)
	}

	_, err = c.Execute(ctx, insert, []Value{DecimalOf(2, 0), past, Null{}})
	assertState(t, err, "22003")

	third := mustRun(t, c, "VALUES 1 / 3").Rows[0][0]
	if valueText(third) != "0.333333" {
		t.Errorf("1/3 is %v", third)
	}

	mustRun(t, c, "COMMIT")
	assertExactThroughSQL(t, text)
}

// assertExactThroughSQL round-trips a 38-digit number through
// database/sql: sent as its text, given back as its text and scanned into
// a Decimal whole; scanned into an int64 it fails, into a float64
// database/sql rounds it.
func assertExactThroughSQL(t *testing.T, text string) {
	t.Helper()

	db := liveDB(t)

	var got string

	var scanned Decimal

	err := db.QueryRowContext(context.Background(), "VALUES (CAST(? AS NUMERIC(38,0)), CAST(? AS NUMERIC(38,0)))",
		text, text).Scan(&got, &scanned)
	if err != nil || got != text || scanned.String() != text {
		t.Errorf("database/sql gave %q and %v, %v", got, scanned, err)
	}

	var narrow int64

	var float float64

	query := "VALUES (CAST(? AS NUMERIC(38,0)))"
	if err := db.QueryRowContext(context.Background(), query, text).Scan(&narrow); err == nil {
		t.Errorf("38 digits scanned into an int64 as %d", narrow)
	}

	if err := db.QueryRowContext(context.Background(), query, text).Scan(&float); err != nil || float != 1e38 {
		t.Errorf("38 digits scanned into a float64 as %v, %v", float, err)
	}
}
