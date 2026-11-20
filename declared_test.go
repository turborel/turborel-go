package turborel

import (
	"context"
	"testing"
)

// integerColumn is a column K declared INTEGER, its tag's type NUMERIC
// of precision 10 and scale 0.
func integerColumn() Column {
	return Column{
		Named: true, Name: "K", Type: ExactType{Precision: 10}, NotNull: true,
		Declared: &Declared{Name: "INTEGER", Written: "INTEGER"},
	}
}

// TestDeclaredTold reads the declared types a Prepared and a
// Description tell.
//
// Verifies: [PROTOCOL Declared types] granted `declared-types`, a
// Prepared and a Description end with tail 19, each column's declared
// type in the columns' order, its name and as SQL writes it; a
// Description with no tail tells none, and the type written is its
// tag's [PROTOCOL The tail] [PROTOCOL Capabilities] [CLIENT Prepared
// statements].
func TestDeclaredTold(t *testing.T) {
	t.Parallel()

	told := []Column{integerColumn(), {Type: VaryingType{Length: 5}}}
	c := openGranted(t, []string{CapabilityDeclaredTypes}, func(f *fake) {
		f.sqlOf()
		f.preparedOf(nil, told)
		f.expect(kindQuery)
		f.reply(kindDescription, func(w *writer) { encodeColumns(w, told) })
		f.done(false)
		f.expect(kindQuery)
		f.reply(kindDescription, func(w *writer) { encodeColumns(w, []Column{{Type: ExactType{Precision: 10}}}) })
		f.done(false)
	})

	ctx := context.Background()

	st, err := c.Prepare(ctx, "SELECT K, V FROM T")
	if err != nil {
		t.Fatal(err)
	}

	want := []Declared{{"INTEGER", "INTEGER"}, {"CHARACTER VARYING", "CHARACTER VARYING(5)"}}
	if got := declaredOf(st.Columns()); got[0] != want[0] || got[1] != want[1] {
		t.Errorf("Prepared told %q", got)
	}

	o, err := c.Query(ctx, "SELECT K, V FROM T")
	if err != nil || len(o.Columns) != 2 || o.Columns[0].TypeWritten() != "INTEGER" ||
		o.Columns[1].TypeWritten() != "CHARACTER VARYING(5)" {
		t.Errorf("Description gave %+v, %v", o, err)
	}

	untold, err := c.Query(ctx, "SELECT K FROM T")
	if err != nil || untold.Columns[0].Declared != nil || untold.Columns[0].TypeWritten() != "NUMERIC(10,0)" {
		t.Errorf("a Description of no tail gave %+v, %v", untold, err)
	}
}

// TestDeclaredMalformed loses the connection of a malformed tail 19.
//
// Verifies: [PROTOCOL Declared types] declared types not one for each
// column, or a tag a Description does not take, fail the connection
// (SQLCODE -919, SQLSTATE 08006) [PROTOCOL The tail] [PROTOCOL Errors
// and notices].
func TestDeclaredMalformed(t *testing.T) {
	t.Parallel()

	tails := map[string]func(w *writer){
		"too few": func(w *writer) {
			w.u8(tailDeclared)
			w.u32(0)
		},
		"out of place": func(w *writer) {
			w.u8(tailTime)
		},
		"cut short": func(w *writer) {
			w.u8(tailDeclared)
			w.u32(1)
			w.str("INTEGER")
		},
	}

	for name, tail := range tails {
		c := openGranted(t, []string{CapabilityDeclaredTypes}, func(f *fake) {
			f.expect(kindQuery)
			f.reply(kindDescription, func(w *writer) {
				encodeColumns(w, []Column{{Type: BooleanType{}}})
				tail(w)
			})
		})

		_, err := c.Query(context.Background(), "VALUES TRUE")
		assertState(t, err, sqlstateConnectionFailure)

		if !c.IsLost() {
			t.Errorf("%s: the connection kept", name)
		}
	}
}

// TestTypeWritten writes each data type as SQL writes it.
//
// Verifies: [PROTOCOL Declared types] the type a column's tag says, as
// turborel writes it where no declared type was told: an exact numeric
// type as NUMERIC, an approximate one as FLOAT [PROTOCOL Data types].
func TestTypeWritten(t *testing.T) {
	t.Parallel()

	for want, typ := range writtenTypes() {
		if got := (Column{Type: typ}).TypeWritten(); got != want {
			t.Errorf("%#v written %q, want %q", typ, got, want)
		}
	}
}

// TestDeclaredTypeName gives database/sql the declared type's name.
//
// Verifies: [DESIGN] D2 a column's DatabaseTypeName is its declared
// type's name where the server told it, else the name of its tag's type
// [PROTOCOL Declared types].
func TestDeclaredTypeName(t *testing.T) {
	t.Parallel()

	rows := &sqlRows{st: &Statement{columns: []Column{integerColumn(), {Type: ExactType{Precision: 10}}}}}
	if a, b := rows.ColumnTypeDatabaseTypeName(0), rows.ColumnTypeDatabaseTypeName(1); a != "INTEGER" || b != "NUMERIC" {
		t.Errorf("type names %q and %q", a, b)
	}
}

// writtenTypes is each data type of TestTypeWritten, by the text it is
// written as.
func writtenTypes() map[string]DataType {
	empty, urn, element := "", "urn:po's", `po"x`
	shape := StructuredType{Name: UDTName{"S", "SHAPE"}}
	return map[string]DataType{
		"CHARACTER(4)":                     CharacterType{Length: 4},
		"CHARACTER(16 OCTETS)":             CharacterType{Length: 16, Units: Octets},
		"CHARACTER VARYING(20)":            VaryingType{Length: 20},
		"NUMERIC(10,0)":                    ExactType{Precision: 10},
		"FLOAT(53)":                        ApproximateType{Precision: 53},
		"ST_GEOMETRY":                      PluginType{Name: "ST_GEOMETRY"},
		"BINARY(16)":                       BinaryType{Length: 16},
		"BINARY VARYING(255)":              VarBinaryType{Length: 255},
		"DATE":                             DateType{},
		"TIME(3)":                          TimeType{Precision: 3},
		"TIME(0) WITH TIME ZONE":           TimeType{Zoned: true},
		"TIMESTAMP(6)":                     TimestampType{Precision: 6},
		"TIMESTAMP(6) WITH TIME ZONE":      TimestampType{Precision: 6, Zoned: true},
		"INTERVAL DAY(2) TO SECOND(6)":     IntervalType{Qualifier{Day, Second, 2, 6}},
		"BOOLEAN":                          BooleanType{},
		"DECFLOAT(34)":                     DecFloatType{Precision: 34},
		"CHARACTER LARGE OBJECT(1048576)":  CLOBType{Length: 1 << 20},
		"CHARACTER LARGE OBJECT(9 OCTETS)": CLOBType{Length: 9, Units: Octets},
		"BINARY LARGE OBJECT(65536)":       BLOBType{Length: 1 << 16, Units: Octets},
		"BOOLEAN ARRAY[3]":                 ArrayType{Max: 3, Element: BooleanType{}},
		"CHARACTER(2) MULTISET":            MultisetType{Element: CharacterType{Length: 2}},
		"ROW(X CHARACTER VARYING(3), Y BOOLEAN)": RowType{Fields: []Field{
			{"X", VaryingType{Length: 3}}, {"Y", BooleanType{}},
		}},
		"S.SHAPE":                shape,
		"REF(S.SHAPE)":           ReferenceType{Referenced: shape},
		"REF(S.SHAPE) SCOPE S.T": ReferenceType{Referenced: shape, Scope: &UDTName{"S", "T"}},
		"JSON":                   JSONType{},
		"XML(DOCUMENT(UNTYPED))": XMLType{},
		"XML(CONTENT(ANY))":      XMLType{Primary: XMLContent, Secondary: XMLAny},
		"XML(SEQUENCE)":          XMLType{Primary: XMLSequence},
		"XML(CONTENT(XMLSCHEMA ID S.PO))": XMLType{
			Primary: XMLContent, Secondary: XMLXMLSchema, Schema: &XMLSchemaName{Schema: "S", Name: "PO"},
		},
		"XML(DOCUMENT(XMLSCHEMA ID S.PO NO NAMESPACE))": XMLType{
			Secondary: XMLXMLSchema, Schema: &XMLSchemaName{Schema: "S", Name: "PO", Namespace: &empty},
		},
		`XML(DOCUMENT(XMLSCHEMA ID S.PO NAMESPACE 'urn:po''s' ELEMENT "po""x"))`: XMLType{
			Secondary: XMLXMLSchema, Schema: &XMLSchemaName{Schema: "S", Name: "PO", Namespace: &urn, Element: &element},
		},
		"UUID": UUIDType{},
	}
}

// TestDeclaredWritten writes the declared types of columns.
//
// Verifies: [PROTOCOL Declared types] tail 19 written of columns one of
// which has a declared type, one with none written as its type says, and
// read back [PROTOCOL The tail].
func TestDeclaredWritten(t *testing.T) {
	t.Parallel()

	var w writer

	encodeColumns(&w, []Column{integerColumn(), {Type: ApproximateType{Precision: 53}}})

	r := newReader(w.buf, "test")
	got := declaredOf(r.describedColumns())

	if r.finish() != nil || got[0] != *integerColumn().Declared || got[1] != (Declared{"FLOAT", "FLOAT(53)"}) {
		t.Errorf("declared types written %q", got)
	}
}
