package turborel

// JSON values [PROTOCOL Values]. On the wire a JSON value is its stored
// form, D89's binary tree (turborel's RULES.md D89, `src/json/binary.rs`):
// a version octet, 1, then its root node, a tag octet and its body,
// integers little-endian.
//
//	0 null | 1 false | 2 true
//	3 integer: i64
//	4 decimal: units i64, scale u8
//	5 wide decimal: units i128, scale u32
//	6 decimal floating point: coefficient i128, exponent i32
//	7 approximate: f64 bits
//	8 string: octets u32, UTF-8
//	9 datetime: moment u8, utc i64, zone i16, zoned u8, precision u8
//	10 array: count u32, size u32, an offset u32 of each element, the elements
//	11 object: count u32, size u32, an offset u32 of each member in key
//	   order, the members in written order, each key octets u32, key, node
//
// Offsets count from the first element or member. An object's directory
// is sorted by key octets, the members of one key in written order.

import (
	"encoding/binary"
	"errors"
	"math"
	"sort"
	"strings"
)

// The layout of D89's binary tree.
const (
	// jsonVersion is a document's first octet.
	jsonVersion byte = 1
	// jsonBeyond begins the message of a JSON value past turborel's limits,
	// as turborel's.
	jsonBeyond = "the SQL/JSON item is beyond turborel's limits: "
	// maxJSONNesting is how deep arrays and objects nest, turborel's
	// MAX_NESTING.
	maxJSONNesting = 128
	// maxJSONOctets is the most octets a document holds, turborel's
	// MAX_DOCUMENT_OCTETS, 256 MiB.
	maxJSONOctets = 1 << 28
	// jsonContainerHead is an array's or an object's count and size.
	jsonContainerHead = 2 * u32Bytes
	// jsonDatetimeBody is a datetime node's body: moment, instant, zone,
	// zoned and precision.
	jsonDatetimeBody = byteBytes + wordBytes + shortBytes + byteBytes + byteBytes
)

// The tags of the nodes.
const (
	jsonNull        byte = 0
	jsonFalse       byte = 1
	jsonTrue        byte = 2
	jsonInteger     byte = 3
	jsonDecimal     byte = 4
	jsonWide        byte = 5
	jsonDecFloat    byte = 6
	jsonApproximate byte = 7
	jsonString      byte = 8
	jsonDatetime    byte = 9
	jsonArray       byte = 10
	jsonObject      byte = 11
)

// JSON is a value of the JSON type: its stored form, a document of D89's
// binary tree, whole and checked.
type JSON struct {
	doc []byte
}

// JSONItem is an SQL/JSON item: [JSONNull], [Bool], [Decimal],
// [DecFloat], [Approx], [Char], [Datetime], [JSONArray] or
// [JSONObject].
type JSONItem interface {
	encodeJSON(w *writer, depth int) error
}

// JSONNull is the SQL/JSON null.
type JSONNull struct{}

// JSONArray is an SQL/JSON array's elements.
type JSONArray []JSONItem

// JSONMember is a member of an SQL/JSON object.
type JSONMember struct {
	Key   string
	Value JSONItem
}

// JSONObject is an SQL/JSON object's members, in the order written; a
// key may come more than once.
type JSONObject []JSONMember

// NewJSON is the JSON value of item. It fails, as an [*Error] of SQLCODE
// -900 and SQLSTATE 0A000, past turborel's limits: if arrays and objects
// nest deeper than 128, or the stored form would be longer than 256 MiB.
func NewJSON(item JSONItem) (JSON, error) {
	w := &writer{buf: []byte{jsonVersion}}
	if err := item.encodeJSON(w, 0); err != nil {
		return JSON{}, err
	}

	if len(w.buf) > maxJSONOctets {
		return JSON{}, beyond(jsonBeyond + "its stored form is longer than 256 MiB")
	}

	return JSON{doc: w.buf}, nil
}

// JSONOf is the JSON value whose stored form is doc. It fails, as an
// [*Error] of SQLSTATE 22032, if doc is not a whole document of D89's
// form.
func JSONOf(doc []byte) (JSON, error) {
	if err := validateJSON(doc); err != nil {
		return JSON{}, jsonFailure(err)
	}

	return JSON{doc: append([]byte{}, doc...)}, nil
}

// Bytes gives the value's stored form.
func (j JSON) Bytes() []byte {
	return append([]byte{}, j.doc...)
}

// Item gives the SQL/JSON item the value holds; of the zero value, the
// SQL/JSON null.
func (j JSON) Item() (JSONItem, error) {
	if len(j.doc) == 0 {
		return JSONNull{}, nil
	}

	item, err := readJSONDocument(j.doc)
	if err != nil {
		return nil, jsonFailure(err)
	}

	return item, nil
}

func (j JSON) encodeValue(w *writer) {
	w.u8(tagJSON)

	if len(j.doc) == 0 {
		w.bytes([]byte{jsonVersion, jsonNull})

		return
	}

	w.bytes(j.doc)
}

// deeper fails past maxJSONNesting.
func deeper(depth int) error {
	if depth > maxJSONNesting {
		return beyond(jsonBeyond + "arrays and objects nest too deep")
	}

	return nil
}

func (JSONNull) encodeJSON(w *writer, _ int) error {
	w.u8(jsonNull)

	return nil
}

func (v Bool) encodeJSON(w *writer, _ int) error {
	w.u8(zonedTag(bool(v), jsonFalse, jsonTrue))

	return nil
}

// encodeJSON writes the narrowest of the three forms that holds d.
func (d Decimal) encodeJSON(w *writer, _ int) error {
	narrow := d.units.fitsInt64()

	switch {
	case narrow && d.scale == 0:
		w.u8(jsonInteger)
		w.u64(d.units.lo)
	case narrow && d.scale <= 0xFF:
		w.u8(jsonDecimal)
		w.u64(d.units.lo)
		w.u8(byte(d.scale))
	default:
		w.u8(jsonWide)
		w.i128(d.units)
		w.u32(d.scale)
	}

	return nil
}

func (d DecFloat) encodeJSON(w *writer, _ int) error {
	w.u8(jsonDecFloat)
	w.i128(d.coefficient)
	w.u32(unsigned32(d.exponent))

	return nil
}

func (v Approx) encodeJSON(w *writer, _ int) error {
	w.u8(jsonApproximate)
	w.u64(math.Float64bits(float64(v)))

	return nil
}

func (v Char) encodeJSON(w *writer, _ int) error {
	w.u8(jsonString)
	w.str(string(v))

	return nil
}

func (d Datetime) encodeJSON(w *writer, _ int) error {
	w.u8(jsonDatetime)
	w.u8(byte(d.Moment))
	w.u64(unsigned64(d.UTC))
	w.buf = binary.LittleEndian.AppendUint16(w.buf, unsigned16(d.Zone))
	w.flag(d.Zoned)
	w.u8(d.Precision)

	return nil
}

func (a JSONArray) encodeJSON(w *writer, depth int) error {
	if err := deeper(depth + 1); err != nil {
		return err
	}

	head, offsets := openContainer(w, jsonArray, len(a))
	body := len(w.buf)

	for i, element := range a {
		patchWord(w, offsets+i*u32Bytes, len(w.buf)-body)

		if err := element.encodeJSON(w, depth+1); err != nil {
			return err
		}
	}

	patchWord(w, head+u32Bytes, len(w.buf)-body)

	return nil
}

func (o JSONObject) encodeJSON(w *writer, depth int) error {
	if err := deeper(depth + 1); err != nil {
		return err
	}

	head, offsets := openContainer(w, jsonObject, len(o))
	body := len(w.buf)
	directory := make([]int, len(o))

	for i, member := range o {
		directory[i] = len(w.buf) - body
		w.str(member.Key)

		if err := member.Value.encodeJSON(w, depth+1); err != nil {
			return err
		}
	}

	order := make([]int, len(o))
	for i := range order {
		order[i] = i
	}

	// Go compares strings octet by octet, as the directory is sorted; a
	// stable sort keeps the members of one key in written order.
	sort.SliceStable(order, func(a, b int) bool { return o[order[a]].Key < o[order[b]].Key })

	for i, member := range order {
		patchWord(w, offsets+i*u32Bytes, directory[member])
	}

	patchWord(w, head+u32Bytes, len(w.buf)-body)

	return nil
}

// openContainer writes a container's tag, its count, a size to patch and
// count offsets to patch; it gives where the count and the first offset
// are.
func openContainer(w *writer, tag byte, count int) (int, int) {
	w.u8(tag)
	head := len(w.buf)
	w.u32(count32(count))
	w.u32(0)
	offsets := len(w.buf)
	w.buf = append(w.buf, make([]byte, count*u32Bytes)...)

	return head, offsets
}

// patchWord writes n as a u32 at at, reserved before.
func patchWord(w *writer, at, n int) {
	var word writer

	word.u32(count32(n))
	copy(w.buf[at:at+u32Bytes], word.buf)
}

// jsonFailure gives the driver's failure of a document readJSONDocument
// refused: past turborel's limits, SQLSTATE 0A000; else 22032.
func jsonFailure(err error) *Error {
	if errors.Is(err, errJSONBeyond) {
		return beyond(jsonBeyond + strings.TrimPrefix(err.Error(), errJSONBeyond.Error()+": "))
	}

	return invalidJSON(err.Error())
}
