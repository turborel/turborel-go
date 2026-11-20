package turborel

// Values read back [PROTOCOL Values], each checked as the reference
// client checks it: a character large object UTF-8, a DECFLOAT in its one
// form, a UUID of 16 octets, a multiset within its cardinality, a JSON
// value a whole document of D89's form.

import (
	"math"
	"strconv"
	"unicode/utf8"
)

// value reads a value.
func (r *reader) value() Value {
	tag := r.u8()
	if r.err != nil {
		return nil
	}

	if v, ok := r.scalarValue(tag); ok {
		return v
	}

	if v, ok := r.constructedValue(tag); ok {
		return v
	}

	if tag == tagCLOB || tag == tagBLOB {
		r.failValue(corruptValue("a value of tag " + strconv.Itoa(int(tag)) +
			", a large object's stored form, is never sent"))

		return nil
	}

	r.fail("no value has this tag")

	return nil
}

// scalarValue reads the value of tag if it is one of tags 0 to 15: tags
// 5, 7, 10 and 12, of no value on the page, read as tags 1, 6, 9 and 11,
// as turborel-protocol's reader takes them.
func (r *reader) scalarValue(tag byte) (Value, bool) {
	switch tag {
	case tagNull:
		return Null{}, true
	case tagCharacter, tagVarying:
		return Char(r.str()), true
	case tagExact:
		return Decimal{units: r.i128(), scale: r.u32()}, true
	case tagApproximate:
		return Approx(math.Float64frombits(r.u64())), true
	case tagPlugin:
		return PluginValue{Type: r.str(), Text: r.str(), Bytes: r.bytes()}, true
	case tagBinary, tagBinaryVarying:
		return Binary(r.bytes()), true
	case tagDate, tagTime, tagTimeZoned, tagTimestamp, tagTimestampZoned:
		return r.datetime(tag), true
	case tagInterval:
		return Interval{Qualifier: r.qualifier(), units: r.i128()}, true
	case tagBoolean:
		return r.truth(), true
	case tagDecFloat:
		return r.decFloat(), true
	default:
		return nil, false
	}
}

// constructedValue reads the value of tag if it is one of tags 16 to
// 27.
func (r *reader) constructedValue(tag byte) (Value, bool) {
	var v Value

	switch tag {
	case tagArray:
		r.nest(func() { v = Array(r.values()) })
	case tagRow:
		r.nest(func() { v = Row(r.values()) })
	case tagMultiset:
		r.nest(func() { v = r.multiset() })
	case tagStructured:
		r.nest(func() { v = Structured{Type: UDTName{Schema: r.str(), Name: r.str()}, Attributes: r.values()} })
	case tagLOBWhole:
		v = r.lob()
	case tagLOBLocator:
		v = Locator{Kind: r.lobKind(), Locator: r.u32(), Octets: r.u64(), Characters: r.u64()}
	case tagReference:
		v = Reference(r.bytes())
	case tagJSON:
		v = r.json()
	case tagXML:
		v = XML(r.str())
	case tagUUID:
		v = r.uuid()
	default:
		return nil, false
	}

	return v, true
}

// values reads a list of values. Nothing is reserved for its count; an
// empty list is empty, not nil, so an empty array reads back as one.
func (r *reader) values() []Value {
	n := r.count()

	list := []Value{}
	for i := uint32(0); r.more(i, n); i++ {
		list = append(list, r.value())
	}

	return list
}

// truth reads a truth value's byte, 0 or 1; another is a value the
// driver cannot take.
func (r *reader) truth() Value {
	b := r.u8()
	if b != flagFalse && b != flagTrue && r.err == nil {
		r.failValue(corruptValue(strconv.Itoa(int(b)) + " is not a truth value"))
	}

	return Bool(b == flagTrue)
}

// datetime reads a date, a time or a timestamp: zoned, precision, the
// displacement's i16 in the low half of a u32, and the instant. As the
// reference client reads it, a zoned byte other than 1 is not zoned.
func (r *reader) datetime(tag byte) Datetime {
	d := Datetime{Moment: MomentTimestamp, Zoned: r.u8() == flagTrue, Precision: r.u8()}

	switch tag {
	case tagDate:
		d.Moment = MomentDate
	case tagTime, tagTimeZoned:
		d.Moment = MomentTime
	}

	zone := r.u32()
	if zone > math.MaxUint16 {
		r.fail("a time zone displacement is out of range")
	}

	d.Zone = signed16(uint16(zone)) //nolint:gosec // G115: checked above to fit 16 bits.
	d.UTC = signed64(r.u64())

	return d
}

// decFloat reads a DECFLOAT, which must be in its one form.
func (r *reader) decFloat() DecFloat {
	d := DecFloat{coefficient: r.i128(), exponent: signed32(r.u32())}
	if r.err == nil && d.check() != nil {
		r.fail("a DECFLOAT value is malformed")
	}

	return d
}

// multiset reads a multiset, of at most 65535 elements.
func (r *reader) multiset() Multiset {
	v := Multiset(r.values())
	if len(v) > maxCardinality {
		r.fail("a multiset has more elements than its maximum cardinality")
	}

	return v
}

// lobKind reads a large object's kind, 0 character or 1 binary.
func (r *reader) lobKind() LOBKind {
	k := LOBKind(r.u8())
	if k != CharacterLOB && k != BinaryLOB {
		r.fail("not a kind of large object")
	}

	return k
}

// lob reads a large object held whole; a character one must be UTF-8.
func (r *reader) lob() LOB {
	v := LOB{Kind: r.lobKind(), Octets: r.bytes()}
	if r.err == nil && v.Kind == CharacterLOB && !utf8.Valid(v.Octets) {
		r.fail("not UTF-8")
	}

	return v
}

// uuid reads a UUID, bytes of 16 octets.
func (r *reader) uuid() UUID {
	var u UUID

	b := r.bytes()
	if r.err == nil && len(b) != uuidOctets {
		r.fail("a UUID is 16 octets")
	}

	copy(u[:], b)

	return u
}

// json reads a JSON value, a whole document of D89's form.
func (r *reader) json() JSON {
	b := r.bytes()
	if r.err != nil {
		return JSON{}
	}

	if err := validateJSON(b); err != nil {
		failure := jsonFailure(err)
		if failure.SQLState == sqlstateInvalidJSON {
			failure = corruptValue(err.Error())
		}

		r.failValue(failure)
	}

	return JSON{doc: b}
}
