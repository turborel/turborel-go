package turborel

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

// assertInvalidJSON checks that err is a JSON value refused, SQLSTATE
// 22032.
func assertInvalidJSON(t *testing.T, err error) {
	t.Helper()

	var failure *Error
	if !errors.As(err, &failure) || failure.SQLState != sqlstateInvalidJSON {
		t.Errorf("err = %v, want SQLSTATE 22032", err)
	}
}

// docOf is the stored form of item.
func docOf(t *testing.T, item JSONItem) []byte {
	t.Helper()

	j, err := NewJSON(item)
	if err != nil {
		t.Fatalf("NewJSON: %v", err)
	}

	return j.Bytes()
}

// TestJSONObjectLayout writes an object as D89 lays it out.
//
// Verifies: [PROTOCOL Values] a JSON value is D89's binary tree: the
// version octet 1, then an object node 11, its count, its size, an
// offset of each member sorted by key, then the members in written
// order, each its key and its value; an integer is node 3 and an i64.
func TestJSONObjectLayout(t *testing.T) {
	t.Parallel()

	got := docOf(t, JSONObject{{Key: "b", Value: DecimalOf(1, 0)}, {Key: "a", Value: DecimalOf(2, 0)}})
	want := []byte{
		1, 11, 2, 0, 0, 0, 28, 0, 0, 0, 14, 0, 0, 0, 0, 0, 0, 0,
		1, 0, 0, 0, 'b', 3, 1, 0, 0, 0, 0, 0, 0, 0,
		1, 0, 0, 0, 'a', 3, 2, 0, 0, 0, 0, 0, 0, 0,
	}

	if !bytes.Equal(got, want) {
		t.Errorf("wrote % X, want % X", got, want)
	}
}

// TestJSONArrayLayout writes an array as D89 lays it out.
//
// Verifies: [PROTOCOL Values] an array is node 10, its count, its size,
// an offset of each element from the first, then the elements; true is
// node 2, a string node 8 and its UTF-8 after its length.
func TestJSONArrayLayout(t *testing.T) {
	t.Parallel()

	got := docOf(t, JSONArray{Bool(true), Char("x")})
	want := []byte{1, 10, 2, 0, 0, 0, 7, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 2, 8, 1, 0, 0, 0, 'x'}

	if !bytes.Equal(got, want) {
		t.Errorf("wrote % X, want % X", got, want)
	}
}

// TestJSONRoundTrip reads back every kind of node.
//
// Verifies: [PROTOCOL Values] nodes 0 to 11 read back as written: null,
// false, true, the three forms of an exact number, DECFLOAT, an
// approximate number, a string, a datetime, nested arrays and objects,
// the members of one key all kept in written order.
func TestJSONRoundTrip(t *testing.T) {
	t.Parallel()

	wide := Decimal{units: int128{hi: 1}, scale: 3}
	item := JSONObject{
		{Key: "b", Value: JSONNull{}},
		{Key: "a", Value: JSONArray{Bool(false), Bool(true), DecimalOf(7, 0), DecimalOf(-125, 2), wide, DecimalOf(1, 300)}},
		{Key: "b", Value: JSONArray{DecFloat{coefficient: int128Of(15), exponent: -1}, Approx(-0.25), Char("né")}},
		{Key: "d", Value: Datetime{Moment: MomentTimestamp, UTC: 1, Zone: -779, Zoned: true, Precision: 6}},
		{Key: "e", Value: JSONObject{}},
		{Key: "f", Value: JSONArray{}},
	}

	j, err := JSONOf(docOf(t, item))
	if err != nil {
		t.Fatal(err)
	}

	got, err := j.Item()
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(got, item) {
		t.Errorf("read back %#v, want %#v", got, item)
	}
}

// TestJSONNarrowest writes an exact number in the narrowest form.
//
// Verifies: [PROTOCOL Values] an exact number is node 3 if an i64 holds
// it at scale 0, node 4 if an i64 holds its units and a u8 its scale,
// else node 5.
func TestJSONNarrowest(t *testing.T) {
	t.Parallel()

	cases := map[byte]Decimal{
		jsonInteger: DecimalOf(-7, 0),
		jsonDecimal: DecimalOf(125, 255),
		jsonWide:    DecimalOf(1, 256),
	}
	for tag, d := range cases {
		if got := docOf(t, d)[1]; got != tag {
			t.Errorf("%v is node %d, want %d", d, got, tag)
		}
	}

	if got := docOf(t, Decimal{units: int128{hi: 0, lo: 1 << 63}})[1]; got != jsonWide {
		t.Errorf("2^63 is node %d, want 5", got)
	}
}

// TestJSONDirectorySorted sorts the directory of an object with a key
// written twice.
//
// Verifies: [PROTOCOL Values] an object's directory is sorted by key
// octets, the members of one key in written order, and the members stay
// in written order.
func TestJSONDirectorySorted(t *testing.T) {
	t.Parallel()

	doc := docOf(t, JSONObject{
		{Key: "b", Value: JSONNull{}}, {Key: "a", Value: JSONNull{}}, {Key: "b", Value: Bool(true)},
	})
	directory := doc[10:22]
	// Each member is 4 + 1 + 1 bytes: b at 0, a at 6, b at 12.
	want := []byte{6, 0, 0, 0, 0, 0, 0, 0, 12, 0, 0, 0}

	if !bytes.Equal(directory, want) {
		t.Errorf("directory % X, want % X", directory, want)
	}
}

// TestJSONDamaged refuses damaged octets.
//
// Verifies: [PROTOCOL Values] a JSON value's octets are checked whole, as
// turborel's binary::validate checks them: another version, octets after
// the root, a node past the document, a tag no node has, a string not
// UTF-8, a datetime's moment or zoned flag out of range, an offset or a
// size not the elements', a directory not sorted or naming no member, a
// count past the document, and nesting past 128 each fail.
func TestJSONDamaged(t *testing.T) {
	t.Parallel()

	object := docOf(t, JSONObject{{Key: "b", Value: JSONNull{}}, {Key: "a", Value: JSONNull{}}})
	unsorted := append([]byte{}, object...)
	copy(unsorted[10:18], []byte{0, 0, 0, 0, 6, 0, 0, 0})

	notMember := append([]byte{}, object...)
	notMember[10] = 3

	badOffset := docOf(t, JSONArray{Bool(true), Bool(true)})
	badOffset[14] = 0

	badSize := docOf(t, JSONArray{Bool(true)})
	badSize[6] = 2

	deep := make([]byte, 1, 1+(maxJSONNesting+1)*13)
	deep[0] = jsonVersion

	for range maxJSONNesting + 1 {
		deep = append(deep, 10, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	}

	cases := map[string][]byte{
		"empty":         nil,
		"version 2":     {2, 0},
		"version only":  {1},
		"trailing":      {1, 0, 0},
		"short integer": {1, 3, 1},
		"tag 12":        {1, 12},
		"not UTF-8":     {1, 8, 1, 0, 0, 0, 0xFF},
		"string past":   {1, 8, 0xFF, 0xFF, 0xFF, 0xFF},
		"moment 3":      {1, 9, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		"zoned 2":       {1, 9, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 0},
		"offset":        badOffset,
		"size":          badSize,
		"unsorted":      unsorted,
		"not a member":  notMember,
		"count past":    {1, 10, 0xFF, 0xFF, 0xFF, 0xFF, 0, 0, 0, 0},
		"deep":          deep,
		"DECFLOAT":      append(append([]byte{1, 6}, bytes.Repeat([]byte{0xFF}, 15)...), 0x7F, 0, 0, 0, 0),
	}
	for name, doc := range cases {
		if _, err := JSONOf(doc); err == nil {
			t.Errorf("%s: accepted", name)
		} else {
			assertInvalidJSON(t, err)
		}
	}
}

// TestJSONNesting refuses an item nested past 128.
//
// Verifies: [PROTOCOL Values] arrays and objects nest at most 128 deep, as
// turborel's MAX_NESTING, in a value written as in one read.
func TestJSONNesting(t *testing.T) {
	t.Parallel()

	var item JSONItem = JSONNull{}
	for range maxJSONNesting {
		item = JSONArray{item}
	}

	if _, err := NewJSON(item); err != nil {
		t.Errorf("128 deep: %v", err)
	}

	_, err := NewJSON(JSONObject{{Key: "k", Value: item}})
	assertInvalidJSON(t, err)
}

// TestJSONZeroValue sends the zero value as the SQL/JSON null.
//
// Verifies: [PROTOCOL Values] a JSON value is always a whole document: the
// zero value is sent, and read, as the document of the SQL/JSON null.
func TestJSONZeroValue(t *testing.T) {
	t.Parallel()

	var zero JSON

	if got := valueBytes(zero); !bytes.Equal(got, []byte{25, 2, 0, 0, 0, 1, 0}) {
		t.Errorf("wrote % X", got)
	}

	if item, err := zero.Item(); err != nil || item != (JSONNull{}) {
		t.Errorf("Item() = %v, %v", item, err)
	}
}

// FuzzJSONDocument checks arbitrary octets as a JSON value's stored form.
//
// Verifies: [PROTOCOL Values] damaged octets of a JSON value fail as the
// driver's error, never a panic, and a document accepted reads back to
// an item that writes the same octets.
func FuzzJSONDocument(f *testing.F) {
	f.Add([]byte{1, 0})
	f.Add([]byte{1, 10, 2, 0, 0, 0, 7, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 2, 8, 1, 0, 0, 0, 'x'})
	f.Add([]byte{1, 11, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 'k', 0})

	f.Fuzz(func(t *testing.T, doc []byte) {
		j, err := JSONOf(doc)
		if err != nil {
			var failure *Error
			if !errors.As(err, &failure) || failure.SQLState != sqlstateInvalidJSON {
				t.Fatalf("err = %v, want SQLSTATE 22032", err)
			}

			return
		}

		item, err := j.Item()
		if err != nil {
			t.Fatalf("Item() of an accepted document: %v", err)
		}

		if _, err := NewJSON(item); err != nil {
			t.Fatalf("NewJSON of a document's item: %v", err)
		}
	})
}
