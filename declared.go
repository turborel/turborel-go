package turborel

// Declared types [PROTOCOL Declared types]: a data type's tag can stand
// for several declared types, as SMALLINT, INTEGER, BIGINT, DECIMAL and
// NUMERIC are all an exact numeric type of tag 2. Granted
// `declared-types`, a Prepared or a Description of one column or more
// ends with tail 19, which names the type each column was declared with,
// in the columns' order. The values are written by their own tags as
// ever.

import (
	"fmt"
	"strings"
)

// Declared is the type a column was declared with, as the server told
// it granted `declared-types`.
type Declared struct {
	// Name is the type's name as the Information Schema's DATA_TYPE gives
	// it: INTEGER, SMALLINT, BIGINT, DECIMAL, NUMERIC, REAL, DOUBLE
	// PRECISION, FLOAT, USER-DEFINED of a distinct type, ARRAY, or the
	// name of any other type.
	Name string
	// Written is the whole type as SQL writes it: INTEGER, DECIMAL(9,2),
	// a distinct type's schema and name, or INTEGER ARRAY[3].
	Written string
}

// TypeWritten gives the column's type as SQL writes it: its declared
// type where the server told it, else the type its tag says, as
// NUMERIC(10,0) of an INTEGER column.
func (c Column) TypeWritten() string {
	if c.Declared != nil {
		return c.Declared.Written
	}

	return writtenType(c.Type)
}

// describedColumns reads the columns of a Description and the tail after
// them, its declared types given to the columns: one for each, or none.
func (r *reader) describedColumns() []Column {
	columns := r.columns()

	t := tail{taken: []byte{tailDeclared}}
	for _, ok := t.next(r); ok; _, ok = t.next(r) {
		r.declare(columns)
	}

	return columns
}

// declare reads tail 19 and gives its declared types to columns, failing
// the connection unless there is one for each.
func (r *reader) declare(columns []Column) {
	declared := r.declaredTypes()
	if r.err != nil {
		return
	}

	if len(declared) != len(columns) {
		r.fail("its declared types are not one for each column")

		return
	}

	for i := range columns {
		columns[i].Declared = &declared[i]
	}
}

// declaredTypes reads tail 19's list: each its name and its type as SQL
// writes it.
func (r *reader) declaredTypes() []Declared {
	n := r.count()

	var list []Declared
	for i := uint32(0); r.more(i, n); i++ {
		list = append(list, Declared{Name: r.str(), Written: r.str()})
	}

	return list
}

// encodeDeclared writes tail 19 of columns, if any has a declared type;
// one without is written as its data type says.
func encodeDeclared(w *writer, columns []Column) {
	told := false
	for _, c := range columns {
		told = told || c.Declared != nil
	}

	if !told {
		return
	}

	w.u8(tailDeclared)
	w.u32(count32(len(columns)))

	for _, c := range columns {
		d := Declared{Name: typeName(c.Type), Written: writtenType(c.Type)}
		if c.Declared != nil {
			d = *c.Declared
		}

		w.str(d.Name)
		w.str(d.Written)
	}
}

// writtenType gives a data type as SQL writes it, as turborel writes the
// type a reply's tag gives: an exact numeric type as NUMERIC(p,s), an
// approximate one as FLOAT(p).
func writtenType(t DataType) string {
	switch t := t.(type) {
	case CharacterType:
		return lengthWritten("CHARACTER", uint64(t.Length), t.Units)
	case VaryingType:
		return lengthWritten("CHARACTER VARYING", uint64(t.Length), t.Units)
	case ExactType:
		return fmt.Sprintf("NUMERIC(%d,%d)", t.Precision, t.Scale)
	case ApproximateType:
		return fmt.Sprintf("FLOAT(%d)", t.Precision)
	case BinaryType:
		return fmt.Sprintf("BINARY(%d)", t.Length)
	case VarBinaryType:
		return fmt.Sprintf("BINARY VARYING(%d)", t.Length)
	case TimeType:
		return zonedName(fmt.Sprintf("TIME(%d)", t.Precision), t.Zoned)
	case TimestampType:
		return zonedName(fmt.Sprintf("TIMESTAMP(%d)", t.Precision), t.Zoned)
	case IntervalType:
		return "INTERVAL " + t.Qualifier.String()
	case DecFloatType:
		return fmt.Sprintf("DECFLOAT(%d)", t.Precision)
	case CLOBType:
		return lengthWritten("CHARACTER LARGE OBJECT", t.Length, t.Units)
	case BLOBType:
		return fmt.Sprintf("BINARY LARGE OBJECT(%d)", t.Length)
	}

	return compositeWritten(t)
}

// compositeWritten gives a type of no string, number or datetime as SQL
// writes it.
func compositeWritten(t DataType) string {
	switch t := t.(type) {
	case ArrayType:
		return fmt.Sprintf("%s ARRAY[%d]", writtenType(t.Element), t.Max)
	case MultisetType:
		return writtenType(t.Element) + " MULTISET"
	case RowType:
		fields := make([]string, len(t.Fields))
		for i, f := range t.Fields {
			fields[i] = f.Name + " " + writtenType(f.Type)
		}

		return "ROW(" + strings.Join(fields, ", ") + ")"
	case StructuredType:
		return t.Name.Schema + "." + t.Name.Name
	case ReferenceType:
		written := "REF(" + t.Referenced.Name.Schema + "." + t.Referenced.Name.Name + ")"
		if t.Scope != nil {
			written += " SCOPE " + t.Scope.Schema + "." + t.Scope.Name
		}

		return written
	case XMLType:
		return xmlWritten(t)
	case PluginType:
		return t.Name
	}

	return typeName(t)
}

// xmlWritten gives an XML type as SQL writes it: of XMLSCHEMA, its
// registered XML Schema by ID, then its namespace and element.
func xmlWritten(t XMLType) string {
	primary := [...]string{XMLDocument: "DOCUMENT", XMLContent: "CONTENT", XMLSequence: "SEQUENCE"}
	secondary := [...]string{XMLUntyped: "UNTYPED", XMLAny: "ANY", XMLXMLSchema: "XMLSCHEMA"}

	name := "?"
	if int(t.Primary) < len(primary) {
		name = primary[t.Primary]
	}

	switch {
	case t.Primary == XMLSequence:
		return "XML(SEQUENCE)"
	case t.Schema != nil:
		return fmt.Sprintf("XML(%s(XMLSCHEMA %s))", name, schemaWritten(*t.Schema))
	case int(t.Secondary) < len(secondary):
		return fmt.Sprintf("XML(%s(%s))", name, secondary[t.Secondary])
	}

	return fmt.Sprintf("XML(%s)", name)
}

// schemaWritten gives the registered XML Schema an XML type names, as
// an XML valid element clause writes it.
func schemaWritten(s XMLSchemaName) string {
	written := "ID " + s.Schema + "." + s.Name

	if s.Namespace != nil {
		if *s.Namespace == "" {
			written += " NO NAMESPACE"
		} else {
			written += " NAMESPACE '" + strings.ReplaceAll(*s.Namespace, "'", "''") + "'"
		}
	}

	if s.Element != nil {
		written += ` ELEMENT "` + strings.ReplaceAll(*s.Element, `"`, `""`) + `"`
	}

	return written
}
