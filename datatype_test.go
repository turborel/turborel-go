package turborel

import (
	"bytes"
	"reflect"
	"testing"
)

// typeBytes is the bytes t writes.
func typeBytes(t DataType) []byte {
	var w writer

	t.encodeType(&w)

	return w.buf
}

// TestDataTypesRoundTrip writes and reads back a type of every tag.
//
// Verifies: [PROTOCOL Data types] tags 1 to 27 each write their kind's
// fields after the tag, as the table lists them, and read back the same:
// lengths and units, precisions and scales, a plug-in's name and
// parameters, an interval's fields 0 YEAR to 5 SECOND, an array's
// cardinality and element, a row's named fields, a multiset's element, a
// structured type's names and supertypes, a reference's scope, and an
// XML type's modifiers and schema.
func TestDataTypesRoundTrip(t *testing.T) {
	t.Parallel()

	for _, want := range sampleTypes() {
		r := newReader(typeBytes(want), "data type")

		got := r.dataType()
		if err := r.finish(); err != nil {
			t.Errorf("%#v: %v", want, err)

			continue
		}

		if !reflect.DeepEqual(got, want) {
			t.Errorf("read back %#v, want %#v", got, want)
		}
	}
}

// sampleTypes are a type of every tag, with their fields set.
func sampleTypes() []DataType {
	namespace, element := "urn:x", "order"

	return []DataType{
		CharacterType{Length: 10, Units: Octets},
		ExactType{Precision: 38, Scale: 4},
		ApproximateType{Precision: 53},
		PluginType{Name: "POINT", Params: []uint32{2, 3}},
		VaryingType{Length: 255},
		BinaryType{Length: 16},
		VarBinaryType{Length: 32},
		DateType{},
		TimeType{Precision: 6},
		TimeType{Precision: 3, Zoned: true},
		TimestampType{Precision: 6},
		TimestampType{Precision: 0, Zoned: true},
		IntervalType{Qualifier: Qualifier{Start: Year, End: Month, Leading: 4}},
		IntervalType{Qualifier: Qualifier{Start: Day, End: Second, Leading: 2, Fraction: 6}},
		BooleanType{},
		DecFloatType{Precision: 34},
		ArrayType{Max: 10, Element: ArrayType{Max: 65535, Element: BooleanType{}}},
		RowType{Fields: []Field{{Name: "A", Type: ExactType{Precision: 9}}, {Name: "B", Type: JSONType{}}}},
		CLOBType{Length: 1 << 40},
		BLOBType{Length: 1 << 20, Units: Octets},
		MultisetType{Element: CharacterType{Length: 1}},
		StructuredType{Name: UDTName{Schema: "S", Name: "ADDRESS"}, Supertypes: []UDTName{{Schema: "S", Name: "PLACE"}}},
		ReferenceType{Referenced: StructuredType{Name: UDTName{Schema: "S", Name: "T"}}},
		ReferenceType{
			Referenced: StructuredType{Name: UDTName{Schema: "S", Name: "T"}},
			Scope:      &UDTName{Schema: "S", Name: "TT"},
		},
		JSONType{},
		XMLType{Primary: XMLDocument, Secondary: XMLAny},
		XMLType{Primary: XMLSequence, Secondary: XMLUntyped},
		XMLType{Primary: XMLContent, Secondary: XMLXMLSchema, Schema: &XMLSchemaName{Schema: "S", Name: "X"}},
		XMLType{Primary: XMLContent, Secondary: XMLXMLSchema, Schema: &XMLSchemaName{
			Schema: "S", Name: "X", Namespace: &namespace, Element: &element,
		}},
		UUIDType{},
	}
}

// TestDataTypesBytes checks the bytes of types against the spec's table.
//
// Verifies: [PROTOCOL Data types] CHARACTER is tag 1, its length u32 and
// units u8; an interval tag 13 and its four u32 fields; an array tag 16,
// its cardinality, then its element type; UUID tag 27 alone.
func TestDataTypesBytes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		t    DataType
		want []byte
	}{
		{CharacterType{Length: 5, Units: Characters}, []byte{1, 5, 0, 0, 0, 0}},
		{IntervalType{Qualifier: Qualifier{Start: Hour, End: Second, Leading: 2, Fraction: 6}},
			[]byte{13, 3, 0, 0, 0, 5, 0, 0, 0, 2, 0, 0, 0, 6, 0, 0, 0}},
		{ArrayType{Max: 3, Element: BooleanType{}}, []byte{16, 3, 0, 0, 0, 14}},
		{UUIDType{}, []byte{27}},
		{TimestampType{Precision: 6, Zoned: true}, []byte{12, 6, 0, 0, 0}},
	}
	for _, c := range cases {
		if got := typeBytes(c.t); !bytes.Equal(got, c.want) {
			t.Errorf("%#v wrote % X, want % X", c.t, got, c.want)
		}
	}
}

// TestDataTypesRefused refuses types the reference client refuses.
//
// Verifies: [PROTOCOL Data types] a tag no type has (20, 21, 28), units
// other than 0 or 1, an interval field past 5 SECOND, an array of
// cardinality 0 or past 65535, a row of no field, a plug-in's type of
// more than two parameters, and XML modifiers the table does not list
// each fail the connection.
func TestDataTypesRefused(t *testing.T) {
	t.Parallel()

	cases := map[string][]byte{
		"tag 20":            {20},
		"tag 28":            {28},
		"units 2":           {1, 5, 0, 0, 0, 2},
		"interval field 6":  {13, 6, 0, 0, 0, 5, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		"array of 0":        {16, 0, 0, 0, 0, 14},
		"array past 65535":  {16, 0, 0, 1, 0, 14},
		"row of no field":   {17, 0, 0, 0, 0},
		"three parameters":  {4, 1, 0, 0, 0, 'P', 3, 0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0, 3, 0, 0, 0},
		"XML secondary 3":   {26, 0, 3},
		"XML primary 3":     {26, 3, 0},
		"sequence typed":    {26, 2, 1},
		"reference scope 2": {24, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2},
		"nothing":           {},
	}
	for name, body := range cases {
		r := newReader(body, "data type")
		r.dataType()

		if r.finish() == nil {
			t.Errorf("%s: read without failing", name)
		}
	}
}
