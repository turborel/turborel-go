package turborel

// Data types [PROTOCOL Data types]: a tag, then what its kind writes
// after it. Columns, parameters and the types inside other types are
// written this way. A distinct type comes as its source type.

// The tags of data types and values [PROTOCOL Data types], [PROTOCOL
// Values]. A value's tag says how it is written, which is not always its
// column type's: a CHARACTER VARYING value comes with tag 1.
const (
	tagNull           byte = 0
	tagCharacter      byte = 1
	tagExact          byte = 2
	tagApproximate    byte = 3
	tagPlugin         byte = 4
	tagVarying        byte = 5
	tagBinary         byte = 6
	tagBinaryVarying  byte = 7
	tagDate           byte = 8
	tagTime           byte = 9
	tagTimeZoned      byte = 10
	tagTimestamp      byte = 11
	tagTimestampZoned byte = 12
	tagInterval       byte = 13
	tagBoolean        byte = 14
	tagDecFloat       byte = 15
	tagArray          byte = 16
	tagRow            byte = 17
	tagCLOB           byte = 18
	tagBLOB           byte = 19
	tagLOBWhole       byte = 20
	tagLOBLocator     byte = 21
	tagMultiset       byte = 22
	tagStructured     byte = 23
	tagReference      byte = 24
	tagJSON           byte = 25
	tagXML            byte = 26
	tagUUID           byte = 27
)

// DataType is a column's or a parameter's type, as the protocol
// describes it. It is one of the types of this file.
type DataType interface {
	encodeType(w *writer)
}

// CharUnits says what a character string's length counts.
type CharUnits uint8

// The units of a length [PROTOCOL Data types].
const (
	// Characters counts characters.
	Characters CharUnits = 0
	// Octets counts octets.
	Octets CharUnits = 1
)

// CharacterType is CHARACTER(Length).
type CharacterType struct {
	Length uint32
	Units  CharUnits
}

// VaryingType is CHARACTER VARYING(Length).
type VaryingType struct {
	Length uint32
	Units  CharUnits
}

// ExactType is an exact numeric type of a precision and scale.
type ExactType struct {
	Precision uint32
	Scale     uint32
}

// ApproximateType is an approximate numeric type of a precision.
type ApproximateType struct {
	Precision uint32
}

// PluginType is a type a plug-in defines, by its name and parameters. A
// program reads values of it by their text.
type PluginType struct {
	Name   string
	Params []uint32
}

// BinaryType is BINARY(Length).
type BinaryType struct {
	Length uint32
}

// VarBinaryType is BINARY VARYING(Length).
type VarBinaryType struct {
	Length uint32
}

// DateType is DATE.
type DateType struct {
	Precision uint32
}

// TimeType is TIME, WITH TIME ZONE if Zoned.
type TimeType struct {
	Precision uint32
	Zoned     bool
}

// TimestampType is TIMESTAMP, WITH TIME ZONE if Zoned.
type TimestampType struct {
	Precision uint32
	Zoned     bool
}

// IntervalField is a field of an interval's qualifier [PROTOCOL Data
// types].
type IntervalField uint32

// The fields of an interval, numbered as the protocol numbers them.
const (
	Year IntervalField = iota
	Month
	Day
	Hour
	Minute
	Second
)

// Qualifier is an interval's qualifier: its first and last fields and
// their precisions.
type Qualifier struct {
	Start    IntervalField
	End      IntervalField
	Leading  uint32
	Fraction uint32
}

// IntervalType is INTERVAL of a qualifier.
type IntervalType struct {
	Qualifier Qualifier
}

// BooleanType is BOOLEAN.
type BooleanType struct{}

// DecFloatType is DECFLOAT(Precision).
type DecFloatType struct {
	Precision uint32
}

// ArrayType is an array of at most Max elements of a type.
type ArrayType struct {
	Max     uint32
	Element DataType
}

// Field is a row type's field.
type Field struct {
	Name string
	Type DataType
}

// RowType is a row of named fields.
type RowType struct {
	Fields []Field
}

// CLOBType is CHARACTER LARGE OBJECT(Length).
type CLOBType struct {
	Length uint64
	Units  CharUnits
}

// BLOBType is BINARY LARGE OBJECT(Length).
type BLOBType struct {
	Length uint64
	Units  CharUnits
}

// MultisetType is a multiset of a type.
type MultisetType struct {
	Element DataType
}

// UDTName is a user-defined type's schema and name.
type UDTName struct {
	Schema string
	Name   string
}

// StructuredType is a structured type and its proper supertypes.
type StructuredType struct {
	Name       UDTName
	Supertypes []UDTName
}

// ReferenceType is REF of a structured type, scoped to a table if Scope
// is not nil.
type ReferenceType struct {
	Referenced StructuredType
	Scope      *UDTName
}

// JSONType is JSON.
type JSONType struct{}

// XMLPrimary is an XML type's primary modifier.
type XMLPrimary uint8

// The primary modifiers [PROTOCOL Data types].
const (
	XMLDocument XMLPrimary = 0
	XMLContent  XMLPrimary = 1
	XMLSequence XMLPrimary = 2
)

// XMLSecondary is an XML type's secondary modifier.
type XMLSecondary uint8

// The secondary modifiers [PROTOCOL Data types].
const (
	XMLUntyped   XMLSecondary = 0
	XMLAny       XMLSecondary = 1
	XMLXMLSchema XMLSecondary = 2
)

// XMLSchemaName is the XML Schema an XML type of XMLSCHEMA names: its
// schema and name, and its namespace and element if given.
type XMLSchemaName struct {
	Schema    string
	Name      string
	Namespace *string
	Element   *string
}

// XMLType is XML of its modifiers; Schema is set of XMLSCHEMA alone.
type XMLType struct {
	Primary   XMLPrimary
	Secondary XMLSecondary
	Schema    *XMLSchemaName
}

// UUIDType is UUID.
type UUIDType struct{}

func (t CharacterType) encodeType(w *writer) {
	w.u8(tagCharacter)
	w.u32(t.Length)
	w.u8(byte(t.Units))
}

func (t VaryingType) encodeType(w *writer) {
	w.u8(tagVarying)
	w.u32(t.Length)
	w.u8(byte(t.Units))
}

func (t ExactType) encodeType(w *writer) {
	w.u8(tagExact)
	w.u32(t.Precision)
	w.u32(t.Scale)
}

func (t ApproximateType) encodeType(w *writer) {
	w.u8(tagApproximate)
	w.u32(t.Precision)
}

func (t PluginType) encodeType(w *writer) {
	w.u8(tagPlugin)
	w.str(t.Name)
	w.u32(count32(len(t.Params)))

	for _, p := range t.Params {
		w.u32(p)
	}
}

func (t BinaryType) encodeType(w *writer) {
	w.u8(tagBinary)
	w.u32(t.Length)
}

func (t VarBinaryType) encodeType(w *writer) {
	w.u8(tagBinaryVarying)
	w.u32(t.Length)
}

func (t DateType) encodeType(w *writer) {
	w.u8(tagDate)
	w.u32(t.Precision)
}

func (t TimeType) encodeType(w *writer) {
	w.u8(zonedTag(t.Zoned, tagTime, tagTimeZoned))
	w.u32(t.Precision)
}

func (t TimestampType) encodeType(w *writer) {
	w.u8(zonedTag(t.Zoned, tagTimestamp, tagTimestampZoned))
	w.u32(t.Precision)
}

// zonedTag is zoned's tag if zoned, else plain's.
func zonedTag(zoned bool, plain, withZone byte) byte {
	if zoned {
		return withZone
	}

	return plain
}

func (t IntervalType) encodeType(w *writer) {
	w.u8(tagInterval)
	t.Qualifier.encode(w)
}

// encode writes the qualifier's four u32 fields.
func (q Qualifier) encode(w *writer) {
	w.u32(uint32(q.Start))
	w.u32(uint32(q.End))
	w.u32(q.Leading)
	w.u32(q.Fraction)
}

func (BooleanType) encodeType(w *writer) { w.u8(tagBoolean) }

func (t DecFloatType) encodeType(w *writer) {
	w.u8(tagDecFloat)
	w.u32(t.Precision)
}

func (t ArrayType) encodeType(w *writer) {
	w.u8(tagArray)
	w.u32(t.Max)
	t.Element.encodeType(w)
}

func (t RowType) encodeType(w *writer) {
	w.u8(tagRow)
	w.u32(count32(len(t.Fields)))

	for _, f := range t.Fields {
		w.str(f.Name)
		f.Type.encodeType(w)
	}
}

func (t CLOBType) encodeType(w *writer) {
	w.u8(tagCLOB)
	w.u64(t.Length)
	w.u8(byte(t.Units))
}

func (t BLOBType) encodeType(w *writer) {
	w.u8(tagBLOB)
	w.u64(t.Length)
	w.u8(byte(t.Units))
}

func (t MultisetType) encodeType(w *writer) {
	w.u8(tagMultiset)
	t.Element.encodeType(w)
}

func (t StructuredType) encodeType(w *writer) {
	w.u8(tagStructured)
	t.encodeNames(w)
}

// encodeNames writes the type's schema and name, then its supertypes'.
func (t StructuredType) encodeNames(w *writer) {
	w.str(t.Name.Schema)
	w.str(t.Name.Name)
	w.u32(count32(len(t.Supertypes)))

	for _, s := range t.Supertypes {
		w.str(s.Schema)
		w.str(s.Name)
	}
}

func (t ReferenceType) encodeType(w *writer) {
	w.u8(tagReference)
	t.Referenced.encodeNames(w)
	w.flag(t.Scope != nil)

	if t.Scope != nil {
		w.str(t.Scope.Schema)
		w.str(t.Scope.Name)
	}
}

func (JSONType) encodeType(w *writer) { w.u8(tagJSON) }

func (t XMLType) encodeType(w *writer) {
	w.u8(tagXML)
	w.u8(byte(t.Primary))
	w.u8(byte(t.Secondary))

	if t.Secondary == XMLXMLSchema && t.Schema != nil {
		w.str(t.Schema.Schema)
		w.str(t.Schema.Name)
		w.optional(t.Schema.Namespace)
		w.optional(t.Schema.Element)
	}
}

// optional writes 0 for none, or 1 and the string.
func (w *writer) optional(s *string) {
	w.flag(s != nil)

	if s != nil {
		w.str(*s)
	}
}

func (UUIDType) encodeType(w *writer) { w.u8(tagUUID) }
