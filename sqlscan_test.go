package turborel

import (
	"database/sql"
	"database/sql/driver"
	"reflect"
	"testing"
	"time"
)

// scanner is a value type scanned from database/sql.
type scanner interface {
	sql.Scanner
}

// TestScanners scans each of the driver's value types.
//
// Verifies: [DESIGN] D2 each of the driver's value
// types is a sql.Scanner, taking a value of its own type, and of the Go
// type the driver gives of it; another type refused (SQLSTATE TB910).
func TestScanners(t *testing.T) {
	t.Parallel()

	at := time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		into scanner
		src  any
		want any
	}{
		{new(Decimal), int64(7), DecimalOf(7, 0)}, {new(Decimal), "-1.50", DecimalOf(-150, 2)},
		{new(Decimal), DecimalOf(1, 0), DecimalOf(1, 0)},
		{new(DecFloat), "1.5E3", decFloat(t, 15, 2)}, {new(DecFloat), "0.25", decFloat(t, 25, -2)},
		{new(Approx), 2.5, Approx(2.5)}, {new(Char), "a", Char("a")}, {new(Char), []byte("b"), Char("b")},
		{new(Binary), []byte{1}, Binary{1}}, {new(Bool), true, Bool(true)},
		{new(Datetime), at, datetimeOf(at)}, {new(LOB), "c", LOB{Kind: CharacterLOB, Octets: []byte("c")}},
		{new(LOB), []byte{2}, LOB{Kind: BinaryLOB, Octets: []byte{2}}}, {new(XML), "<a/>", XML("<a/>")},
		{new(UUID), "00000000-0000-0000-0000-000000000001", UUID{15: 1}},
		{new(Interval), Interval{}, Interval{}}, {new(Locator), Locator{Locator: 1}, Locator{Locator: 1}},
		{new(JSON), JSON{}, JSON{}}, {new(Array), Array{Null{}}, Array{Null{}}}, {new(Row), Row{Null{}}, Row{Null{}}},
		{new(Multiset), Multiset{}, Multiset{}}, {new(Structured), Structured{}, Structured{}},
		{new(Reference), Reference{1}, Reference{1}}, {new(PluginValue), PluginValue{Text: "p"}, PluginValue{Text: "p"}},
	}

	for _, c := range cases {
		if err := c.into.Scan(c.src); err != nil || !reflect.DeepEqual(reflect.ValueOf(c.into).Elem().Interface(), c.want) {
			t.Errorf("%T of %#v gave %#v, %v", c.into, c.src, c.into, err)
		}

		assertState(t, c.into.Scan(struct{}{}), sqlstateMisuse)
	}

	for _, bad := range []struct {
		into scanner
		src  any
	}{{new(Decimal), "1.2.3"}, {new(Decimal), "x"}, {new(DecFloat), "x"}, {new(DecFloat), "1Ex"}, {new(UUID), "x"}} {
		if err := bad.into.Scan(bad.src); err == nil {
			t.Errorf("%T took %q", bad.into, bad.src)
		}
	}
}

// TestValuers gives each of the driver's value types to any driver.
//
// Verifies: [DESIGN] D2 each of the driver's value
// types is a driver.Valuer, giving the Go value of Go's types it is:
// a number's text or float64, a string, bytes, a bool, a time.Time, and
// of any other its text or literal.
func TestValuers(t *testing.T) {
	t.Parallel()

	at := time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		v    driver.Valuer
		want driver.Value
	}{
		{DecimalOf(150, 2), "1.50"}, {decFloat(t, 15, -1), "1.5"}, {Approx(2), 2.0}, {Char("a"), "a"}, {Binary{1}, []byte{1}},
		{Bool(true), true}, {datetimeOf(at), datetimeOf(at).Time()}, {Interval{Qualifier: Qualifier{Leading: 1}}, "0"},
		{LOB{Kind: CharacterLOB, Octets: []byte("c")}, "c"},
		{Locator{Characters: 3}, "<CHARACTER LARGE OBJECT of 3 characters>"},
		{XML("<a/>"), "<a/>"}, {UUID{}, "00000000-0000-0000-0000-000000000000"}, {Array{Null{}}, "ARRAY[NULL]"},
		{Row{Bool(true)}, "ROW(TRUE)"}, {Multiset{}, "MULTISET[]"}, {Structured{Type: UDTName{"S", "T"}}, "S.T()"},
		{Reference{1}, "REF(01)"}, {PluginValue{Text: "p"}, "p"}, {Null{}, nil},
	}

	for _, c := range cases {
		if got, err := c.v.Value(); err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%#v gave %#v, %v", c.v, got, err)
		}
	}

	j, err := NewJSON(JSONArray{})
	if err != nil {
		t.Fatal(err)
	}

	if got, _ := j.Value(); got != "[]" {
		t.Errorf("JSON gave %#v", got)
	}
}
