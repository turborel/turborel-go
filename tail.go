package turborel

// A message's tail [PROTOCOL The tail]: the fields version 1 gained after
// a message's first form, each a tag and its field, in increasing order
// of tag, to the end of the body. A tag the message does not take, or
// one not past the last, fails the connection; a message with an empty
// tail is its first form exactly.

import (
	"fmt"
	"slices"
)

// The tags of a tail [PROTOCOL The tail].
const (
	tailAttributes byte = 1
	tailMulti      byte = 2
	tailCursor     byte = 3
	tailPosition   byte = 4
	tailMore       byte = 5
	tailRows       byte = 6
	tailSeverity   byte = 7
	tailDetail     byte = 8
	tailHint       byte = 9
	tailContext    byte = 10
	tailSchema     byte = 11
	tailTable      byte = 12
	tailColumn     byte = 13
	tailDataType   byte = 14
	tailConstraint byte = 15
	tailGenerated  byte = 16
	tailConditions byte = 17
	tailTime       byte = 18
)

// The bounds of a message's attributes [PROTOCOL The tail].
const (
	maxAttributes     = 32
	maxAttributeName  = 64
	maxAttributeValue = 1024
)

// checkAttributes refuses attributes past the bounds a server takes,
// before they are sent: a server fails the connection of a message with
// more than 32, or a name not of 1 to 64 bytes, or a value of more than
// 1024 [PROTOCOL The tail], so the driver gives the same failure, SQLCODE
// -919 and SQLSTATE 08006, without sending it.
func checkAttributes(attributes []Attribute) error {
	if len(attributes) > maxAttributes {
		return broken(fmt.Sprintf("%d attributes are more than the %d taken", len(attributes), maxAttributes))
	}

	for _, a := range attributes {
		if a.Name == "" || len(a.Name) > maxAttributeName || len(a.Value) > maxAttributeValue {
			return broken(fmt.Sprintf("an attribute's name is 1 to %d bytes and its value at most %d",
				maxAttributeName, maxAttributeValue))
		}
	}

	return nil
}

// Attribute is a name and a value a connection or a request carries,
// read in SQL by QUERY_ATTRIBUTE.
type Attribute struct {
	Name  string
	Value string
}

// tail reads a message's tail, its tags among taken.
type tail struct {
	last  byte
	taken []byte
}

// next gives the next tag, false at the end of the body; a tag not past
// the last, or not among those taken, fails.
func (t *tail) next(r *reader) (byte, bool) {
	if r.err != nil || r.atEnd() {
		return 0, false
	}

	tag := r.u8()
	if tag <= t.last || !slices.Contains(t.taken, tag) {
		r.fail(fmt.Sprintf("its tail has tag %d out of place", tag))

		return 0, false
	}

	t.last = tag

	return tag, true
}

// attributes writes tag and attributes, unless there are none.
func (w *writer) attributes(tag byte, attributes []Attribute) {
	if len(attributes) == 0 {
		return
	}

	w.u8(tag)
	w.u32(count32(len(attributes)))

	for _, a := range attributes {
		w.str(a.Name)
		w.str(a.Value)
	}
}

// mark writes tag, with no field, if set.
func (w *writer) mark(tag byte, set bool) {
	if set {
		w.u8(tag)
	}
}

// number writes tag and n, unless n is 0, a field's default.
func (w *writer) number(tag byte, n uint32) {
	if n != 0 {
		w.u8(tag)
		w.u32(n)
	}
}

// diagnosticTags are the tags of a failure's or a notice's tail, 7 to
// 15.
func diagnosticTags() []byte {
	return []byte{
		tailSeverity, tailDetail, tailHint, tailContext, tailSchema,
		tailTable, tailColumn, tailDataType, tailConstraint,
	}
}

// diagnostics reads a tail of diagnostics alone: nil if it is empty.
func (r *reader) diagnostics() *Diagnostics {
	var d *Diagnostics

	t := tail{taken: diagnosticTags()}
	for tag, ok := t.next(r); ok; tag, ok = t.next(r) {
		if d == nil {
			d = &Diagnostics{}
		}

		*d.slot(tag) = r.str()
	}

	return d
}

// slot gives the field of tag, one of diagnosticTags.
func (d *Diagnostics) slot(tag byte) *string {
	slots := []*string{
		&d.Severity, &d.Detail, &d.Hint, &d.Context, &d.Schema,
		&d.Table, &d.Column, &d.DataType, &d.Constraint,
	}

	return slots[tag-tailSeverity]
}
