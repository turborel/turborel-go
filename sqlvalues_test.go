package turborel

import (
	"context"
	"database/sql/driver"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"
)

// valuer is a program's type sent as what it gives.
type valuer struct {
	v   driver.Value
	err error
}

func (v valuer) Value() (driver.Value, error) { return v.v, v.err }

// TestValuesSent makes Go values the values they are sent as.
//
// Verifies: [DESIGN] D2 a Go value is sent as the
// driver's value it is: nil a null, each integer an exact number, a float
// an approximate one, a bool, a string, bytes, a time.Time a TIMESTAMP
// WITH TIME ZONE to the microsecond, the driver's own values as they are,
// a driver.Valuer as what it gives; another type refused (SQLSTATE
// TB910).
func TestValuesSent(t *testing.T) {
	t.Parallel()

	at := time.Date(2024, 2, 29, 12, 0, 0, 1500, time.FixedZone("", 3600))
	cases := []struct {
		in   any
		want Value
	}{
		{nil, Null{}}, {int8(-3), DecimalOf(-3, 0)}, {uint64(math.MaxUint64), mustDecimal(t, "18446744073709551615")},
		{3.5, Approx(3.5)}, {float32(0.5), Approx(0.5)}, {true, Bool(true)}, {"a", Char("a")}, {[]byte{1}, Binary{1}},
		{Char("x"), Char("x")}, {valuer{v: int64(7)}, DecimalOf(7, 0)},
		{at, Datetime{Moment: MomentTimestamp, UTC: instant(at), Zone: 60, Zoned: true, Precision: 6}},
	}

	for _, c := range cases {
		if got, err := valueOf(c.in); err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%#v gave %#v, %v", c.in, got, err)
		}
	}

	_, err := valueOf(struct{}{})
	assertState(t, err, sqlstateMisuse)

	if _, err := valueOf(valuer{err: errDiskGone}); !errors.Is(err, errDiskGone) {
		t.Errorf("a Valuer's failure gave %v", err)
	}
}

// mustDecimal is the exact number of text.
func mustDecimal(t *testing.T, text string) Decimal {
	t.Helper()

	d, err := parseDecimal(text)
	if err != nil {
		t.Fatal(err)
	}

	return d
}

// TestValuesRead gives values read as the Go values database/sql scans.
//
// Verifies: [DESIGN] D2 a value read is given as Go's
// simplest type: a null nil, an exact number of scale 0 an int64 holds an
// int64, another its text as a DECFLOAT's; an approximate number a
// float64; a string a string; bytes; a bool; a datetime a time.Time; a
// large object held whole a string or bytes; any other the driver's own;
// each column type named and its scan type the Go type given.
func TestValuesRead(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   Value
		want driver.Value
	}{
		{Null{}, nil}, {DecimalOf(7, 0), int64(7)}, {DecimalOf(150, 2), "1.50"}, {decFloat(t, 15, -1), "1.5"},
		{Approx(2), 2.0}, {Char("a"), "a"}, {Binary{1}, []byte{1}}, {Bool(true), true},
		{LOB{Kind: CharacterLOB, Octets: []byte("c")}, "c"}, {LOB{Kind: BinaryLOB, Octets: []byte{2}}, []byte{2}},
		{UUID{3}, UUID{3}},
	}

	for _, c := range cases {
		if got, err := driverValue(t.Context(), nil, c.in); err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%#v gave %#v, %v", c.in, got, err)
		}
	}

	types := []struct {
		t    DataType
		name string
		scan reflect.Type
	}{
		{ExactType{Precision: 9}, "NUMERIC", reflect.TypeFor[int64]()},
		{ExactType{Precision: 9, Scale: 2}, "NUMERIC", reflect.TypeFor[string]()},
		{TimeType{Zoned: true}, "TIME WITH TIME ZONE", reflect.TypeFor[time.Time]()},
		{TimestampType{}, "TIMESTAMP", reflect.TypeFor[time.Time]()},
		{BLOBType{}, "BINARY LARGE OBJECT", reflect.TypeFor[[]byte]()},
		{ApproximateType{}, "FLOAT", reflect.TypeFor[float64]()}, {BooleanType{}, "BOOLEAN", reflect.TypeFor[bool]()},
		{UUIDType{}, "UUID", reflect.TypeFor[any]()}, {PluginType{Name: "POINT"}, "POINT", reflect.TypeFor[any]()},
		{StructuredType{}, "USER-DEFINED", reflect.TypeFor[any]()},
	}

	for _, c := range types {
		if typeName(c.t) != c.name || scanType(c.t) != c.scan {
			t.Errorf("%#v is %s, scanned as %v", c.t, typeName(c.t), scanType(c.t))
		}
	}

	for _, t2 := range []DataType{
		CharacterType{}, VaryingType{}, BinaryType{}, VarBinaryType{}, DateType{}, IntervalType{}, DecFloatType{}, CLOBType{},
		ArrayType{}, MultisetType{}, RowType{}, ReferenceType{}, JSONType{}, XMLType{},
	} {
		if typeName(t2) == "USER-DEFINED" {
			t.Errorf("%#v has no name", t2)
		}
	}
}

// TestLocatorRead reads a large object sent by its locator whole, for
// database/sql.
//
// Verifies: [DESIGN] D2 a large object that came by
// its locator is given to database/sql whole, read a chunk a request, a
// CLOB a string and a BLOB bytes; a failure to read it is Next's.
func TestLocatorRead(t *testing.T) {
	t.Parallel()

	c := openGranted(t, nil, func(f *fake) {
		f.expect(kindLobRequest)
		f.lobData([]byte("abc"))
		f.expect(kindLobRequest)
		f.lobData([]byte{1})
		f.expect(kindLobRequest)
		f.failure(-301, "0F001")
	})

	if got, err := driverValue(t.Context(), c, Locator{Kind: CharacterLOB, Octets: 3}); err != nil || got != "abc" {
		t.Errorf("a CLOB gave %#v, %v", got, err)
	}

	if got, err := driverValue(t.Context(), c, Locator{Kind: BinaryLOB, Octets: 1}); err != nil ||
		!reflect.DeepEqual(got, []byte{1}) {
		t.Errorf("a BLOB gave %#v, %v", got, err)
	}

	_, err := driverValue(t.Context(), c, Locator{Kind: BinaryLOB, Octets: 1})
	assertState(t, err, "0F001")
}

// TestWideExactGivenAsText gives an exact number past an int64 as its
// text.
//
// Verifies: [DESIGN] D2 database/sql is given an exact number of scale 0
// and at most 18 digits as an int64, and any other, 38 digits among them,
// as its text, every digit kept; a column of more than 18 digits scans as
// a string; never a float64 [PROTOCOL Values].
func TestWideExactGivenAsText(t *testing.T) {
	t.Parallel()

	wide, text := thirtyEight(t)

	got, err := driverValue(context.Background(), nil, wide)
	if err != nil || got != text {
		t.Errorf("38 digits given as %#v, %v", got, err)
	}

	if scanType(ExactType{Precision: 38}) != reflect.TypeFor[string]() ||
		scanType(ExactType{Precision: 18}) != reflect.TypeFor[int64]() {
		t.Error("the scan types of exact columns")
	}
}
