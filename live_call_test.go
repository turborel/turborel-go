package turborel

import (
	"context"
	"database/sql"
	"slices"
	"testing"
)

// liveMixed makes schema.MIXED (OUT TOTAL INTEGER, IN TIMES INTEGER,
// INOUT TAG VARCHAR(10)) over a table of 1, 2 and 3, committed.
func liveMixed(t *testing.T, schema string) *Conn {
	t.Helper()

	c := liveSchema(t, schema)
	mustRun(t, c, "CREATE TABLE "+schema+".T (K INTEGER)")
	mustRun(t, c, "INSERT INTO "+schema+".T VALUES (1), (2), (3)")
	mustRun(t, c, "CREATE PROCEDURE "+schema+".MIXED (OUT TOTAL INTEGER, IN TIMES INTEGER, INOUT TAG VARCHAR(10)) "+
		"SPECIFIC MIXED READS SQL DATA SELECT SUM(K) * TIMES, TAG || 'X' INTO TOTAL, TAG FROM "+schema+".T")
	mustRun(t, c, "COMMIT")

	return c
}

// TestLiveParameterModes prepares a CALL and reads its outputs.
//
// Verifies: [PROTOCOL Parameter modes] granted `parameter-modes`, which
// connecting asks for, a prepared CALL tells each `?` with its
// parameter's mode and ordinal position, the OUT one's too, and the
// procedure's specific name; executed with values for the IN and INOUT
// ones, the OUT and INOUT ones' come back as a row; a query's `?` is IN,
// of no procedure; not granted, the inputs alone, each IN [PROTOCOL
// Capabilities] [CLIENT Prepared statements].
func TestLiveParameterModes(t *testing.T) {
	t.Parallel()

	c := liveMixed(t, "MODES_TEST")
	ctx := context.Background()
	call := "CALL MODES_TEST.MIXED(?, ?, ?)"

	mixed, err := c.Prepare(ctx, call)
	if err != nil {
		t.Fatal(err)
	}

	procedure, told := mixed.Procedure()
	if got := modesOf(mixed); !slices.Equal(got, []string{"OUT 1", "IN 2", "INOUT 3"}) || !told ||
		procedure != (QualifiedName{"MODES_TEST", "MIXED"}) || len(mixed.Inputs()) != 2 {
		t.Errorf("modes %q of %v, %v", got, procedure, told)
	}

	o, err := c.Execute(ctx, mixed, []Value{DecimalOf(2, 0), Char("A")})
	if err != nil || len(o.Rows) != 1 || valueText(o.Rows[0]) != "ROW(12, 'AX')" {
		t.Errorf("the CALL gave %+v, %v", o, err)
	}

	query, err := c.Prepare(ctx, "SELECT K FROM MODES_TEST.T WHERE K = ?")
	if _, told := query.Procedure(); err != nil || told || !slices.Equal(modesOf(query), []string{"IN 0"}) {
		t.Errorf("a query's modes %q, %v", modesOf(query), err)
	}

	offer := DefaultOffer()
	offer.Capabilities = slices.DeleteFunc(offer.Capabilities, func(c string) bool {
		return c == CapabilityParameterModes
	})
	plain := liveConnect(t, liveTLS(t, 0, false), Trust{}, &Config{Timeouts: DefaultTimeouts(), Offer: &offer})

	untold, err := plain.Prepare(ctx, call)
	if _, told := untold.Procedure(); err != nil || told || !slices.Equal(modesOf(untold), []string{"IN 0", "IN 0"}) {
		t.Errorf("not granted, modes %q, %v", modesOf(untold), err)
	}
}

// modesOf gives each `?` of st as its mode and ordinal position.
func modesOf(st *Statement) []string {
	modes := make([]string, 0, len(st.Parameters()))

	for _, p := range st.Parameters() {
		modes = append(modes, p.Mode.String()+" "+valueText(DecimalOf(int64(p.Ordinal), 0)))
	}

	return modes
}

// TestLiveSQLOut calls a procedure through database/sql with sql.Out.
//
// Verifies: [DESIGN] D2 through database/sql an OUT `?` takes an
// sql.Out, an INOUT one an sql.Out with In set, and the CALL fills each
// Dest from its row; an OUT `?` given a value is refused (TB910)
// [PROTOCOL Parameter modes] [CLIENT Prepared statements].
func TestLiveSQLOut(t *testing.T) {
	t.Parallel()

	liveMixed(t, "OUT_TEST")

	db := liveDB(t)
	ctx := context.Background()

	var total int64

	tag := "B"

	_, err := db.ExecContext(ctx, "CALL OUT_TEST.MIXED(?, ?, ?)", sql.Out{Dest: &total}, 3, sql.Out{Dest: &tag, In: true})
	if err != nil || total != 18 || tag != "BX" {
		t.Errorf("the CALL gave %d and %q, %v", total, tag, err)
	}

	_, err = db.ExecContext(ctx, "CALL OUT_TEST.MIXED(?, ?, ?)", 1, 3, "C")
	assertState(t, err, sqlstateMisuse)
}
