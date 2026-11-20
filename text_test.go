package turborel

import (
	"math"
	"math/big"
	"strings"
	"testing"
	"time"
)

// decFloat is coefficient times ten to exponent.
func decFloat(t *testing.T, coefficient int64, exponent int32) DecFloat {
	t.Helper()

	d, err := NewDecFloat(big.NewInt(coefficient), exponent)
	if err != nil {
		t.Fatal(err)
	}

	return d
}

// instant is the microseconds of when from 0001-01-01 00:00:00 UTC.
func instant(when time.Time) int64 {
	return when.Sub(time.Date(1970, time.January, 1, 0, 0, 0, 0, time.UTC)).Microseconds() + unixEpochDays*microsPerDay
}

// interval is the interval of qualifier and units.
func interval(t *testing.T, q Qualifier, units int64) Interval {
	t.Helper()

	i, err := NewInterval(q, big.NewInt(units))
	if err != nil {
		t.Fatal(err)
	}

	return i
}

// jsonValue is the JSON value of item.
func jsonValue(t *testing.T, item JSONItem) JSON {
	t.Helper()

	j, err := NewJSON(item)
	if err != nil {
		t.Fatal(err)
	}

	return j
}

// TestValueText writes each value as turborel-client shows it.
//
// Verifies: [CLIENT Copying and exporting] an export writes each value as
// turborel-client's Display of it: a character string as it is; an
// exact number with its scale's digits; an approximate number as Rust's
// {:E}; a DECFLOAT plain within its exponents, else with E; a binary
// string as hexadecimal digits; a truth value as TRUE or FALSE; a
// datetime as its literal's string in its displacement; an interval as
// its literal's string; a large object held whole as its text, one by
// its locator as what it is; a JSON value as its JSON text; XML, a UUID
// and a plug-in's value as their text; a composite value as its literal.
func TestValueText(t *testing.T) {
	t.Parallel()

	daySecond := Qualifier{Start: Day, End: Second, Leading: 2, Fraction: 6}
	cases := []struct {
		v    Value
		want string
	}{
		{Null{}, "NULL"},
		{Char("a b"), "a b"},
		{DecimalOf(150, 2), "1.50"},
		{Approx(1.5), "1.5E0"},
		{Approx(100), "1E2"},
		{Approx(1234.5), "1.2345E3"},
		{Approx(1e-7), "1E-7"},
		{Approx(0), "0E0"},
		{Approx(math.Copysign(0, -1)), "-0E0"},
		{Approx(math.NaN()), "NaN"},
		{Approx(math.Inf(1)), "inf"},
		{Approx(math.Inf(-1)), "-inf"},
		{decFloat(t, 15, -6), "0.000015"},
		{decFloat(t, 15, 40), "1.5E41"},
		{decFloat(t, -15, -8), "-1.5E-7"},
		{decFloat(t, 0, 0), "0"},
		{decFloat(t, 1, 33), "1000000000000000000000000000000000"},
		{decFloat(t, 1, 34), "1E34"},
		{Binary{0x0a, 0xff}, "0AFF"},
		{Bool(true), "TRUE"},
		{Bool(false), "FALSE"},
		{XML("<a>b</a>"), "<a>b</a>"},
		{UUID{0x91, 0x91, 15: 0xa8}, "91910000-0000-0000-0000-0000000000a8"},
		{PluginValue{Type: "POINT", Text: "(1, 2)"}, "(1, 2)"},
		{LOB{Kind: CharacterLOB, Octets: []byte("hé")}, "hé"},
		{LOB{Kind: BinaryLOB, Octets: []byte{1, 0xab}}, "01AB"},
		{Locator{Kind: CharacterLOB, Locator: 1, Octets: 9, Characters: 5}, "<CHARACTER LARGE OBJECT of 5 characters>"},
		{Locator{Kind: BinaryLOB, Locator: 1, Octets: 9}, "<BINARY LARGE OBJECT of 9 octets>"},
		{interval(t, daySecond, -(((24+2)*60+3)*60+4)*microsPerSecond-500000), "-1 02:03:04.500000"},
		{interval(t, Qualifier{Start: Year, End: Month, Leading: 2}, 43), "3-07"},
		{interval(t, Qualifier{Start: Second, End: Second, Leading: 2, Fraction: 3}, 5250000), "5.250"},
		{interval(t, Qualifier{Start: Hour, End: Minute, Leading: 3}, 125*microsPerMinute), "2:05"},
		{Array{DecimalOf(1, 0), Null{}, DecimalOf(3, 0)}, "ARRAY[1, NULL, 3]"},
		{Row{DecimalOf(1, 0), Char("a'b")}, "ROW(1, 'a''b')"},
		{Multiset{Bool(true)}, "MULTISET[TRUE]"},
		{Structured{Type: UDTName{Schema: "S", Name: "T"}, Attributes: []Value{Char("x")}}, "S.T('x')"},
		{Reference{1, 2}, "REF(0102)"},
	}
	for _, c := range cases {
		if got := valueText(c.v); got != c.want {
			t.Errorf("%#v gave %q, want %q", c.v, got, c.want)
		}
	}
}

// TestDatetimeText writes a datetime in its displacement.
//
// Verifies: [CLIENT Copying and exporting] a datetime's text is its
// literal's string: YYYY-MM-DD, HH:MM:SS with as many digits of the
// fraction as its precision, and its displacement ±HH:MM if zoned, its
// fields as shown in its displacement; a time of day within a day.
func TestDatetimeText(t *testing.T) {
	t.Parallel()

	leap := instant(time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC))
	stamp := instant(time.Date(2024, time.January, 2, 3, 4, 5, 500000000, time.UTC))
	cases := []struct {
		d    Datetime
		want string
	}{
		{Datetime{Moment: MomentDate, UTC: leap}, "2024-02-29"},
		{Datetime{Moment: MomentTime, UTC: ((13*60+4)*60+5)*microsPerSecond + 123456, Precision: 6}, "13:04:05.123456"},
		{Datetime{Moment: MomentTimestamp, UTC: stamp, Precision: 1}, "2024-01-02 03:04:05.5"},
		{Datetime{Moment: MomentTimestamp, UTC: stamp, Zoned: true, Precision: 6}, "2024-01-02 03:04:05.500000+00:00"},
		{Datetime{Moment: MomentTimestamp, UTC: stamp, Zone: -330, Zoned: true}, "2024-01-01 21:34:05-05:30"},
		{Datetime{Moment: MomentTime, UTC: (23*60 + 30) * microsPerMinute, Zone: 60, Zoned: true}, "00:30:00+01:00"},
		{Datetime{Moment: MomentTime, UTC: 30 * microsPerMinute, Zone: -60}, "23:30:00"},
	}
	for _, c := range cases {
		if got := valueText(c.d); got != c.want {
			t.Errorf("%+v gave %q, want %q", c.d, got, c.want)
		}
	}
}

// TestValueLiterals writes each value of a composite value as a literal.
//
// Verifies: [CLIENT Copying and exporting] within a composite value each
// part is its literal: a string quoted, a quote twice; DECFLOAT as a
// CAST; a binary string X'..'; a datetime and an interval with their
// key words and qualifier; a large object held whole as a string's or a
// binary string's literal; JSON, XML and a UUID as turborel writes them.
func TestValueLiterals(t *testing.T) {
	t.Parallel()

	leap := instant(time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC))
	cases := []struct {
		v    Value
		want string
	}{
		{decFloat(t, 15, -1), "CAST('1.5' AS DECFLOAT)"},
		{Binary{0xff}, "X'FF'"},
		{Datetime{Moment: MomentDate, UTC: leap}, "DATE '2024-02-29'"},
		{Datetime{Moment: MomentTime}, "TIME '00:00:00'"},
		{Datetime{Moment: MomentTimestamp, UTC: leap}, "TIMESTAMP '2024-02-29 00:00:00'"},
		{interval(t, Qualifier{Start: Day, End: Day, Leading: 2}, -microsPerDay), "INTERVAL -'1' DAY(2)"},
		{
			interval(t, Qualifier{Start: Day, End: Second, Leading: 2, Fraction: 0}, microsPerDay),
			"INTERVAL '1 00:00:00' DAY(2) TO SECOND(0)",
		},
		{LOB{Kind: CharacterLOB, Octets: []byte("it's")}, "'it''s'"},
		{LOB{Kind: BinaryLOB, Octets: []byte{2}}, "X'02'"},
		{jsonValue(t, JSONArray{Char("a'b")}), `JSON('["a''b"]')`},
		{XML("<a/>"), "XMLPARSE(CONTENT '<a/>' PRESERVE WHITESPACE)"},
		{UUID{}, "UUID '00000000-0000-0000-0000-000000000000'"},
		{Null{}, "NULL"},
		{Bool(true), "TRUE"},
	}
	for _, c := range cases {
		if got := valueText(Array{c.v}); got != "ARRAY["+c.want+"]" {
			t.Errorf("%#v gave %q, want %q", c.v, got, c.want)
		}
	}
}

// TestJSONText writes a JSON value's JSON text.
//
// Verifies: [CLIENT Copying and exporting] a JSON value's text has no
// insignificant white space; members in their order; a quotation mark,
// a reverse solidus and control characters escaped, the rest as they
// are; an exact number as it is, an approximate one as CAST writes it, a
// DECFLOAT plain; of a datetime or an approximate number not finite, of
// which no JSON text is, a note saying so.
func TestJSONText(t *testing.T) {
	t.Parallel()

	cases := []struct {
		item JSONItem
		want string
	}{
		{
			JSONObject{
				{Key: "a", Value: JSONArray{DecimalOf(1, 0), DecimalOf(25, 1), Char(`x"y`)}}, {Key: "b", Value: JSONNull{}},
			},
			`{"a":[1,2.5,"x\"y"],"b":null}`,
		},
		{Char("\\/\b\f\n\r\t\x01é"), `"\\/\b\f\n\r\t\u0001é"`},
		{JSONArray{Approx(100), Approx(2.5), Approx(0), Bool(true), Bool(false)}, "[1.0E2,2.5E0,0E0,true,false]"},
		{decFloat(t, 15, 40), "15" + strings.Repeat("0", 40)},
		{JSONArray{Datetime{Moment: MomentDate}}, "<[ISO/IEC 9075-2:2023 §9.43 GR2a ii] no JSON text parses to a datetime>"},
		{JSONObject{{Key: "n", Value: Approx(math.Inf(1))}},
			"<[ISO/IEC 9075-2:2023 §9.43 GR2a ii] no JSON text parses to an approximate number that is not finite>"},
	}
	for _, c := range cases {
		if got := valueText(jsonValue(t, c.item)); got != c.want {
			t.Errorf("%#v gave %q, want %q", c.item, got, c.want)
		}
	}

	damagedText := "<a JSON value's octets are damaged: it does not begin with its format's version>"
	if got := valueText(JSON{doc: []byte{0}}); got != damagedText {
		t.Errorf("a damaged value gave %q", got)
	}
}
