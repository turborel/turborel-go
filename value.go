package turborel

// Values [PROTOCOL Values]: a tag, then its body. Each value type of this
// file writes itself; value_decode.go reads them back.

import (
	"encoding/hex"
	"errors"
	"math"
	"math/big"
	"time"
)

// Value is a value of a row or a parameter. It is one of the value types
// of this package: [Null], [Char], [Decimal], [Approx], [PluginValue],
// [Binary], [Datetime], [Interval], [Bool], [DecFloat], [Array], [Row],
// [LOB], [Locator], [Multiset], [Structured], [Reference], [JSON], [XML]
// or [UUID].
type Value interface {
	encodeValue(w *writer)
}

// Null is the null value.
type Null struct{}

// Char is a character string, of any character string type.
type Char string

// Approx is an approximate number, an IEEE 754 double, NaN and −0.0
// kept as they are.
type Approx float64

// PluginValue is a value of a type a plug-in defines: the type's name,
// the value as text, and the type's own bytes. One received is sent back
// as it came.
type PluginValue struct {
	Type  string
	Text  string
	Bytes []byte
}

// Binary is a binary string, of either binary type.
type Binary []byte

// Moment says which fields a datetime has.
type Moment uint8

// The moments of a datetime.
const (
	// MomentDate is a date.
	MomentDate Moment = iota
	// MomentTime is a time of day.
	MomentTime
	// MomentTimestamp is a date and a time.
	MomentTimestamp
)

// Datetime is a date, a time or a timestamp, as turborel keeps it: UTC
// is microseconds in UTC, of a timestamp from 0001-01-01 00:00:00, of a
// time from midnight, of a date its midnight's from 0001-01-01; Zone is
// the displacement in minutes, the value's own if Zoned, else the one it
// is shown in; Precision is the digits of its seconds' fraction shown.
type Datetime struct {
	Moment    Moment
	UTC       int64
	Zone      int16
	Zoned     bool
	Precision uint8
}

// The calendar's constants a datetime's instant is counted by.
const (
	// unixEpochDays is the days from 0001-01-01 to 1970-01-01.
	unixEpochDays = 719162
	// microsPerDay is the microseconds of a day.
	microsPerDay = 86400000000
	// secondsPerMinute is the seconds of a minute.
	secondsPerMinute = 60
)

// Time gives the datetime's instant as a time.Time in its displacement.
// A time of day falls on 1970-01-01.
func (d Datetime) Time() time.Time {
	zone := time.FixedZone("", int(d.Zone)*secondsPerMinute)
	if d.Moment == MomentTime {
		return time.UnixMicro(d.UTC).In(zone)
	}

	return time.UnixMicro(d.UTC - unixEpochDays*microsPerDay).In(zone)
}

// Interval is an interval of its qualifier: months of a year-month
// interval, microseconds of a day-time one.
type Interval struct {
	Qualifier Qualifier
	units     int128
}

// NewInterval is the interval of qualifier and units. It fails, as an
// [*Error] of SQLSTATE 22003, if units is outside the range of a signed
// 128-bit integer.
func NewInterval(qualifier Qualifier, units *big.Int) (Interval, error) {
	bits, ok := int128OfBig(units)
	if !ok {
		return Interval{}, outOfRange(errUnitsRange.Error())
	}

	return Interval{Qualifier: qualifier, units: bits}, nil
}

// Units gives the interval's months or microseconds.
func (i Interval) Units() *big.Int {
	return i.units.toBig()
}

// Bool is a truth value; unknown is [Null].
type Bool bool

// Array is an array's elements.
type Array []Value

// Row is a row's fields' values.
type Row []Value

// LOBKind says whether a large object holds characters or octets.
type LOBKind uint8

// The kinds of large object [PROTOCOL Values].
const (
	// CharacterLOB is a CHARACTER LARGE OBJECT.
	CharacterLOB LOBKind = 0
	// BinaryLOB is a BINARY LARGE OBJECT.
	BinaryLOB LOBKind = 1
)

// LOB is a large object held whole: a character one's octets are UTF-8.
type LOB struct {
	Kind   LOBKind
	Octets []byte
}

// Locator is a large object reached by its locator, with its lengths; it
// lasts until its transaction ends.
type Locator struct {
	Kind       LOBKind
	Locator    uint32
	Octets     uint64
	Characters uint64
}

// Multiset is a multiset's elements, in no set order.
type Multiset []Value

// Structured is a value of a structured type: its most specific type,
// and its attributes.
type Structured struct {
	Type       UDTName
	Attributes []Value
}

// Reference is a reference's octets.
type Reference []byte

// XML is an XML value, its serialization as CONTENT.
type XML string

// UUID is a UUID's 16 octets.
type UUID [uuidOctets]byte

// The layout of a UUID's text: 8-4-4-4-12 hexadecimal digits.
const (
	uuidOctets = 16
	uuidText   = 36
	// hexDigitsPerOctet is the hexadecimal digits that write an octet.
	hexDigitsPerOctet = 2
)

// uuidGroups are where each group of a UUID's octets ends.
func uuidGroups() [5]int { return [5]int{4, 6, 8, 10, uuidOctets} }

// errUUIDText is text that is no UUID.
var errUUIDText = errors.New("a UUID is 32 hexadecimal digits in groups of 8, 4, 4, 4 and 12")

// String gives the UUID as text, as 919108f7-52d1-4320-9bac-f847db4148a8.
func (u UUID) String() string {
	text := make([]byte, 0, uuidText)
	from := 0

	for i, to := range uuidGroups() {
		if i > 0 {
			text = append(text, '-')
		}

		text = hex.AppendEncode(text, u[from:to])
		from = to
	}

	return string(text)
}

// ParseUUID reads a UUID's text. It fails, as an [*Error] of SQLSTATE
// 22003, if text is not one.
func ParseUUID(text string) (UUID, error) {
	var u UUID

	if len(text) != uuidText {
		return u, outOfRange(errUUIDText.Error())
	}

	from, at := 0, 0
	for i, to := range uuidGroups() {
		if i > 0 {
			if text[at] != '-' {
				return u, outOfRange(errUUIDText.Error())
			}

			at++
		}

		digits := (to - from) * hexDigitsPerOctet
		if _, err := hex.Decode(u[from:to], []byte(text[at:at+digits])); err != nil {
			return u, outOfRange(errUUIDText.Error())
		}

		from, at = to, at+digits
	}

	return u, nil
}

func (Null) encodeValue(w *writer) { w.u8(tagNull) }

func (v Char) encodeValue(w *writer) {
	w.u8(tagCharacter)
	w.str(string(v))
}

func (d Decimal) encodeValue(w *writer) {
	w.u8(tagExact)
	w.i128(d.units)
	w.u32(d.scale)
}

func (v Approx) encodeValue(w *writer) {
	w.u8(tagApproximate)
	w.u64(math.Float64bits(float64(v)))
}

func (v PluginValue) encodeValue(w *writer) {
	w.u8(tagPlugin)
	w.str(v.Type)
	w.str(v.Text)
	w.bytes(v.Bytes)
}

func (v Binary) encodeValue(w *writer) {
	w.u8(tagBinary)
	w.bytes(v)
}

// momentTag is the value tag of a date, a time or a timestamp.
func momentTag(m Moment) byte {
	switch m {
	case MomentDate:
		return tagDate
	case MomentTime:
		return tagTime
	case MomentTimestamp:
		return tagTimestamp
	default:
		return tagTimestamp
	}
}

func (d Datetime) encodeValue(w *writer) {
	w.u8(momentTag(d.Moment))
	w.flag(d.Zoned)
	w.u8(d.Precision)
	w.u32(uint32(unsigned16(d.Zone)))
	w.u64(unsigned64(d.UTC))
}

func (i Interval) encodeValue(w *writer) {
	w.u8(tagInterval)
	i.Qualifier.encode(w)
	w.i128(i.units)
}

func (v Bool) encodeValue(w *writer) {
	w.u8(tagBoolean)
	w.flag(bool(v))
}

func (d DecFloat) encodeValue(w *writer) {
	w.u8(tagDecFloat)
	w.i128(d.coefficient)
	w.u32(unsigned32(d.exponent))
}

func (v Array) encodeValue(w *writer) {
	w.u8(tagArray)
	encodeValues(w, v)
}

func (v Row) encodeValue(w *writer) {
	w.u8(tagRow)
	encodeValues(w, v)
}

func (v Multiset) encodeValue(w *writer) {
	w.u8(tagMultiset)
	encodeValues(w, v)
}

// encodeValues writes a list of values: their count, then each.
func encodeValues(w *writer, values []Value) {
	w.u32(count32(len(values)))

	for _, v := range values {
		v.encodeValue(w)
	}
}

func (v LOB) encodeValue(w *writer) {
	w.u8(tagLOBWhole)
	w.u8(byte(v.Kind))
	w.bytes(v.Octets)
}

func (v Locator) encodeValue(w *writer) {
	w.u8(tagLOBLocator)
	w.u8(byte(v.Kind))
	w.u32(v.Locator)
	w.u64(v.Octets)
	w.u64(v.Characters)
}

func (v Structured) encodeValue(w *writer) {
	w.u8(tagStructured)
	w.str(v.Type.Schema)
	w.str(v.Type.Name)
	encodeValues(w, v.Attributes)
}

func (v Reference) encodeValue(w *writer) {
	w.u8(tagReference)
	w.bytes(v)
}

func (v XML) encodeValue(w *writer) {
	w.u8(tagXML)
	w.str(string(v))
}

func (u UUID) encodeValue(w *writer) {
	w.u8(tagUUID)
	w.bytes(u[:])
}
