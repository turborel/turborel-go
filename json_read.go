package turborel

// JSON documents read and checked whole, as turborel's binary::validate
// checks them: each node within the document, every string UTF-8, every
// offset an element's or a member's, an object's directory sorted, and
// no octet after the root. Damaged octets fail; they never panic.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"unicode/utf8"
)

// errJSONBeyond is a document past turborel's limits.
var errJSONBeyond = errors.New("past turborel's limits")

// errJSONDamaged is a document whose octets are not of D89's form.
var errJSONDamaged = errors.New("a JSON value's octets are damaged")

// damaged is errJSONDamaged, saying why.
func damaged(why string) error {
	return fmt.Errorf("%w: %s", errJSONDamaged, why)
}

// validateJSON checks that doc is a whole document of D89's form.
func validateJSON(doc []byte) error {
	_, err := readJSONDocument(doc)

	return err
}

// readJSONDocument reads the item the document doc holds, checking it
// whole.
func readJSONDocument(doc []byte) (JSONItem, error) {
	if len(doc) < 2 || doc[0] != jsonVersion {
		return nil, damaged("it does not begin with its format's version")
	}

	if len(doc) > maxJSONOctets {
		return nil, fmt.Errorf("%w: its stored form is longer than 256 MiB", errJSONBeyond)
	}

	j := jsonReader{doc: doc[1:]}

	item, length := j.node(0, 0)
	if j.err == nil && length != len(j.doc) {
		j.err = damaged("octets follow its root")
	}

	return item, j.err
}

// jsonReader reads the nodes of a document's body, keeping the first
// error.
type jsonReader struct {
	doc []byte
	err error
}

// fail records why the document is damaged.
func (j *jsonReader) fail(why string) {
	if j.err == nil {
		j.err = damaged(why)
	}
}

// field gives the n octets at at, or fails if the document is shorter.
// After a failure a fixed field reads as zeros, so the reader goes on
// without indexing past its document; a string's octets read as none,
// so a damaged length reserves nothing.
func (j *jsonReader) field(at, n int) []byte {
	if j.err == nil && at >= 0 && n >= 0 && at <= len(j.doc)-n {
		return j.doc[at : at+n]
	}

	j.fail("a node runs past its document")

	if n > int128Bytes || n < 0 {
		return nil
	}

	return make([]byte, n)
}

func (j *jsonReader) word(at int) int {
	return int(binary.LittleEndian.Uint32(j.field(at, u32Bytes)))
}

func (j *jsonReader) long(at int) uint64 {
	return binary.LittleEndian.Uint64(j.field(at, wordBytes))
}

// node reads the node at at, within depth arrays and objects: its item
// and the octets it takes.
func (j *jsonReader) node(at, depth int) (JSONItem, int) {
	if depth > maxJSONNesting {
		if j.err == nil {
			j.err = fmt.Errorf("%w: arrays and objects nest too deep", errJSONBeyond)
		}

		return nil, 0
	}

	tag := j.field(at, byteBytes)[0]
	body := at + byteBytes

	item, length := j.scalar(tag, body)
	if j.err != nil || item != nil {
		return item, byteBytes + length
	}

	switch tag {
	case jsonArray:
		item, length = j.array(body, depth+1)
	case jsonObject:
		item, length = j.object(body, depth+1)
	default:
		j.fail("a node's tag is no kind's")
	}

	return item, byteBytes + length
}

// scalar reads the body, starting at offset at, of a scalar node of tag;
// nil for a container's tag.
func (j *jsonReader) scalar(tag byte, at int) (JSONItem, int) {
	switch tag {
	case jsonNull:
		return JSONNull{}, 0
	case jsonFalse, jsonTrue:
		return Bool(tag == jsonTrue), 0
	case jsonInteger:
		return Decimal{units: int128Of(signed64(j.long(at)))}, wordBytes
	case jsonDecimal:
		return Decimal{units: int128Of(signed64(j.long(at))), scale: uint32(j.field(at+wordBytes, byteBytes)[0])},
			wordBytes + byteBytes
	case jsonWide:
		units, scale := int128At(j.field(at, int128Bytes)), binary.LittleEndian.Uint32(j.field(at+int128Bytes, u32Bytes))

		return Decimal{units: units, scale: scale}, int128Bytes + u32Bytes
	case jsonDecFloat:
		return j.decFloat(at), int128Bytes + u32Bytes
	case jsonApproximate:
		return Approx(math.Float64frombits(j.long(at))), wordBytes
	case jsonString:
		s, n := j.string(at)

		return Char(s), n
	case jsonDatetime:
		return j.datetime(at), jsonDatetimeBody
	default:
		return nil, 0
	}
}

// decFloat reads a DECFLOAT node's body, as turborel's node reader does:
// by DecFloat::new at 34 digits, so a longer coefficient is rounded and
// trailing zeros are taken into the exponent; one out of range fails.
func (j *jsonReader) decFloat(at int) JSONItem {
	coefficient := int128At(j.field(at, int128Bytes))
	exponent := signed32(binary.LittleEndian.Uint32(j.field(at+int128Bytes, u32Bytes)))

	d, err := NewDecFloat(coefficient.toBig(), exponent)
	if err != nil {
		j.fail("a number is out of range")
	}

	return d
}

// string reads a string's length and UTF-8 octets at at.
func (j *jsonReader) string(at int) (string, int) {
	n := j.word(at)
	octets := j.field(at+u32Bytes, n)

	if j.err == nil && !utf8.Valid(octets) {
		j.fail("a string is not UTF-8")
	}

	return string(octets), u32Bytes + n
}

// The places of a datetime node's fields in its body.
const (
	jsonUTCAt   = byteBytes
	jsonZoneAt  = jsonUTCAt + wordBytes
	jsonFlagsAt = jsonZoneAt + shortBytes
)

// datetime reads a datetime node's body.
func (j *jsonReader) datetime(at int) Datetime {
	f := j.field(at, jsonDatetimeBody)
	d := Datetime{
		Moment:    Moment(f[0]),
		UTC:       signed64(binary.LittleEndian.Uint64(f[jsonUTCAt:])),
		Zone:      signed16(binary.LittleEndian.Uint16(f[jsonZoneAt:])),
		Precision: f[jsonFlagsAt+1],
	}

	if d.Moment > MomentTimestamp {
		j.fail("a datetime's moment is no moment")
	}

	switch f[jsonFlagsAt] {
	case flagFalse:
	case flagTrue:
		d.Zoned = true
	default:
		j.fail("a datetime's zoned flag is neither 0 nor 1")
	}

	return d
}

// container reads an array's or an object's count and size at at, and
// where its elements or members begin.
func (j *jsonReader) container(at int) (int, int, int) {
	count, size := j.word(at), j.word(at+u32Bytes)
	if j.err == nil && count > (len(j.doc)-at)/u32Bytes {
		j.fail("a count is past its document")
	}

	if j.err != nil {
		return 0, 0, at
	}

	return count, size, at + jsonContainerHead + count*u32Bytes
}

// array reads an array's body at at: each element where its offset
// says, one after another, filling its size.
func (j *jsonReader) array(at, depth int) (JSONItem, int) {
	count, size, first := j.container(at)
	elements := JSONArray{}
	expected := 0

	for i := 0; i < count && j.err == nil; i++ {
		if j.word(at+jsonContainerHead+i*u32Bytes) != expected {
			j.fail("an element's offset is not where it is")

			break
		}

		element, n := j.node(first+expected, depth)
		elements = append(elements, element)
		expected += n
	}

	if j.err == nil && expected != size {
		j.fail("an array's size is not its elements'")
	}

	return elements, first - at + size
}

// object reads an object's body at at: its members in written order,
// filling its size, and its directory, listing each once, sorted.
func (j *jsonReader) object(at, depth int) (JSONItem, int) {
	count, size, first := j.container(at)
	members := JSONObject{}
	starts := map[int]string{}
	offset := 0

	for i := 0; i < count && j.err == nil; i++ {
		key, n := j.string(first + offset)
		value, length := j.node(first+offset+n, depth)
		starts[offset] = key
		members = append(members, JSONMember{Key: key, Value: value})
		offset += n + length
	}

	if j.err == nil && offset != size {
		j.fail("an object's size is not its members'")
	}

	j.directory(at, count, starts)

	return members, first - at + size
}

// directory checks an object's directory: each offset a member's start,
// sorted by key octets, and of one key by offset.
func (j *jsonReader) directory(at, count int, starts map[int]string) {
	previousKey, previousOffset := "", -1

	for i := 0; i < count && j.err == nil; i++ {
		offset := j.word(at + jsonContainerHead + i*u32Bytes)

		key, ok := starts[offset]
		ordered := previousOffset < 0 || key > previousKey || key == previousKey && offset > previousOffset

		if !ok || !ordered {
			j.fail("an object's key directory is not sorted")
		}

		previousKey, previousOffset = key, offset
	}
}
