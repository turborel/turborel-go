package turborel

// Values between database/sql and the driver (RULES.md D2): a program's
// Go value made the driver's Value it is sent as, and a value read given
// to database/sql as the Go value it scans most simply, the driver's own
// type where Go has none. Each of the driver's value types is a
// sql.Scanner and a driver.Valuer, so a program scans a column into it
// and sends it to any driver.

import (
	"context"
	"database/sql/driver"
	"fmt"
	"io"
	"math"
	"math/big"
	"reflect"
	"strings"
	"time"
)

// The precision of a timestamp a time.Time is sent as: microseconds.
const timePrecision = 6

// The most digits of an exact number of scale 0 an int64 always holds.
const int64Digits = 18

// valueOf gives the Value a Go value is sent as: nil a null; an integer an
// exact number; a float an approximate one; a bool a truth value; a
// string a character string; bytes a binary string; a time.Time a
// TIMESTAMP WITH TIME ZONE to the microsecond; one of the driver's values
// as it is; a driver.Valuer as what it gives.
func valueOf(v any) (Value, error) {
	switch v := v.(type) {
	case nil:
		return Null{}, nil
	case Value:
		return v, nil
	case bool:
		return Bool(v), nil
	case string:
		return Char(v), nil
	case []byte:
		return Binary(append([]byte(nil), v...)), nil
	case float64:
		return Approx(v), nil
	case float32:
		return Approx(float64(v)), nil
	case time.Time:
		return datetimeOf(v), nil
	case driver.Valuer:
		given, err := v.Value()
		if err != nil {
			return nil, err //nolint:wrapcheck // The program's value's own failure.
		}

		return valueOf(given)
	}

	return integerOf(v)
}

// integerOf gives the exact number an integer of any of Go's types is.
func integerOf(v any) (Value, error) {
	r := reflect.ValueOf(v)

	switch r.Kind() { //nolint:exhaustive // The kinds an integer is; any other is refused below.
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return DecimalOf(r.Int(), 0), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return NewDecimal(new(big.Int).SetUint64(r.Uint()), 0)
	}

	return nil, misused(fmt.Sprintf("a value of type %T is no value the driver sends", v))
}

// datetimeOf gives the TIMESTAMP WITH TIME ZONE t is, to the microsecond.
func datetimeOf(t time.Time) Datetime {
	_, offset := t.Zone()

	return Datetime{
		Moment: MomentTimestamp, UTC: t.UnixMicro() + unixEpochDays*microsPerDay,
		Zone:  int16(offset / secondsPerMinute), //nolint:gosec // G115: a displacement's minutes fit 16 bits.
		Zoned: true, Precision: timePrecision,
	}
}

// driverValue gives v as the Go value database/sql scans: a null nil; an
// exact number of scale 0 an int64 holds that int64, another its text, as
// a DECFLOAT; an approximate number a float64; a character string a
// string; a binary string bytes; a truth value a bool; a datetime a
// time.Time; a large object its whole value, a string or bytes, read by
// its locator if it came by one; any other value the driver's own.
func driverValue(ctx context.Context, c *Conn, v Value) (driver.Value, error) {
	switch v := v.(type) {
	case Null:
		return nil, nil //nolint:nilnil // A null is database/sql's nil, and no failure.
	case Decimal:
		if v.scale == 0 && v.units.fitsInt64() {
			return v.Units().Int64(), nil
		}

		return v.String(), nil
	case DecFloat:
		return v.String(), nil
	case Approx:
		return float64(v), nil
	case Char:
		return string(v), nil
	case Binary:
		return []byte(v), nil
	case Bool:
		return bool(v), nil
	case Datetime:
		return v.Time(), nil
	case LOB:
		return lobValue(v.Kind, v.Octets), nil
	case Locator:
		octets, err := io.ReadAll(c.NewLOBReader(ctx, v))
		if err != nil {
			return nil, err //nolint:wrapcheck // The reader gives the driver's failure.
		}

		return lobValue(v.Kind, octets), nil
	}

	return v, nil
}

// lobValue gives a large object's octets as a string, of a character
// one, or as bytes.
func lobValue(kind LOBKind, octets []byte) driver.Value {
	if kind == CharacterLOB {
		return string(octets)
	}

	return octets
}

// scanTypes gives the Go types driverValue gives of each column's type.
func scanType(t DataType) reflect.Type {
	switch t := t.(type) {
	case ExactType:
		if t.Scale == 0 && t.Precision <= int64Digits {
			return reflect.TypeFor[int64]()
		}

		return reflect.TypeFor[string]()
	case DecFloatType, CharacterType, VaryingType, CLOBType:
		return reflect.TypeFor[string]()
	case ApproximateType:
		return reflect.TypeFor[float64]()
	case BinaryType, VarBinaryType, BLOBType:
		return reflect.TypeFor[[]byte]()
	case BooleanType:
		return reflect.TypeFor[bool]()
	case DateType, TimeType, TimestampType:
		return reflect.TypeFor[time.Time]()
	}

	return reflect.TypeFor[any]()
}

// typeName gives a data type's name, as the server's catalog names it.
func typeName(t DataType) string {
	switch t := t.(type) {
	case CharacterType:
		return "CHARACTER"
	case VaryingType:
		return "CHARACTER VARYING"
	case ExactType:
		return "NUMERIC"
	case ApproximateType:
		return "FLOAT"
	case BinaryType:
		return "BINARY"
	case VarBinaryType:
		return "BINARY VARYING"
	case DateType:
		return "DATE"
	case TimeType:
		return zonedName("TIME", t.Zoned)
	case TimestampType:
		return zonedName("TIMESTAMP", t.Zoned)
	case PluginType:
		return t.Name
	}

	return otherTypeName(t)
}

// otherTypeName gives the name of a type of no character, number,
// binary string or datetime.
func otherTypeName(t DataType) string {
	switch t.(type) {
	case IntervalType:
		return "INTERVAL"
	case BooleanType:
		return "BOOLEAN"
	case DecFloatType:
		return "DECFLOAT"
	case CLOBType:
		return "CHARACTER LARGE OBJECT"
	case BLOBType:
		return "BINARY LARGE OBJECT"
	case ArrayType:
		return "ARRAY"
	case MultisetType:
		return "MULTISET"
	case RowType:
		return "ROW"
	case ReferenceType:
		return "REF"
	case JSONType:
		return "JSON"
	case XMLType:
		return "XML"
	case UUIDType:
		return "UUID"
	}

	return "USER-DEFINED"
}

// zonedName gives a datetime type's name, WITH TIME ZONE if zoned.
func zonedName(name string, zoned bool) string {
	if zoned {
		return name + " WITH TIME ZONE"
	}

	return name
}

// scanFailed is a value that cannot be scanned into a type.
func scanFailed(src any, into string) error {
	return misused(fmt.Sprintf("a value of type %T cannot be scanned into %s", src, into))
}

// scanAs sets *into to src, if src is of its type.
func scanAs[T any](into *T, src any, name string) error {
	v, ok := src.(T)
	if !ok {
		return scanFailed(src, name)
	}

	*into = v

	return nil
}

// Scan sets the number to src: a Decimal, an int64, or its text.
func (d *Decimal) Scan(src any) error {
	switch v := src.(type) {
	case int64:
		*d = DecimalOf(v, 0)

		return nil
	case string:
		parsed, err := parseDecimal(v)
		if err != nil {
			return err
		}

		*d = parsed

		return nil
	}

	return scanAs(d, src, "Decimal")
}

// parseDecimal reads an exact number's text, as String writes it.
func parseDecimal(text string) (Decimal, error) {
	whole, fraction, _ := strings.Cut(text, ".")

	units, ok := new(big.Int).SetString(whole+fraction, decimalRadix)
	if !ok || strings.ContainsAny(fraction, "+-") || len(fraction) > math.MaxUint32 {
		return Decimal{}, outOfRange(text + " is no exact number")
	}

	return NewDecimal(units, uint32(len(fraction))) //nolint:gosec // G115: checked above to fit.
}

// Scan sets the number to src: a DecFloat, or its text.
func (d *DecFloat) Scan(src any) error {
	if text, ok := src.(string); ok {
		r, ok := new(big.Rat).SetString(text)
		if !ok {
			return outOfRange(text + " is no decimal floating-point number")
		}

		parsed, err := decFloatOfRat(r, text)
		if err != nil {
			return err
		}

		*d = parsed

		return nil
	}

	return scanAs(d, src, "DecFloat")
}

// decFloatOfRat gives the DECFLOAT text, read as r, is: its digits and
// exponent as written.
func decFloatOfRat(r *big.Rat, text string) (DecFloat, error) {
	mantissa, exponentText, _ := strings.Cut(strings.ToUpper(text), "E")
	whole, fraction, _ := strings.Cut(mantissa, ".")

	exponent := int64(0)
	if exponentText != "" {
		e, ok := new(big.Int).SetString(exponentText, decimalRadix)
		if !ok || !e.IsInt64() {
			return DecFloat{}, outOfRange(text + " is no decimal floating-point number")
		}

		exponent = e.Int64()
	}

	coefficient, ok := new(big.Int).SetString(whole+fraction, decimalRadix)
	if !ok || r == nil {
		return DecFloat{}, outOfRange(text + " is no decimal floating-point number")
	}

	return roundDecFloat(coefficient, exponent-int64(len(fraction)))
}

// Scan sets the number to src: an Approx or a float64.
func (a *Approx) Scan(src any) error {
	if f, ok := src.(float64); ok {
		*a = Approx(f)

		return nil
	}

	return scanAs(a, src, "Approx")
}

// Scan sets the string to src: a Char, a string or bytes.
func (s *Char) Scan(src any) error {
	switch v := src.(type) {
	case string:
		*s = Char(v)

		return nil
	case []byte:
		*s = Char(v)

		return nil
	}

	return scanAs(s, src, "Char")
}

// Scan sets the string to src: a Binary or bytes.
func (b *Binary) Scan(src any) error {
	if v, ok := src.([]byte); ok {
		*b = Binary(append([]byte(nil), v...))

		return nil
	}

	return scanAs(b, src, "Binary")
}

// Scan sets the truth value to src: a Bool or a bool.
func (b *Bool) Scan(src any) error {
	if v, ok := src.(bool); ok {
		*b = Bool(v)

		return nil
	}

	return scanAs(b, src, "Bool")
}

// Scan sets the datetime to src: a Datetime, or a time.Time as a
// TIMESTAMP WITH TIME ZONE to the microsecond.
func (d *Datetime) Scan(src any) error {
	if t, ok := src.(time.Time); ok {
		*d = datetimeOf(t)

		return nil
	}

	return scanAs(d, src, "Datetime")
}

// Scan sets the interval to src, an Interval.
func (i *Interval) Scan(src any) error { return scanAs(i, src, "Interval") }

// Scan sets the large object to src: a LOB, or a string of a character
// one or bytes of a binary one.
func (v *LOB) Scan(src any) error {
	switch s := src.(type) {
	case string:
		*v = LOB{Kind: CharacterLOB, Octets: []byte(s)}

		return nil
	case []byte:
		*v = LOB{Kind: BinaryLOB, Octets: append([]byte(nil), s...)}

		return nil
	}

	return scanAs(v, src, "LOB")
}

// Scan sets the locator to src, a Locator.
func (v *Locator) Scan(src any) error { return scanAs(v, src, "Locator") }

// Scan sets the JSON value to src, a JSON.
func (j *JSON) Scan(src any) error { return scanAs(j, src, "JSON") }

// Scan sets the XML value to src: an XML, or its serialization as a
// string.
func (x *XML) Scan(src any) error {
	if s, ok := src.(string); ok {
		*x = XML(s)

		return nil
	}

	return scanAs(x, src, "XML")
}

// Scan sets the UUID to src: a UUID, or its text.
func (u *UUID) Scan(src any) error {
	if s, ok := src.(string); ok {
		parsed, err := ParseUUID(s)
		if err != nil {
			return err
		}

		*u = parsed

		return nil
	}

	return scanAs(u, src, "UUID")
}

// Scan sets the array to src, an Array.
func (a *Array) Scan(src any) error { return scanAs(a, src, "Array") }

// Scan sets the row to src, a Row.
func (r *Row) Scan(src any) error { return scanAs(r, src, "Row") }

// Scan sets the multiset to src, a Multiset.
func (m *Multiset) Scan(src any) error { return scanAs(m, src, "Multiset") }

// Scan sets the structured value to src, a Structured.
func (s *Structured) Scan(src any) error { return scanAs(s, src, "Structured") }

// Scan sets the reference to src, a Reference.
func (r *Reference) Scan(src any) error { return scanAs(r, src, "Reference") }

// Scan sets the plug-in's value to src, a PluginValue.
func (p *PluginValue) Scan(src any) error { return scanAs(p, src, "PluginValue") }

// Value gives the number as its text, for any driver.
func (d Decimal) Value() (driver.Value, error) { return d.String(), nil }

// Value gives the number as its text, for any driver.
func (d DecFloat) Value() (driver.Value, error) { return d.String(), nil }

// Value gives the number as a float64, for any driver.
func (a Approx) Value() (driver.Value, error) { return float64(a), nil }

// Value gives the string, for any driver.
func (s Char) Value() (driver.Value, error) { return string(s), nil }

// Value gives the octets, for any driver.
func (b Binary) Value() (driver.Value, error) { return []byte(b), nil }

// Value gives the truth value, for any driver.
func (b Bool) Value() (driver.Value, error) { return bool(b), nil }

// Value gives the datetime as a time.Time, for any driver.
func (d Datetime) Value() (driver.Value, error) { return d.Time(), nil }

// Value gives the interval as its text, for any driver.
func (i Interval) Value() (driver.Value, error) { return i.text(), nil }

// Value gives the large object as a string or bytes, for any driver.
func (v LOB) Value() (driver.Value, error) { return lobValue(v.Kind, v.Octets), nil }

// Value gives what the locator is, as its text, for any driver.
func (v Locator) Value() (driver.Value, error) { return v.text(), nil }

// Value gives the JSON value as its JSON text, for any driver.
func (j JSON) Value() (driver.Value, error) { return j.text(), nil }

// Value gives the XML value as its serialization, for any driver.
func (x XML) Value() (driver.Value, error) { return string(x), nil }

// Value gives the UUID as its text, for any driver.
func (u UUID) Value() (driver.Value, error) { return u.String(), nil }

// Value gives the array as its literal, for any driver.
func (a Array) Value() (driver.Value, error) { return valueText(a), nil }

// Value gives the row as its literal, for any driver.
func (r Row) Value() (driver.Value, error) { return valueText(r), nil }

// Value gives the multiset as its literal, for any driver.
func (m Multiset) Value() (driver.Value, error) { return valueText(m), nil }

// Value gives the structured value as its literal, for any driver.
func (s Structured) Value() (driver.Value, error) { return valueText(s), nil }

// Value gives the reference as its literal, for any driver.
func (r Reference) Value() (driver.Value, error) { return valueText(r), nil }

// Value gives the plug-in's value as its text, for any driver.
func (p PluginValue) Value() (driver.Value, error) { return p.Text, nil }

// Value gives a null, nil, for any driver.
func (Null) Value() (driver.Value, error) { return nil, nil } //nolint:nilnil // A null is nil, and no failure.
