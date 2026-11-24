package turborel

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"slices"
	"testing"
)

// mixedCall is what a Prepared of CALL MIXED(?, ?, ?) tells of MIXED (OUT
// TOTAL INTEGER, IN TIMES INTEGER, INOUT TAG VARCHAR(10)).
func mixedCall() *called {
	integer, tag := ExactType{Precision: 10}, VaryingType{Length: 10}

	return &called{procedure: QualifiedName{"S", "MIXED"}, parameters: []Parameter{
		{Type: integer, Mode: ModeOut, Ordinal: 1},
		{Type: integer, Mode: ModeIn, Ordinal: 2},
		{Type: tag, Mode: ModeInOut, Ordinal: 3},
	}}
}

// mixedColumns is MIXED's outputs, TOTAL and TAG, as a CALL's columns.
func mixedColumns() []Column {
	return []Column{
		{Named: true, Name: "TOTAL", Type: ExactType{Precision: 10}},
		{Named: true, Name: "TAG", Type: VaryingType{Length: 10}},
	}
}

// preparedCall replies a Prepared of told's inputs, columns and tail 20.
func (f *fake) preparedCall(told *called, columns []Column) {
	f.reply(kindPrepared, func(w *writer) {
		w.u32(0)

		var inputs []Parameter

		if told != nil {
			for _, p := range told.parameters {
				if p.Mode != ModeOut {
					inputs = append(inputs, p)
				}
			}
		}

		w.u32(count32(len(inputs)))

		for _, p := range inputs {
			w.flag(p.Varying)
			p.Type.encodeType(w)
		}

		encodeColumns(w, columns)
		encodeCalled(w, told)
	})
}

// TestCalledTold reads a CALL's parameters' modes from tail 20.
//
// Verifies: [PROTOCOL Parameter modes] granted `parameter-modes`, a
// Prepared of a CALL ends with tail 20: the procedure's specific name,
// then every `?` in the order written, OUT ones too, each its mode, its
// parameter's ordinal position and its type; the head's parameters the
// inputs alone; a Prepared of no tail 20 has its inputs alone, each IN,
// of no procedure [PROTOCOL The tail] [CLIENT Prepared statements].
func TestCalledTold(t *testing.T) {
	t.Parallel()

	c := openGranted(t, []string{CapabilityParameterModes}, func(f *fake) {
		f.sqlOf()
		f.preparedCall(mixedCall(), mixedColumns())
		f.sqlOf()
		f.preparedOf([]DataType{ExactType{Precision: 10}}, nil)
	})

	mixed, err := c.Prepare(context.Background(), "CALL S.MIXED(?, ?, ?)")
	if err != nil {
		t.Fatal(err)
	}

	procedure, ok := mixed.Procedure()
	modes := modesOf(mixed)

	if !ok || procedure != (QualifiedName{"S", "MIXED"}) || !slices.Equal(modes, []string{"OUT 1", "IN 2", "INOUT 3"}) ||
		len(mixed.Inputs()) != 2 {
		t.Errorf("CALL told %v %v, modes %q, inputs %v", procedure, ok, modes, mixed.Inputs())
	}

	query, err := c.Prepare(context.Background(), "SELECT K FROM T WHERE K = ?")
	if _, told := query.Procedure(); err != nil || told || query.Parameters()[0].Mode != ModeIn ||
		query.Parameters()[0].Ordinal != 0 {
		t.Errorf("a query told %+v, %v", query.Parameters(), err)
	}

	if ParameterMode(3).String() != "mode 3" {
		t.Error("a mode of no code in words")
	}
}

// TestCalledMalformed loses the connection of a malformed tail 20.
//
// Verifies: [PROTOCOL Parameter modes] a mode of another code than 1, 2
// or 4, or an ordinal position of 0, fails the connection (SQLCODE -919,
// SQLSTATE 08006); so does tail 20 before tail 19, or on a Description
// [PROTOCOL The tail] [PROTOCOL Errors and notices].
func TestCalledMalformed(t *testing.T) {
	t.Parallel()

	bad := func(mode ParameterMode, ordinal uint32) func(w *writer) {
		return func(w *writer) {
			encodeCalled(w, &called{parameters: []Parameter{{Type: BooleanType{}, Mode: mode, Ordinal: ordinal}}})
		}
	}
	tails := map[string]func(w *writer){
		"mode 3":    bad(3, 1),
		"ordinal 0": bad(ModeIn, 0),
		"20, then 19": func(w *writer) {
			bad(ModeIn, 1)(w)
			w.u8(tailDeclared)
		},
	}

	for name, tail := range tails {
		c := openGranted(t, []string{CapabilityParameterModes}, func(f *fake) {
			f.sqlOf()
			f.reply(kindPrepared, func(w *writer) {
				w.u32(0)
				w.u32(0)
				encodeColumns(w, nil)
				tail(w)
			})
		})

		_, err := c.Prepare(context.Background(), "CALL S.P(?)")
		assertState(t, err, sqlstateConnectionFailure)

		if !c.IsLost() {
			t.Errorf("%s: the connection kept", name)
		}
	}

	var w writer

	encodeColumns(&w, nil)
	bad(ModeIn, 1)(&w)

	_, err := decodeReply(kindDescription, w.buf)
	assertState(t, err, sqlstateConnectionFailure)
}

// callScript serves a prepared CALL of MIXED, executed with the values
// given checked: TOTAL 12 and TAG AX back.
func callScript(t *testing.T, f *fake) {
	t.Helper()

	f.sqlOf()
	f.preparedCall(mixedCall(), mixedColumns())

	r := newReader(f.expect(kindExecute), "request")
	r.u32()

	if given := r.values(); len(given) != 2 || valueText(given[0]) != "2" || valueText(given[1]) != "A" {
		t.Errorf("the CALL sent %v", given)
	}

	f.reply(kindRows, func(w *writer) {
		w.u32(1)
		encodeValues(w, []Value{DecimalOf(12, 0), Char("AX")})
	})
	f.complete("00000", false)
	f.expect(kindDeallocate)
	f.complete("00000", false)
}

// TestSQLOut calls a procedure through database/sql with sql.Out.
//
// Verifies: [DESIGN] D2 a CALL's `?`s through database/sql by their
// modes, told by `parameter-modes`: an IN one takes a value, an OUT one
// an sql.Out its CALL's row fills, an INOUT one an sql.Out with In set,
// its value sent and filled again; only the IN and INOUT values are sent
// [PROTOCOL Parameter modes] [CLIENT Prepared statements].
func TestSQLOut(t *testing.T) {
	t.Parallel()

	c := &sqlConn{conn: openGranted(t, []string{CapabilityParameterModes}, func(f *fake) { callScript(t, f) })}

	var total int32

	tag := "A"
	args := []driver.NamedValue{
		{Ordinal: 1, Value: sql.Out{Dest: &total}},
		{Ordinal: 2, Value: int64(2)},
		{Ordinal: 3, Value: sql.Out{Dest: &tag, In: true}},
	}

	for i := range args {
		if err := c.CheckNamedValue(&args[i]); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := c.ExecContext(context.Background(), "CALL S.MIXED(?, ?, ?)", args); err != nil {
		t.Fatal(err)
	}

	if total != 12 || tag != "AX" {
		t.Errorf("the outputs gave %d and %q", total, tag)
	}
}

// TestSQLOutMisused refuses sql.Out where a CALL's modes do not take it.
//
// Verifies: [DESIGN] D2 an OUT `?` given a value, or an sql.Out with In
// set, an IN one given an sql.Out, an INOUT one an sql.Out without In, an
// sql.Out of no pointer, a count of arguments not the `?`s', and an
// sql.Out to a statement whose modes were not told are each refused
// (SQLCODE -910, SQLSTATE TB910); an output is put in its Dest by its
// Scan, as it is, or as another of Go's numbers only where nothing is
// lost [PROTOCOL Parameter modes].
func TestSQLOutMisused(t *testing.T) {
	t.Parallel()

	var n int64

	out, in := Parameter{Mode: ModeOut, Ordinal: 1}, Parameter{Mode: ModeIn, Ordinal: 2}
	both := Parameter{Mode: ModeInOut, Ordinal: 3}
	refused := []struct {
		p   Parameter
		arg any
	}{
		{out, int64(1)}, {out, sql.Out{Dest: &n, In: true}}, {in, sql.Out{Dest: &n}},
		{both, sql.Out{Dest: &n}}, {both, sql.Out{Dest: n, In: true}},
	}

	for _, r := range refused {
		_, _, err := bindOne(r.p, r.arg, 1)
		assertState(t, err, sqlstateMisuse)
	}

	if v, o, err := bindOne(both, int64(5), 3); err != nil || o != nil || valueText(v) != "5" {
		t.Errorf("an INOUT ? of a value gave %v, %v, %v", v, o, err)
	}

	told := &sqlStmt{st: &Statement{called: mixedCall()}}
	_, _, err := told.bind([]driver.NamedValue{{Value: int64(1)}})
	assertState(t, err, sqlstateMisuse)

	untold := &sqlStmt{st: &Statement{inputs: []Parameter{in}}}
	_, _, err = untold.bind([]driver.NamedValue{{Value: sql.Out{Dest: &n}}})
	assertState(t, err, sqlstateMisuse)

	assertOutputs(t)
}

// assertOutputs puts outputs in Dests of several types.
func assertOutputs(t *testing.T) {
	t.Helper()

	var (
		small    int8
		text     string
		anything any = 1
		nullable sql.NullString
		pointer  = new(int)
	)

	for _, ok := range []error{
		assignOut(&small, int64(100)), assignOut(&text, []byte("b")), assignOut(&anything, nil),
		assignOut(&nullable, "s"), assignOut(&pointer, nil),
	} {
		if ok != nil {
			t.Error(ok)
		}
	}

	if small != 100 || text != "b" || anything != nil || nullable.String != "s" || pointer != nil {
		t.Errorf("put %d, %q, %v, %v, %v", small, text, anything, nullable, pointer)
	}

	for _, bad := range []error{
		assignOut(&small, int64(1000)), assignOut(&small, 1.5), assignOut(&text, nil), assignOut(small, int64(1)),
		assignOut(&text, true),
	} {
		assertState(t, bad, sqlstateMisuse)
	}
}
