package turborel

import (
	"bytes"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

// valueBytes is the bytes v writes.
func valueBytes(v Value) []byte {
	var w writer

	v.encodeValue(&w)

	return w.buf
}

// readValue reads one value from body, failing the test if it fails.
func readValue(t *testing.T, body []byte) Value {
	t.Helper()

	r := newReader(body, "value")

	v := r.value()
	if err := r.finish(); err != nil {
		t.Fatalf("reading % X: %v", body, err)
	}

	return v
}

// TestValuesRoundTrip writes and reads back a value of every tag.
//
// Verifies: [PROTOCOL Values] tags 0 to 27 each write their body as the
// table lists it and read back the same: null, strings, exact and
// approximate numbers, a plug-in's value kept as it came, binary strings,
// dates, times and timestamps, intervals, truth values, DECFLOAT, nested
// arrays, rows, whole large objects and locators, multisets, structured
// values, references, JSON, XML and UUIDs.
func TestValuesRoundTrip(t *testing.T) {
	t.Parallel()

	doc, err := NewJSON(JSONObject{{Key: "a", Value: JSONArray{Decimal{}, Char("x")}}})
	if err != nil {
		t.Fatal(err)
	}

	values := []Value{
		Null{},
		Char("héllo"),
		DecimalOf(-1250, 2),
		Approx(-0.25),
		PluginValue{Type: "POINT", Text: "POINT (1 2)", Bytes: []byte{1, 2}},
		Binary{0, 0xFF},
		Datetime{Moment: MomentDate, UTC: 63871977600000000},
		Datetime{Moment: MomentTime, UTC: 3600000000, Zone: -779, Precision: 3},
		Datetime{Moment: MomentTimestamp, UTC: 63871977600123456, Zone: 840, Zoned: true, Precision: 6},
		Interval{Qualifier: Qualifier{Start: Year, End: Month, Leading: 4}, units: int128Of(-14)},
		Interval{Qualifier: Qualifier{Start: Day, End: Second, Leading: 9, Fraction: 6}, units: int128{hi: 1, lo: 2}},
		Bool(true),
		Bool(false),
		DecFloat{coefficient: int128Of(15), exponent: -1},
		Array{Array{Char("a"), Null{}}, Array{}},
		Row{DecimalOf(1, 0), Char("Ada")},
		LOB{Kind: CharacterLOB, Octets: []byte("text")},
		LOB{Kind: BinaryLOB, Octets: []byte{7, 7}},
		Locator{Kind: BinaryLOB, Locator: 9, Octets: 1 << 33, Characters: 0},
		Multiset{Char("b"), Char("a")},
		Structured{Type: UDTName{Schema: "S", Name: "ADDRESS"}, Attributes: []Value{Char("Main St"), Row{Null{}}}},
		Reference{1, 2, 3},
		doc,
		XML("<order no=\"18\"><item>jam</item></order>"),
		UUID{0x91, 0x91, 0x08, 0xf7, 0x52, 0xd1, 0x43, 0x20, 0x9b, 0xac, 0xf8, 0x47, 0xdb, 0x41, 0x48, 0xa8},
	}
	for _, want := range values {
		if got := readValue(t, valueBytes(want)); !reflect.DeepEqual(got, want) {
			t.Errorf("read back %#v, want %#v", got, want)
		}
	}
}

// TestValuesBytes checks values' bytes against the spec.
//
// Verifies: [PROTOCOL Values] a timestamp is tag 11, zoned u8, precision
// u8, the displacement's i16 bits in the low half of a u32 and the
// instant `i64 as u64`; a date and a time are written as a timestamp,
// under tags 8 and 9; a UUID is tag 27 and bytes of 16 octets.
func TestValuesBytes(t *testing.T) {
	t.Parallel()

	stamp := valueBytes(Datetime{Moment: MomentTimestamp, Zoned: true, Precision: 6, Zone: -60, UTC: -1})
	want := []byte{11, 1, 6, 0xC4, 0xFF, 0, 0, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

	if !bytes.Equal(stamp, want) {
		t.Errorf("timestamp wrote % X, want % X", stamp, want)
	}

	if got := valueBytes(Datetime{Moment: MomentDate})[0]; got != tagDate {
		t.Errorf("a date's tag is %d", got)
	}

	if got := valueBytes(Datetime{Moment: MomentTime})[0]; got != tagTime {
		t.Errorf("a time's tag is %d", got)
	}

	id := valueBytes(UUID{15: 1})
	if !bytes.Equal(id[:5], []byte{27, 16, 0, 0, 0}) || len(id) != 21 || id[20] != 1 {
		t.Errorf("UUID wrote % X", id)
	}
}

// TestValuesApproximateBits keeps NaN and -0.0 as they are.
//
// Verifies: [PROTOCOL Values] an approximate number is its double's bits,
// so NaN, -0.0 and the infinities come back bit for bit.
func TestValuesApproximateBits(t *testing.T) {
	t.Parallel()

	negativeZero, infinity := math.Float64bits(math.Copysign(0, -1)), math.Float64bits(math.Inf(-1))
	for _, bits := range []uint64{negativeZero, 0x7FF8000000000001, infinity} {
		v, ok := readValue(t, valueBytes(Approx(math.Float64frombits(bits)))).(Approx)
		if !ok || math.Float64bits(float64(v)) != bits {
			t.Errorf("%016X came back as %016X", bits, math.Float64bits(float64(v)))
		}
	}
}

// TestValuesVaryingAsCharacter reads a CHARACTER VARYING value under
// tag 1.
//
// Verifies: [PROTOCOL Values] a value's tag says how it is written, not
// its column's type: no value has tag 5, CHARACTER VARYING's type tag,
// and a CHARACTER VARYING value comes with tag 1.
func TestValuesVaryingAsCharacter(t *testing.T) {
	t.Parallel()

	if v := readValue(t, []byte{1, 1, 0, 0, 0, 'x'}); v != Char("x") {
		t.Errorf("read %#v", v)
	}

	r := newReader([]byte{5, 1, 0, 0, 0, 'x'}, "value")
	r.value()
	assertBroken(t, r.finish(), "no value has this tag")
}

// TestValuesRefused refuses values the reference client refuses.
//
// Verifies: [PROTOCOL Values] a DECFLOAT not in its one form, a UUID not
// of 16 octets, a character large object not UTF-8, a large object of a
// kind not 0 or 1, a displacement past 16 bits, a multiset past 65535
// elements, a damaged JSON value, and a truth value not 0 or 1 each fail
// the connection.
func TestValuesRefused(t *testing.T) {
	t.Parallel()

	bigMultiset := append([]byte{22, 0, 0, 1, 0}, make([]byte, maxCardinality+1)...)
	cases := map[string][]byte{
		"DECFLOAT trailing zero": append(append([]byte{15, 10}, make([]byte, 15)...), 0, 0, 0, 0),
		"UUID of 15":             append([]byte{27, 15, 0, 0, 0}, make([]byte, 15)...),
		"CLOB not UTF-8":         {20, 0, 1, 0, 0, 0, 0xFF},
		"LOB kind 2":             {20, 2, 0, 0, 0, 0},
		"locator kind 2":         append([]byte{21, 2}, make([]byte, 20)...),
		"displacement":           append([]byte{11, 0, 0, 0, 0, 1, 0}, make([]byte, 8)...),
		"multiset":               bigMultiset,
		"JSON damaged":           {25, 2, 0, 0, 0, 1, 99},
		"truth 2":                {14, 2},
		"interval field 9":       append([]byte{13, 9, 0, 0, 0}, make([]byte, 28)...),
	}
	for name, body := range cases {
		r := newReader(body, "value")
		r.value()

		if r.finish() == nil {
			t.Errorf("%s: read without failing", name)
		}
	}
}

// TestValuesZonedLenient reads a zoned byte as the reference client
// does.
//
// Verifies: [PROTOCOL Values] a datetime's zoned byte is 1 for a type
// WITH TIME ZONE; as turborel-client reads it, any other byte is not
// zoned, and without a zone the displacement is the one it is shown in.
func TestValuesZonedLenient(t *testing.T) {
	t.Parallel()

	body := []byte{11, 7, 0, 0x3C, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}

	d, ok := readValue(t, body).(Datetime)
	if !ok || d.Zoned || d.Zone != 60 {
		t.Errorf("read %#v", d)
	}
}

// TestDatetimeTime gives a datetime's instant in its displacement.
//
// Verifies: [PROTOCOL Values] a timestamp's instant is microseconds in
// UTC, from 0001-01-01, shown in its displacement; a time's from
// midnight.
func TestDatetimeTime(t *testing.T) {
	t.Parallel()

	stamp := Datetime{Moment: MomentTimestamp, UTC: (unixEpochDays*86400 + 3600) * 1000000, Zone: 120}
	if got := stamp.Time(); !got.Equal(time.Unix(3600, 0)) || got.Format("15:04-07:00") != "03:00+02:00" {
		t.Errorf("Time() = %v", got)
	}

	clock := Datetime{Moment: MomentTime, UTC: 90 * 60 * 1000000}
	if got := clock.Time(); got.Format("15:04") != "01:30" {
		t.Errorf("Time() of a time = %v", got)
	}
}

// TestUUIDText writes and reads a UUID's text.
//
// Verifies: [PROTOCOL Values] a UUID's 16 octets, as turborel-client's
// text gives them, 8-4-4-4-12 hexadecimal digits; other text refused.
func TestUUIDText(t *testing.T) {
	t.Parallel()

	const text = "919108f7-52d1-4320-9bac-f847db4148a8"

	u, err := ParseUUID(text)
	if err != nil || u.String() != text || u[0] != 0x91 || u[15] != 0xa8 {
		t.Errorf("ParseUUID = %v, %v", u, err)
	}

	for _, bad := range []string{"", strings.Replace(text, "-", "+", 1), strings.Replace(text, "f", "g", 1), text + "0"} {
		_, err := ParseUUID(bad)
		assertOutOfRange(t, err)
	}
}

// TestIntervalUnits gives and takes an interval's units.
//
// Verifies: [PROTOCOL Values] an interval's units are an i128, months or
// microseconds; units outside it are refused.
func TestIntervalUnits(t *testing.T) {
	t.Parallel()

	q := Qualifier{Start: Day, End: Second, Leading: 9, Fraction: 6}

	i, err := NewInterval(q, bigOf(t, "-170141183460469231731687303715884105728"))
	if err != nil || i.Units().String() != "-170141183460469231731687303715884105728" {
		t.Errorf("NewInterval = %v, %v", i.Units(), err)
	}

	_, err = NewInterval(q, bigOf(t, "170141183460469231731687303715884105728"))
	assertOutOfRange(t, err)
}
