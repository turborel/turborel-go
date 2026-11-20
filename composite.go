package turborel

// Composite fields [PROTOCOL Composite fields]: a row, a column, a
// parameter and a failure, as the replies carry them.

// Column is a column of a result: its name, if it has one, its type,
// and whether it is NOT NULL. Type is what its values are read by; a
// type's tag can stand for several declared types, so an INTEGER
// column's Type is an exact numeric type of precision 10 and scale 0.
// Declared is the type it was declared with, where the server told it,
// granted `declared-types`; else nil.
type Column struct {
	Name     string
	Named    bool
	Type     DataType
	NotNull  bool
	Declared *Declared
}

// Parameter is the type of a statement's `?` parameter. Varying says the
// server gives it CHARACTER VARYING of the longest length, as it does
// one whose type the statement does not decide; its type is given all
// the same.
type Parameter struct {
	Varying bool
	Type    DataType
}

// column reads a column: its named flag and name, its type, its not null
// flag.
func (r *reader) column() Column {
	c := Column{Named: r.flag()}
	if c.Named {
		c.Name = r.str()
	}

	c.Type = r.dataType()
	c.NotNull = r.flag()

	return c
}

// columns reads a list of columns.
func (r *reader) columns() []Column {
	n := r.count()

	var list []Column
	for i := uint32(0); r.more(i, n); i++ {
		list = append(list, r.column())
	}

	return list
}

// encode writes the column, as a Description carries it.
func (c Column) encode(w *writer) {
	w.flag(c.Named)

	if c.Named {
		w.str(c.Name)
	}

	c.Type.encodeType(w)
	w.flag(c.NotNull)
}

// parameters reads a list of parameters, each its varying flag and type.
func (r *reader) parameters() []Parameter {
	n := r.count()

	var list []Parameter
	for i := uint32(0); r.more(i, n); i++ {
		list = append(list, Parameter{Varying: r.flag(), Type: r.dataType()})
	}

	return list
}

// rows reads a list of rows, each a list of values.
func (r *reader) rows() []Row {
	n := r.count()

	var list []Row
	for i := uint32(0); r.more(i, n); i++ {
		list = append(list, Row(r.values()))
	}

	return list
}

// failure reads a failure: SQLCODE, SQLSTATE, message, the rule cited,
// the position, whether a transaction is active, and its diagnostics.
func (r *reader) failure() *Error {
	e := &Error{SQLCode: signed32(r.u32()), SQLState: r.str(), Message: r.str()}
	if r.flag() {
		e.Rule = r.str()
	}

	e.Placed = r.flag()
	if e.Placed {
		e.Position = r.u64()
	}

	e.Active = r.flag()
	e.Diagnostics = r.diagnostics()

	return e
}
