package turborel

// Parameter modes [PROTOCOL Parameter modes]: granted `parameter-modes`,
// a Prepared of a CALL of an SQL-invoked procedure ends with tail 20,
// which names the procedure's specific name and then every `?` of the
// statement in the order written, its OUT ones too, each with the mode
// and ordinal position of the procedure's parameter whose argument holds
// it, and its type. The head's parameters are the input ones alone, those
// an Execute gives values for; the output ones' values come back as the
// row the CALL's columns describe.

import "fmt"

// QualifiedName is a schema object's schema and name.
type QualifiedName struct {
	Schema string
	Name   string
}

// called is what a Prepared of a CALL tells of the procedure it invokes
// and of its `?`s.
type called struct {
	procedure  QualifiedName
	parameters []Parameter
}

// preparedTail reads the tail of a Prepared: its declared types given to
// columns, and what it tells of a CALL, nil if nothing.
func (r *reader) preparedTail(columns []Column) *called {
	var told *called

	t := tail{taken: []byte{tailDeclared, tailCalled}}
	for tag, ok := t.next(r); ok; tag, ok = t.next(r) {
		if tag == tailDeclared {
			r.declare(columns)

			continue
		}

		told = &called{procedure: QualifiedName{Schema: r.str(), Name: r.str()}}

		n := r.count()
		for i := uint32(0); r.more(i, n); i++ {
			told.parameters = append(told.parameters, r.calledParameter())
		}
	}

	return told
}

// calledParameter reads one `?` of tail 20: its mode, its ordinal
// position and its parameter. A mode of no code, or a position of 0,
// fails the connection.
func (r *reader) calledParameter() Parameter {
	mode := ParameterMode(r.u8())
	ordinal := r.u32()

	if r.err == nil && mode != ModeIn && mode != ModeInOut && mode != ModeOut {
		r.fail(fmt.Sprintf("%d is no parameter mode's code", mode))
	}

	if r.err == nil && ordinal == 0 {
		r.fail("a parameter's ordinal position is from 1")
	}

	return Parameter{Mode: mode, Ordinal: ordinal, Varying: r.flag(), Type: r.dataType()}
}

// encodeCalled writes tail 20 of told, if there is one.
func encodeCalled(w *writer, told *called) {
	if told == nil {
		return
	}

	w.u8(tailCalled)
	w.str(told.procedure.Schema)
	w.str(told.procedure.Name)
	w.u32(count32(len(told.parameters)))

	for _, p := range told.parameters {
		w.u8(byte(p.Mode))
		w.u32(p.Ordinal)
		w.flag(p.Varying)
		p.Type.encodeType(w)
	}
}
