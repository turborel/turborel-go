package turborel

// Data types read back [PROTOCOL Data types], each checked as the
// reference client checks it: an interval's fields 0 to 5, an array's
// cardinality within its bound, a row of one field or more, an XML
// type's modifiers among those the table lists.

// maxCardinality is the most elements an array type may hold, and a
// multiset value, as turborel's MAX_ARRAY_CARDINALITY.
const maxCardinality = 65535

// maxPluginParams is the most parameters a plug-in's type takes, as
// turborel's MAX_PARAMS.
const maxPluginParams = 2

// dataType reads a data type.
func (r *reader) dataType() DataType {
	tag := r.u8()
	if r.err != nil {
		return nil
	}

	if t, ok := r.scalarType(tag); ok {
		return t
	}

	if t, ok := r.constructedType(tag); ok {
		return t
	}

	r.fail("no data type has this tag")

	return nil
}

// scalarType reads the type of tag if it is one of tags 1 to 15.
func (r *reader) scalarType(tag byte) (DataType, bool) {
	switch tag {
	case tagCharacter:
		return CharacterType{Length: r.u32(), Units: r.units()}, true
	case tagVarying:
		return VaryingType{Length: r.u32(), Units: r.units()}, true
	case tagExact:
		return ExactType{Precision: r.u32(), Scale: r.u32()}, true
	case tagApproximate:
		return ApproximateType{Precision: r.u32()}, true
	case tagPlugin:
		return r.pluginType(), true
	case tagBinary:
		return BinaryType{Length: r.u32()}, true
	case tagBinaryVarying:
		return VarBinaryType{Length: r.u32()}, true
	case tagBoolean:
		return BooleanType{}, true
	case tagDecFloat:
		return DecFloatType{Precision: r.u32()}, true
	default:
		return r.datetimeType(tag)
	}
}

// datetimeType reads the type of tag if it is a datetime or an
// interval.
func (r *reader) datetimeType(tag byte) (DataType, bool) {
	switch tag {
	case tagDate:
		return DateType{Precision: r.u32()}, true
	case tagTime, tagTimeZoned:
		return TimeType{Precision: r.u32(), Zoned: tag == tagTimeZoned}, true
	case tagTimestamp, tagTimestampZoned:
		return TimestampType{Precision: r.u32(), Zoned: tag == tagTimestampZoned}, true
	case tagInterval:
		return IntervalType{Qualifier: r.qualifier()}, true
	default:
		return nil, false
	}
}

// constructedType reads the type of tag if it is one of tags 16 to 27.
func (r *reader) constructedType(tag byte) (DataType, bool) {
	var t DataType

	switch tag {
	case tagArray:
		r.nest(func() { t = r.arrayType() })
	case tagMultiset:
		r.nest(func() { t = MultisetType{Element: r.dataType()} })
	case tagRow:
		r.nest(func() { t = r.rowType() })
	case tagCLOB:
		t = CLOBType{Length: r.u64(), Units: r.units()}
	case tagBLOB:
		t = BLOBType{Length: r.u64(), Units: r.units()}
	case tagStructured:
		t = r.structuredType()
	case tagReference:
		t = ReferenceType{Referenced: r.structuredType(), Scope: r.scope()}
	case tagJSON:
		t = JSONType{}
	case tagXML:
		t = r.xmlType()
	case tagUUID:
		t = UUIDType{}
	default:
		return nil, false
	}

	return t, true
}

// units reads a length's units, 0 characters or 1 octets.
func (r *reader) units() CharUnits {
	u := CharUnits(r.u8())
	if u != Characters && u != Octets {
		r.failValue(corruptValue("a length's units are neither characters nor octets"))
	}

	return u
}

// pluginType reads a plug-in's type: its name and its parameters.
func (r *reader) pluginType() PluginType {
	t := PluginType{Name: r.str()}
	n := r.count()

	for i := uint32(0); r.more(i, n); i++ {
		t.Params = append(t.Params, r.u32())
	}

	if len(t.Params) > maxPluginParams {
		r.fail("a type has too many parameters")
	}

	return t
}

// qualifier reads an interval's qualifier, its fields 0 YEAR to 5
// SECOND.
func (r *reader) qualifier() Qualifier {
	q := Qualifier{Start: IntervalField(r.u32()), End: IntervalField(r.u32()), Leading: r.u32(), Fraction: r.u32()}
	if q.Start > Second || q.End > Second {
		r.fail("an interval's field is out of range")
	}

	return q
}

// arrayType reads an array type's cardinality, 1 to 65535, and its
// element type.
func (r *reader) arrayType() ArrayType {
	t := ArrayType{Max: r.u32()}
	if r.err == nil && (t.Max < 1 || t.Max > maxCardinality) {
		r.fail("an array type's maximum cardinality is out of range")
	}

	t.Element = r.dataType()

	return t
}

// rowType reads a row type's fields, one or more.
func (r *reader) rowType() RowType {
	var t RowType

	n := r.count()
	for i := uint32(0); r.more(i, n); i++ {
		t.Fields = append(t.Fields, Field{Name: r.str(), Type: r.dataType()})
	}

	if r.err == nil && len(t.Fields) == 0 {
		r.fail("a row type has no fields")
	}

	return t
}

// structuredType reads a structured type's names and its supertypes'.
func (r *reader) structuredType() StructuredType {
	t := StructuredType{Name: UDTName{Schema: r.str(), Name: r.str()}}
	n := r.count()

	for i := uint32(0); r.more(i, n); i++ {
		t.Supertypes = append(t.Supertypes, UDTName{Schema: r.str(), Name: r.str()})
	}

	return t
}

// scope reads a reference type's scope: a flag, then the table's names.
func (r *reader) scope() *UDTName {
	if !r.flag() {
		return nil
	}

	return &UDTName{Schema: r.str(), Name: r.str()}
}

// xmlType reads an XML type's modifiers, and its XML Schema's names of
// XMLSCHEMA.
func (r *reader) xmlType() XMLType {
	t := XMLType{Primary: XMLPrimary(r.u8()), Secondary: XMLSecondary(r.u8())}

	switch t.Secondary {
	case XMLUntyped, XMLAny:
	case XMLXMLSchema:
		t.Schema = &XMLSchemaName{Schema: r.str(), Name: r.str(), Namespace: r.optional(), Element: r.optional()}
	default:
		r.fail("an XML type's secondary modifier is not one")
	}

	sequence := t.Primary == XMLSequence && t.Secondary == XMLUntyped
	if t.Primary != XMLDocument && t.Primary != XMLContent && !sequence {
		r.fail("an XML type's primary modifier is not one")
	}

	return t
}

// optional reads 0 for none, or 1 and a string.
func (r *reader) optional() *string {
	if !r.flag() {
		return nil
	}

	s := r.str()

	return &s
}
