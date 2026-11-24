package turborel

// A CALL through database/sql [PROTOCOL Parameter modes] [CLIENT Prepared
// statements]: granted `parameter-modes`, each `?` of a prepared CALL has
// its parameter's mode. An IN one takes a value; an OUT one takes an
// sql.Out, given no value, whose Dest the CALL's row fills; an INOUT one
// takes an sql.Out with In set, its Dest's value sent and filled again,
// or a plain value, its output not read.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"reflect"
	"slices"
)

// output is where an OUT or INOUT parameter's value goes: the sql.Out's
// Dest, and the column of the CALL's row that holds it.
type output struct {
	dest   any
	column int
}

// bind gives the values args send and the outputs they ask for, by the
// statement's parameters' modes; told none, every arg is an input.
func (s *sqlStmt) bind(args []driver.NamedValue) ([]Value, []output, error) {
	if s.st.called == nil {
		given, err := values(args)

		return given, nil, err
	}

	params := s.st.called.parameters
	if len(args) != len(params) {
		return nil, nil, misused(fmt.Sprintf("%d arguments for %d parameters", len(args), len(params)))
	}

	columns := outputColumns(params)

	var (
		given []Value
		outs  []output
	)

	for i, a := range args {
		v, out, err := bindOne(params[i], a.Value, i+1)
		if err != nil {
			return nil, nil, err
		}

		if v != nil {
			given = append(given, v)
		}

		if out != nil {
			outs = append(outs, output{dest: out.Dest, column: columns[params[i].Ordinal]})
		}
	}

	return given, outs, nil
}

// bindOne gives the value arg sends to the `?` at place, of p's mode, nil
// for an OUT one, and the sql.Out whose Dest takes its output, if any.
func bindOne(p Parameter, arg any, place int) (Value, *sql.Out, error) {
	out, isOut := arg.(sql.Out)

	switch p.Mode {
	case ModeOut:
		if !isOut || out.In {
			return nil, nil, misused(fmt.Sprintf("the ? %d is of an OUT parameter: give it sql.Out, In not set", place))
		}

		return nil, &out, nil
	case ModeInOut:
		if !isOut {
			v, err := valueOf(arg)

			return v, nil, err
		}

		if !out.In {
			return nil, nil, misused(fmt.Sprintf("the ? %d is of an INOUT parameter: give it sql.Out, In set", place))
		}

		v, err := pointedTo(out.Dest)
		if err != nil {
			return nil, nil, err
		}

		input, err := valueOf(v)

		return input, &out, err
	case ModeIn:
	}

	if isOut {
		return nil, nil, misused(fmt.Sprintf("the ? %d is of an IN parameter: give it a value, not sql.Out", place))
	}

	v, err := valueOf(arg)

	return v, nil, err
}

// pointedTo gives the value dest points to.
func pointedTo(dest any) (any, error) {
	rv := reflect.ValueOf(dest)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return nil, misused(fmt.Sprintf("sql.Out's Dest is %T, no pointer", dest))
	}

	return rv.Elem().Interface(), nil
}

// outputColumns gives the column of the CALL's row of each OUT or INOUT
// parameter, by its ordinal position: one column for each, in the
// procedure's order.
func outputColumns(params []Parameter) map[uint32]int {
	var ordinals []uint32

	for _, p := range params {
		if p.Mode != ModeIn && !slices.Contains(ordinals, p.Ordinal) {
			ordinals = append(ordinals, p.Ordinal)
		}
	}

	slices.Sort(ordinals)

	columns := make(map[uint32]int, len(ordinals))
	for i, ordinal := range ordinals {
		columns[ordinal] = i
	}

	return columns
}

// fill puts the CALL's outputs, the one row out holds, in their Dests.
func fill(ctx context.Context, c *Conn, out *Outcome, outs []output) error {
	if len(outs) == 0 {
		return nil
	}

	if len(out.Rows) != 1 {
		return misused(fmt.Sprintf("the CALL gave %d rows of its outputs, not one", len(out.Rows)))
	}

	row := out.Rows[0]

	for _, o := range outs {
		if o.column >= len(row) {
			return misused(fmt.Sprintf("the CALL gave %d outputs, none for column %d", len(row), o.column+1))
		}

		v, err := driverValue(ctx, c, row[o.column])
		if err != nil {
			return err
		}

		if err := assignOut(o.dest, v); err != nil {
			return err
		}
	}

	return nil
}

// assignOut puts v, a value as Rows.Next gives it, in dest: by its Scan,
// as it is, or converted to another of Go's numbers where it converts
// back the same, so no value is rounded or cut.
func assignOut(dest any, v driver.Value) error {
	if scanner, ok := dest.(sql.Scanner); ok {
		return scanner.Scan(v) //nolint:wrapcheck // The program's own type's failure.
	}

	rv := reflect.ValueOf(dest)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return misused(fmt.Sprintf("sql.Out's Dest is %T, no pointer", dest))
	}

	into := rv.Elem()

	if v == nil {
		if into.Kind() == reflect.Pointer || into.Kind() == reflect.Interface {
			into.SetZero()

			return nil
		}

		return misused(fmt.Sprintf("a null cannot be put in %T", dest))
	}

	src := reflect.ValueOf(v)

	switch {
	case src.Type().AssignableTo(into.Type()):
		into.Set(src)
	case into.Kind() == reflect.String && src.Kind() == reflect.Slice && src.Type().Elem().Kind() == reflect.Uint8:
		into.SetString(string(src.Bytes()))
	case isNumber(src.Kind()) && isNumber(into.Kind()) && convertsBack(src, into.Type()):
		into.Set(src.Convert(into.Type()))
	default:
		return misused(fmt.Sprintf("a %T cannot be put in %T", v, dest))
	}

	return nil
}

// isNumber says whether kind is one of Go's integers or floats.
func isNumber(kind reflect.Kind) bool {
	return (kind >= reflect.Int && kind <= reflect.Uint64) || kind == reflect.Float32 || kind == reflect.Float64
}

// convertsBack says whether src converted to t converts back to itself.
func convertsBack(src reflect.Value, t reflect.Type) bool {
	return src.Convert(t).Convert(src.Type()).Equal(src)
}

// values gives args as the driver's values; an sql.Out is refused, as
// only a CALL whose parameters' modes the server told takes one.
func values(args []driver.NamedValue) ([]Value, error) {
	all := make([]Value, len(args))

	for i, a := range args {
		if _, ok := a.Value.(sql.Out); ok {
			return nil, misused("sql.Out is taken by the ? of a CALL's OUT or INOUT parameter, as told " +
				"by a server granting parameter-modes")
		}

		v, err := valueOf(a.Value)
		if err != nil {
			return nil, err
		}

		all[i] = v
	}

	return all, nil
}
