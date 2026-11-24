package turborel

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// preparedOf replies Prepared of parameters of types params and columns.
func (f *fake) preparedOf(params []DataType, columns []Column) {
	f.reply(kindPrepared, func(w *writer) {
		w.u32(0)
		w.u32(count32(len(params)))

		for _, p := range params {
			w.flag(false)
			p.encodeType(w)
		}

		encodeColumns(w, columns)
	})
}

// sqlOf reads a Prepare, giving its SQL.
func (f *fake) sqlOf() string {
	r := newReader(f.expect(kindPrepare), "request")
	r.u32()

	return r.str()
}

// refuse replies an Error of sqlstate, the transaction active after it if
// active, its detail detail.
func (f *fake) refuse(sqlstate string, active bool, detail string) {
	f.reply(kindError, func(w *writer) {
		w.failure(&Error{
			SQLCode: -201, SQLState: sqlstate, Message: "refused", Active: active,
			Diagnostics: &Diagnostics{Severity: severityError, Detail: detail},
		})
	})
}

// done replies a Complete, the transaction active after it if active.
func (f *fake) done(active bool) { f.complete("00000", active) }

// TestImportByInsert imports by a prepared INSERT, skipping bad rows.
//
// Verifies: [CLIENT Importing] with no headings the table's columns are
// described by a prepared query and deallocated; the INSERT names them
// delimited, its table's name read as SQL reads it; a field longer than
// 64 KiB for a CLOB column is sent ahead by a locator [CLIENT Large
// objects]; a row SQL refuses, it alone undone, and a record of too few
// fields are skipped and given to the program; the batch is committed at
// the end and the INSERT deallocated.
func TestImportByInsert(t *testing.T) {
	t.Parallel()

	var insert string

	c := openGranted(t, nil, func(f *fake) {
		f.sqlOf()
		f.preparedOf(nil, []Column{
			{Named: true, Name: "ID", Type: ExactType{Precision: 9}}, {Named: true, Name: "BODY", Type: CLOBType{Length: 9}},
		})
		f.expect(kindDeallocate)
		f.done(false)

		insert = f.sqlOf()
		f.preparedOf([]DataType{ExactType{Precision: 9}, CLOBType{Length: 1 << 20}}, nil)
		f.expect(kindLobRequest)
		f.lobLocator(5)
		f.expect(kindLobRequest)
		f.done(true)
		f.expect(kindExecute)
		f.done(true)
		f.expect(kindExecute)
		f.refuse("23000", true, "")
		f.expect(kindQuery)
		f.done(false)
		f.expect(kindDeallocate)
		f.done(false)
	})

	long := Char(strings.Repeat("x", LOBInlineOctets+1))
	source := sourceOf(nil, []Value{DecimalOf(1, 0), long}, []Value{Null{}, Char("x")}, []Value{DecimalOf(3, 0)})

	var skipped []Rejected

	done, err := c.Import(context.Background(), source, Import{Table: "s . t", Skip: true},
		func(r Rejected) { skipped = append(skipped, r) })
	if err != nil || done != (Imported{Rows: 1, Rejected: 2}) || len(skipped) != 2 ||
		skipped[0].Cause.SQL == nil || skipped[1].Cause.Input != "[DESIGN] 1 fields, for 2 columns" {
		t.Errorf("the import gave %+v, %v, %v", done, skipped, err)
	}

	if insert != `INSERT INTO "S"."T" ("ID", "BODY") VALUES (?, ?)` {
		t.Errorf("the INSERT was %s", insert)
	}
}

// insertScript serves an import by INSERT of one column, its INSERT
// prepared from the source's headings, then each step given.
func insertScript(steps ...func(f *fake)) func(f *fake) {
	return func(f *fake) {
		f.sqlOf()
		f.preparedOf([]DataType{ExactType{Precision: 9}}, nil)

		for _, step := range steps {
			step(f)
		}
	}
}

// executed serves an Execute, a Complete in a transaction.
func executed(f *fake) {
	f.expect(kindExecute)
	f.done(true)
}

// replied serves a request of kind, replying a Complete with no
// transaction active.
func replied(kind byte) func(f *fake) {
	return func(f *fake) {
		f.expect(kind)
		f.done(false)
	}
}

// failed serves a request of kind, refusing it with sqlstate.
func failed(kind byte, sqlstate string) func(f *fake) {
	return func(f *fake) {
		f.expect(kind)
		f.refuse(sqlstate, false, "")
	}
}

// headed is a source of one heading, ID, and records of one value each,
// failing after them with failure.
func headed(failure error, ids ...int64) *scriptedSource {
	s := &scriptedSource{headings: []Heading{{Name: "id"}}, failure: failure}
	for i, id := range ids {
		s.records = append(s.records, &Record{Values: []Value{DecimalOf(id, 0)}, Position: Position{Record: uint64(i + 1)}})
	}

	return s
}

// TestImportStops stops an import by INSERT, saying where and why.
//
// Verifies: [CLIENT Importing] rows are committed a batch at a time; a
// row that fails, not skipped, stops the import, the batch rolled back,
// its failure saying where its record began, the rows committed and
// rolled back; so does a source that fails, and a commit that fails; the
// INSERT is deallocated even then, a failure to deallocate the import's;
// a failure that loses the connection stops it with nothing more sent.
func TestImportStops(t *testing.T) {
	t.Parallel()

	sourceFailure := &SourceError{Position: Position{Record: 2}, Message: "gone"}
	cases := []struct {
		script func(f *fake)
		source *scriptedSource
		want   string
	}{
		{insertScript(executed, executed, replied(kindQuery), failed(kindExecute, "23000"), replied(kindDeallocate)),
			headed(nil, 1, 2, 3), "record 3: SQLCODE -201 (SQLSTATE 23000): refused; 2 rows committed, 0 rolled back"},
		{insertScript(executed, replied(kindQuery), replied(kindDeallocate)),
			headed(sourceFailure, 1), "record 2: gone; 0 rows committed, 1 rolled back"},
		{insertScript(executed, failed(kindQuery, "40002"), replied(kindDeallocate)),
			headed(nil, 1), "SQLCODE -201 (SQLSTATE 40002): refused; 0 rows committed, 1 rolled back"},
		{insertScript(executed, replied(kindQuery), failed(kindDeallocate, "26000")),
			headed(nil, 1), "SQLCODE -201 (SQLSTATE 26000): refused; 1 row committed, 0 rolled back"},
		{insertScript(failed(kindExecute, "08006")),
			headed(nil, 1), "record 1: SQLCODE -201 (SQLSTATE 08006): refused; 0 rows committed, 0 rolled back"},
	}

	for _, k := range cases {
		c := openGranted(t, nil, k.script)

		_, err := c.Import(context.Background(), k.source, Import{Table: "T", Batch: 2}, nil)
		if failure, ok := errors.AsType[*ImportError](err); !ok || failure.Error() != k.want {
			t.Errorf("gave %v, want %s", err, k.want)
		}
	}
}

// TestImportRefusedBefore refuses an import before its first row.
//
// Verifies: [CLIENT Importing] an import begins with no transaction
// active; a table's name that is no <table name> is refused; so is a
// source whose headings fail, where it says, and a table the server
// cannot describe or insert into, SQL's failure without its position.
func TestImportRefusedBefore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	active := openGranted(t, nil, func(f *fake) {
		f.expect(kindQuery)
		f.done(true)
	})
	if _, err := active.Query(ctx, "INSERT"); err != nil {
		t.Fatal(err)
	}

	if _, err := active.Import(ctx, headed(nil), ImportInto("T"), nil); err == nil {
		t.Error("an import began within a transaction")
	}

	c := openGranted(t, nil, func(f *fake) {
		f.sqlOf()
		f.reply(kindError, func(w *writer) {
			w.failure(&Error{SQLCode: -103, SQLState: "42000", Message: "no table", Position: 14, Placed: true})
		})
		f.sqlOf()
		f.refuse("42000", false, "")
	})

	headless := &scriptedSource{at: -1, failure: &SourceError{Position: Position{Line: 1, HasLine: true}, Message: "no"}}
	cases := []struct {
		source Source
		table  string
		want   string
	}{
		{headed(nil), "a b", "[ISO/IEC 9075:1992 §5.4] a b is not a <table name>; 0 rows committed, 0 rolled back"},
		{headless, "T", "record 0, line 1: no; 0 rows committed, 0 rolled back"},
		{sourceOf(nil), "T", "SQLCODE -103 (SQLSTATE 42000): no table; 0 rows committed, 0 rolled back"},
		{headed(nil), "T", "SQLCODE -201 (SQLSTATE 42000): refused; 0 rows committed, 0 rolled back"},
	}

	for _, k := range cases {
		_, err := c.Import(ctx, k.source, ImportInto(k.table), nil)
		if failure, ok := errors.AsType[*ImportError](err); !ok || failure.Error() != k.want {
			t.Errorf("gave %v, want %s", err, k.want)
		}
	}
}

// copyScript serves an import by copy of two columns, its INSERT
// prepared from the source's headings and deallocated, then each step
// given.
func copyScript(steps ...func(f *fake)) func(f *fake) {
	return func(f *fake) {
		f.sqlOf()
		f.preparedOf([]DataType{ExactType{Precision: 9}, VaryingType{Length: 3}}, nil)
		f.expect(kindDeallocate)
		f.done(false)

		for _, step := range steps {
			step(f)
		}
	}
}

// copied serves a copy of rows, ended by the Complete or the Error end
// gives.
func copied(end func(f *fake)) func(f *fake) {
	return func(f *fake) {
		f.copyIn()
		f.copyData()
		f.copyEnd()
		end(f)
	}
}

// abandoned serves a copy the client abandons before sending a row.
func abandoned(f *fake) {
	f.copyIn()

	if !f.copyEnd() {
		f.t.Error("the copy was not abandoned")
	}

	f.counted(0)
}

// pairs is a source of two headings and records of an ID and a name.
func pairs(failure error, names ...string) *scriptedSource {
	s := &scriptedSource{headings: []Heading{{Name: "ID"}, {Name: "NAME"}}, failure: failure}
	for i, name := range names {
		values := []Value{DecimalOf(int64(i+1), 0), Char(name)}
		if name == "" {
			values = values[:1]
		}

		s.records = append(s.records, &Record{Values: values, Position: Position{Record: uint64(i + 1)}})
	}

	return s
}

// TestImportByCopy imports by copy, a batch to a copy.
//
// Verifies: [CLIENT Importing] where copy is granted, no row skipped and
// no column a large object's, the INSERT is deallocated once its
// parameters' types are known and the rows go a batch to a copy, each
// committing as it ends [CLIENT Copying and exporting]; a row the server
// refuses fails its batch, the failure at the record its detail names,
// the rows before it rolled back, or at none if it names none; a string
// too long, a record of too few fields and a source that fails stop it
// before the row is sent.
func TestImportByCopy(t *testing.T) {
	t.Parallel()

	refusedAt := func(detail string) func(f *fake) { return func(f *fake) { f.refuse("23000", false, detail) } }
	cases := []struct {
		script func(f *fake)
		source *scriptedSource
		want   string
	}{
		{copyScript(copied(func(f *fake) { f.counted(2) }), copied(refusedAt("row 1 of the copy"))),
			pairs(nil, "a", "b", "c"), "record 3: SQLCODE -201 (SQLSTATE 23000): refused; 2 rows committed, 0 rolled back"},
		{copyScript(copied(refusedAt("row 9 of the copy"))),
			pairs(nil, "a", "b"), "SQLCODE -201 (SQLSTATE 23000): refused; 0 rows committed, 2 rolled back"},
		{copyScript(copied(refusedAt("row x of the copy"))),
			pairs(nil, "a"), "SQLCODE -201 (SQLSTATE 23000): refused; 0 rows committed, 1 rolled back"},
		{copyScript(copied(refusedAt("the row"))),
			pairs(nil, "a"), "SQLCODE -201 (SQLSTATE 23000): refused; 0 rows committed, 1 rolled back"},
		{copyScript(abandoned),
			pairs(nil, "a", "long"), "record 2: SQLCODE -301 (SQLSTATE 22001): a value of 4 characters, not spaces " +
				"beyond 3, does not fit CHARACTER VARYING(3) [ISO/IEC 9075:1992 §13.8 GR4b iii, §9.2 GR3e ii]; " +
				"0 rows committed, 1 rolled back"},
		{copyScript(abandoned),
			pairs(nil, "a", ""), "record 2: [DESIGN] 1 fields, for 2 columns; 0 rows committed, 1 rolled back"},
		{copyScript(abandoned),
			pairs(errDiskGone, "a"), "the disk is gone; 0 rows committed, 1 rolled back"},
	}

	for _, k := range cases {
		c := openGranted(t, []string{CapabilityCopy}, k.script)

		_, err := c.Import(context.Background(), k.source, Import{Table: "T", Batch: 2}, nil)
		if failure, ok := errors.AsType[*ImportError](err); !ok || failure.Error() != k.want {
			t.Errorf("gave %v, want %s", err, k.want)
		}
	}

	whole := openGranted(t, []string{CapabilityCopy}, copyScript(copied(func(f *fake) { f.counted(2) })))
	if done, err := whole.Import(context.Background(), pairs(nil, "a", "b"), Import{Table: "T"}, nil); err != nil ||
		done.Rows != 2 {
		t.Errorf("a whole import by copy gave %+v, %v", done, err)
	}
}

// TestImportWords writes an import's results in words.
//
// Verifies: [CLIENT Importing] a position is its record and its line, if
// it has one; a row skipped says where and why; an import's failure says
// where, why, and the rows committed and rolled back, a row's count in
// words; SQL's failure is the one it wraps; ImportInto commits
// DefaultBatch rows at a time.
func TestImportWords(t *testing.T) {
	t.Parallel()

	at := Position{Record: 7, Line: 9, HasLine: true}
	sql := &Error{SQLCode: -201, SQLState: "23000", Message: "refused", Position: 3, Placed: true}

	cause := sqlCause(sql)
	if cause.SQL.Placed || cause.String() != "SQLCODE -201 (SQLSTATE 23000): refused" ||
		sqlCause(errDiskGone).Input != "the disk is gone" {
		t.Errorf("causes %v", cause)
	}

	stopped := &ImportError{Position: at, Placed: true, Cause: cause, Committed: 1, RolledBack: 2}
	if got := (Rejected{Position: at, Cause: Cause{Input: "short"}}).String(); got != "record 7, line 9: short; skipped" {
		t.Errorf("a row skipped: %s", got)
	}

	if stopped.Error() != "record 7, line 9: SQLCODE -201 (SQLSTATE 23000): refused; 1 row committed, 2 rolled back" ||
		!errors.Is(stopped, cause.SQL) {
		t.Errorf("an import's failure: %v", stopped)
	}

	input := &ImportError{Cause: Cause{Input: "short"}}
	assertFailure(t, input, sqlcodeData, sqlstateDataException, "short")
	assertFailure(t, &SourceError{Message: "gone"}, sqlcodeData, sqlstateDataException, "gone")

	if how := ImportInto("T"); how != (Import{Table: "T", Batch: DefaultBatch}) {
		t.Errorf("ImportInto gave %+v", how)
	}

	if !slices.Equal([]string{rowsText(0), rowsText(1), rowsText(2)}, []string{"0 rows", "1 row", "2 rows"}) {
		t.Error("rows in words")
	}
}
