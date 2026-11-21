package turborel

import (
	"context"
	"slices"
	"testing"
)

// declaredTable is the table of TestLiveDeclaredTypes: a column of each
// name an exact or approximate number is declared with, one of a
// distinct type and an array.
const declaredTable = "CREATE TABLE DECLARED_TEST.T (I INTEGER, S SMALLINT, B BIGINT, D DECIMAL(9,2), N NUMERIC(5), " +
	"R REAL, P DOUBLE PRECISION, F FLOAT(20), M DECLARED_TEST.MONEY, A INTEGER ARRAY[3])"

// declaredExpected is each column's declared type, its name and as SQL
// writes it.
func declaredExpected() []Declared {
	return []Declared{
		{"INTEGER", "INTEGER"}, {"SMALLINT", "SMALLINT"}, {"BIGINT", "BIGINT"}, {"DECIMAL", "DECIMAL(9,2)"},
		{"NUMERIC", "NUMERIC(5,0)"}, {"REAL", "REAL"}, {"DOUBLE PRECISION", "DOUBLE PRECISION"},
		{"FLOAT", "FLOAT(24)"}, {"USER-DEFINED", "DECLARED_TEST.MONEY"}, {"ARRAY", "INTEGER ARRAY[3]"},
	}
}

// declaredOf gives each column's declared type, a zero one where none
// was told.
func declaredOf(columns []Column) []Declared {
	list := make([]Declared, len(columns))
	for i, c := range columns {
		if c.Declared != nil {
			list[i] = *c.Declared
		}
	}

	return list
}

// TestLiveDeclaredTypes reads the declared types of a table's columns.
//
// Verifies: [PROTOCOL Declared types] granted `declared-types`, which
// connecting asks for, a Prepared and a Description name each column's
// declared type, in the columns' order, its name as DATA_TYPE gives it and
// as SQL writes it, by the page's table; not granted, none, the type
// written the tag's, and the values the same either way [PROTOCOL
// Capabilities] [PROTOCOL The tail] [CLIENT Prepared statements].
func TestLiveDeclaredTypes(t *testing.T) {
	t.Parallel()

	c := liveSchema(t, "DECLARED_TEST")
	mustRun(t, c, "CREATE TYPE DECLARED_TEST.MONEY AS DECIMAL(9,2)")
	mustRun(t, c, declaredTable)
	mustRun(t, c, "INSERT INTO DECLARED_TEST.T VALUES (1, 2, 3, 4.5, 6, 7.5, 8.5, 9.5, 10.25, ARRAY[1, 2])")
	mustRun(t, c, "COMMIT")

	ctx := context.Background()
	every := "SELECT I, S, B, D, N, R, P, F, M, A FROM DECLARED_TEST.T"

	if !c.Granted(CapabilityDeclaredTypes) {
		t.Fatal("declared-types not granted")
	}

	st, err := c.Prepare(ctx, every)
	if err != nil {
		t.Fatal(err)
	}

	told := mustRun(t, c, every)
	if got := declaredOf(st.Columns()); !slices.Equal(got, declaredExpected()) {
		t.Errorf("Prepared told %q", got)
	}

	if got := declaredOf(told.Columns); !slices.Equal(got, declaredExpected()) {
		t.Errorf("Description told %q", got)
	}

	if w := told.Columns[0].TypeWritten(); w != "INTEGER" {
		t.Errorf("INTEGER written %q", w)
	}

	offer := DefaultOffer()
	offer.Capabilities = slices.DeleteFunc(offer.Capabilities, func(c string) bool { return c == CapabilityDeclaredTypes })
	plain := liveConnect(t, liveTLS(t, 0, false), Trust{}, &Config{Timeouts: DefaultTimeouts(), Offer: &offer})
	untold := mustRun(t, plain, every)

	if got := declaredOf(untold.Columns); !slices.Equal(got, make([]Declared, len(got))) {
		t.Errorf("not granted, told %q", got)
	}

	if w := untold.Columns[0].TypeWritten(); w != "NUMERIC(10,0)" {
		t.Errorf("not granted, INTEGER written %q", w)
	}

	if valueLiteral(Array(told.Rows[0])) != valueLiteral(Array(untold.Rows[0])) {
		t.Errorf("the values differ: %v and %v", told.Rows, untold.Rows)
	}
}

// TestLiveDeclaredTypeNames gives database/sql the declared type's name.
//
// Verifies: [DESIGN] D2 a column's DatabaseTypeName is its declared
// type's name, INTEGER of an INTEGER column, DECIMAL of a DECIMAL(9,2)
// one, of a VALUES of CASTs too [PROTOCOL Declared types].
func TestLiveDeclaredTypeNames(t *testing.T) {
	t.Parallel()

	db := liveDB(t)

	rows, err := db.QueryContext(context.Background(),
		"VALUES (CAST(1 AS INTEGER), CAST(2.5 AS DECIMAL(9,2)), CAST(1 AS DOUBLE PRECISION))")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if rows.Close() != nil || rows.Err() != nil {
			t.Error("the rows failed")
		}
	})

	types, err := rows.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}

	names := make([]string, len(types))
	for i, typ := range types {
		names[i] = typ.DatabaseTypeName()
	}

	if !slices.Equal(names, []string{"INTEGER", "DECIMAL", "DOUBLE PRECISION"}) {
		t.Errorf("type names %q", names)
	}
}

// TestLiveDeclaredComputed reads the declared types of computed columns.
//
// Verifies: [PROTOCOL Declared types] values of one number type written
// alike keep its declared type: a VALUES of CASTs, and a UNION of two
// INTEGER columns, declare INTEGER; INTEGER with SMALLINT is NUMERIC
// [CLIENT Prepared statements].
func TestLiveDeclaredComputed(t *testing.T) {
	t.Parallel()

	c := liveSchema(t, "COMPUTED_TEST")
	mustRun(t, c, "CREATE TABLE COMPUTED_TEST.T (I INTEGER, J INTEGER, S SMALLINT)")

	written := map[string][]string{
		"VALUES (CAST(1 AS INTEGER), CAST(2.5 AS DECIMAL(9,2)))":            {"INTEGER", "DECIMAL(9,2)"},
		"SELECT I FROM COMPUTED_TEST.T UNION SELECT J FROM COMPUTED_TEST.T": {"INTEGER"},
	}

	for sql, want := range written {
		st, err := c.Prepare(context.Background(), sql)
		if err != nil {
			t.Fatal(err)
		}

		got := make([]string, len(st.Columns()))
		for i, col := range st.Columns() {
			got[i] = col.TypeWritten()
		}

		if !slices.Equal(got, want) {
			t.Errorf("%s declared %q", sql, got)
		}
	}

	mixed := mustRun(t, c, "SELECT I FROM COMPUTED_TEST.T UNION SELECT S FROM COMPUTED_TEST.T")
	if d := mixed.Columns[0].Declared; d == nil || d.Name != "NUMERIC" {
		t.Errorf("INTEGER with SMALLINT declared %+v", d)
	}
}
