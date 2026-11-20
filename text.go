package turborel

// A value's text, as turborel-client shows it: a character string as it
// is; a number, a datetime and an interval as their literals' strings; a
// binary string's octets as hexadecimal digits; a large object held
// whole as its text or octets, one reached by its locator as what it is;
// an array, multiset, row, structured value or reference as its literal;
// a JSON value as its JSON text. An export writes each value so.

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// The words a value's text and literal are written with.
const (
	nullWord         = "NULL"
	trueWord         = "TRUE"
	falseWord        = "FALSE"
	quote            = "'"
	quoteTwice       = "''"
	literalSeparator = ", "
)

// The time a datetime's text counts by.
const (
	microsPerSecond = 1000000
	microsPerMinute = secondsPerMinute * microsPerSecond
	minutesPerHour  = 60
	secondsPerHour  = secondsPerMinute * minutesPerHour
	fractionDigits  = 6
)

// texter is a value that gives its own text.
type texter interface {
	text() string
}

// valueText gives v's text, as turborel-client's Display of the value
// gives it.
func valueText(v Value) string {
	switch v := v.(type) {
	case Null:
		return nullWord
	case Char:
		return string(v)
	case Approx:
		return approxText(float64(v))
	case Binary:
		return strings.ToUpper(hex.EncodeToString(v))
	case Bool:
		return boolWord(bool(v))
	case XML:
		return string(v)
	case PluginValue:
		return v.Text
	case texter:
		return v.text()
	case fmt.Stringer:
		return v.String()
	default:
		return valueLiteral(v)
	}
}

// valueLiteral gives v written as an SQL literal, as turborel's kinds of
// value write one.
func valueLiteral(v Value) string {
	switch v := v.(type) {
	case Array:
		return "ARRAY[" + literals(v) + "]"
	case Multiset:
		return "MULTISET[" + literals(v) + "]"
	case Row:
		return "ROW(" + literals(v) + ")"
	case Structured:
		return v.Type.Schema + "." + v.Type.Name + "(" + literals(v.Attributes) + ")"
	case Reference:
		return "REF(" + valueText(Binary(v)) + ")"
	default:
		return scalarLiteral(v)
	}
}

// scalarLiteral gives v, of no composite type, as an SQL literal.
func scalarLiteral(v Value) string {
	switch v := v.(type) {
	case Char:
		return quoted(string(v))
	case DecFloat:
		return "CAST('" + v.String() + "' AS DECFLOAT)"
	case Binary:
		return "X'" + valueText(v) + quote
	case Datetime:
		return v.keyWord() + " '" + v.text() + quote
	case Interval:
		return v.literal()
	case LOB:
		if v.Kind == CharacterLOB {
			return quoted(v.text())
		}

		return "X'" + v.text() + quote
	case JSON:
		return "JSON(" + quoted(v.text()) + ")"
	case XML:
		return "XMLPARSE(CONTENT " + quoted(string(v)) + " PRESERVE WHITESPACE)"
	case UUID:
		return "UUID '" + v.String() + quote
	default:
		return valueText(v)
	}
}

// literals gives parts' literals, separated by commas.
func literals(parts []Value) string {
	written := make([]string, len(parts))
	for i, p := range parts {
		written[i] = valueLiteral(p)
	}

	return strings.Join(written, literalSeparator)
}

// quoted gives s as a character string literal, a quote in it twice.
func quoted(s string) string {
	return quote + strings.ReplaceAll(s, quote, quoteTwice) + quote
}

// boolWord gives a truth value's literal.
func boolWord(b bool) string {
	if b {
		return trueWord
	}

	return falseWord
}

// approxText gives x as an approximate numeric literal, as Rust's {:E}
// writes it: the shortest mantissa that reads back as x, then E and its
// exponent, as 1.5E2; NaN, inf and -inf as such.
func approxText(x float64) string {
	switch {
	case math.IsNaN(x):
		return "NaN"
	case math.IsInf(x, 1):
		return "inf"
	case math.IsInf(x, -1):
		return "-inf"
	}

	mantissa, exponent, _ := strings.Cut(strconv.FormatFloat(x, 'E', -1, 64), "E")
	e, _ := strconv.Atoi(exponent) // FormatFloat writes a whole exponent.

	return mantissa + "E" + strconv.Itoa(e)
}

// approxLiteral gives x as CAST writes it as a character string, as
// turborel's approximate_literal: 0E0 of a zero, else approxText with a
// point in the mantissa.
func approxLiteral(x float64) string {
	if x == 0 {
		return "0E0"
	}

	text := approxText(x)
	if mantissa, exponent, ok := strings.Cut(text, "E"); ok && !strings.Contains(mantissa, ".") {
		return mantissa + ".0E" + exponent
	}

	return text
}

// local gives the datetime's microseconds as shown, in its displacement:
// of a time, from midnight.
func (d Datetime) local() int64 {
	local := d.UTC + int64(d.Zone)*microsPerMinute
	if d.Moment == MomentTime {
		return floorRem(local, microsPerDay)
	}

	return local
}

// keyWord gives the SQL key word of the datetime's type.
func (d Datetime) keyWord() string {
	switch d.Moment {
	case MomentDate:
		return "DATE"
	case MomentTime:
		return "TIME"
	case MomentTimestamp:
	}

	return "TIMESTAMP"
}

// text gives the datetime's literal's string: YYYY-MM-DD, HH:MM:SS with
// as many digits of the fraction as its precision, and its displacement
// if zoned.
func (d Datetime) text() string {
	var b strings.Builder

	local := d.local()
	days, time0 := floorDiv(local, microsPerDay), floorRem(local, microsPerDay)

	if d.Moment != MomentTime {
		date := time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, int(days))
		fmt.Fprintf(&b, "%04d-%02d-%02d", date.Year(), int(date.Month()), date.Day())
	}

	if d.Moment == MomentDate {
		return b.String()
	}

	if d.Moment == MomentTimestamp {
		b.WriteString(" ")
	}

	seconds := floorDiv(time0, microsPerSecond)
	fmt.Fprintf(&b, "%02d:%02d:%02d", seconds/secondsPerHour, seconds/secondsPerMinute%minutesPerHour,
		seconds%secondsPerMinute)

	if d.Precision > 0 {
		fraction := fmt.Sprintf("%06d", floorRem(time0, microsPerSecond))
		b.WriteString("." + fraction[:min(int(d.Precision), fractionDigits)])
	}

	if d.Zoned {
		b.WriteString(displacementText(d.Zone))
	}

	return b.String()
}

// displacementText gives a displacement in minutes as ±HH:MM.
func displacementText(zone int16) string {
	sign, size := '+', int(zone)
	if zone < 0 {
		sign, size = '-', -size
	}

	return fmt.Sprintf("%c%02d:%02d", sign, size/minutesPerHour, size%minutesPerHour)
}

// floorDiv gives a divided by b, rounded toward negative infinity.
func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}

	return q
}

// floorRem gives a's remainder after floorDiv by b, of b's sign.
func floorRem(a, b int64) int64 {
	return a - floorDiv(a, b)*b
}

// fieldName gives an interval field's name.
func fieldName(f IntervalField) string {
	switch f {
	case Year:
		return "YEAR"
	case Month:
		return "MONTH"
	case Day:
		return "DAY"
	case Hour:
		return "HOUR"
	case Minute:
		return "MINUTE"
	case Second:
		return "SECOND"
	default:
		return strconv.FormatUint(uint64(f), decimalRadix)
	}
}

// String gives the qualifier as SQL writes it, as DAY(2) TO SECOND(6).
func (q Qualifier) String() string {
	start := fieldName(q.Start)
	if q.Start == q.End {
		if q.End == Second {
			return fmt.Sprintf("SECOND(%d, %d)", q.Leading, q.Fraction)
		}

		return fmt.Sprintf("%s(%d)", start, q.Leading)
	}

	text := fmt.Sprintf("%s(%d) TO %s", start, q.Leading, fieldName(q.End))
	if q.End == Second {
		text += fmt.Sprintf("(%d)", q.Fraction)
	}

	return text
}

// fieldUnit gives the months or microseconds of one of field.
func fieldUnit(field IntervalField) *big.Int {
	switch field {
	case Year:
		return big.NewInt(monthsPerYear)
	case Month:
		return big.NewInt(1)
	case Day:
		return big.NewInt(microsPerDay)
	case Hour:
		return big.NewInt(secondsPerHour * microsPerSecond)
	case Minute:
		return big.NewInt(microsPerMinute)
	case Second:
	}

	return big.NewInt(microsPerSecond)
}

// monthsPerYear is the months of a year.
const monthsPerYear = 12

// field gives the size of the interval's field, without its sign: of
// SECOND, in microseconds.
func (i Interval) field(size *big.Int, field IntervalField) *big.Int {
	above := new(big.Int).Set(size)
	if field != i.Qualifier.Start {
		above.Rem(above, fieldUnit(field-1))
	}

	if field == Second {
		return above
	}

	return above.Quo(above, fieldUnit(field))
}

// text gives the interval's literal's string: its sign, if negative; its
// leading field's digits; each other field two, after its separator; the
// second's fraction to its precision.
func (i Interval) text() string {
	var b strings.Builder

	units := i.units.toBig()
	if units.Sign() < 0 {
		b.WriteString("-")
	}

	size := new(big.Int).Abs(units)
	million := big.NewInt(microsPerSecond)

	for f := i.Qualifier.Start; f <= i.Qualifier.End && f <= Second; f++ {
		n := i.field(size, f)

		whole := n
		if f == Second {
			whole = new(big.Int).Quo(n, million)
		}

		if f == i.Qualifier.Start {
			b.WriteString(whole.String())
		} else {
			fmt.Fprintf(&b, "%c%02s", separator(f), whole.String())
		}

		if f == Second && i.Qualifier.Fraction > 0 {
			fraction := fmt.Sprintf("%06s", new(big.Int).Rem(n, million).String())
			b.WriteString("." + fraction[:min(int(i.Qualifier.Fraction), fractionDigits)])
		}
	}

	return b.String()
}

// separator gives the character written before field, not the leading
// one, in an interval's string: - before MONTH, a space before HOUR, :
// before MINUTE and SECOND.
func separator(field IntervalField) rune {
	switch field {
	case Month:
		return '-'
	case Hour:
		return ' '
	case Year, Day, Minute, Second:
	}

	return ':'
}

// literal gives the interval as an interval literal, its sign before its
// quoted string, as INTERVAL -'1 02' DAY(2) TO HOUR.
func (i Interval) literal() string {
	text := i.text()
	sign := ""

	if body, ok := strings.CutPrefix(text, "-"); ok {
		sign, text = "-", body
	}

	return "INTERVAL " + sign + quoted(text) + " " + i.Qualifier.String()
}

// text gives a large object held whole as its text, a binary one's
// octets as hexadecimal digits.
func (v LOB) text() string {
	if v.Kind == CharacterLOB {
		return strings.ToValidUTF8(string(v.Octets), "�")
	}

	return valueText(Binary(v.Octets))
}

// text gives what a large object reached by its locator is.
func (v Locator) text() string {
	if v.Kind == CharacterLOB {
		return fmt.Sprintf("<CHARACTER LARGE OBJECT of %d characters>", v.Characters)
	}

	return fmt.Sprintf("<BINARY LARGE OBJECT of %d octets>", v.Octets)
}

// The JSON text's literal names, and the last control character, which
// a string escapes.
const (
	jsonTrueText  = "true"
	jsonFalseText = "false"
	jsonNullText  = "null"
	lastControl   = 0x1F
)

// The note of an item of which no JSON text is, as turborel's.
const noJSONText = "[ISO/IEC 9075-2:2023 §9.43 GR2a ii] no JSON text parses to "

// text gives the JSON value's JSON text, as turborel serializes it: no
// insignificant white space, members in their order, a string's
// quotation marks, reverse solidi and control characters escaped, a
// number as CAST writes it, a decimal floating-point one plain; of an
// item no JSON text is, a note saying so.
func (j JSON) text() string {
	item, err := j.Item()
	if failure, ok := errors.AsType[*Error](err); ok {
		return "<" + failure.Message + ">"
	}

	var b strings.Builder
	if why := writeJSON(&b, item); why != "" {
		return "<" + noJSONText + why + ">"
	}

	return b.String()
}

// writeJSON writes item's JSON text to b: what it holds of which no JSON
// text is, if it holds one.
func writeJSON(b *strings.Builder, item JSONItem) string {
	switch v := item.(type) {
	case JSONArray:
		return writeJSONArray(b, v)
	case JSONObject:
		return writeJSONObject(b, v)
	case Char:
		writeJSONString(b, string(v))
	case Bool:
		if v {
			b.WriteString(jsonTrueText)
		} else {
			b.WriteString(jsonFalseText)
		}
	case Decimal:
		b.WriteString(v.String())
	case DecFloat:
		b.WriteString(v.numeral())
	case Approx:
		if math.IsInf(float64(v), 0) || math.IsNaN(float64(v)) {
			return "an approximate number that is not finite"
		}

		b.WriteString(approxLiteral(float64(v)))
	case Datetime:
		return "a datetime"
	default:
		b.WriteString(jsonNullText)
	}

	return ""
}

// writeJSONObject writes an object's JSON text to b.
func writeJSONObject(b *strings.Builder, members JSONObject) string {
	b.WriteString("{")

	for i, m := range members {
		if i > 0 {
			b.WriteString(",")
		}

		writeJSONString(b, m.Key)
		b.WriteString(":")

		if why := writeJSON(b, m.Value); why != "" {
			return why
		}
	}

	b.WriteString("}")

	return ""
}

// writeJSONArray writes an array's JSON text to b.
func writeJSONArray(b *strings.Builder, elements JSONArray) string {
	b.WriteString("[")

	for i, e := range elements {
		if i > 0 {
			b.WriteString(",")
		}

		if why := writeJSON(b, e); why != "" {
			return why
		}
	}

	b.WriteString("]")

	return ""
}

// writeJSONString writes s as a JSON string: a quotation mark and a
// reverse solidus escaped, a control character by its two-character
// escape, else by \u and four lower case hexadecimal digits.
func writeJSONString(b *strings.Builder, s string) {
	b.WriteString(`"`)

	for _, c := range s {
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if c <= lastControl {
				fmt.Fprintf(b, `\u%04x`, c)
			} else {
				b.WriteRune(c)
			}
		}
	}

	b.WriteString(`"`)
}
