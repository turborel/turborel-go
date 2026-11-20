package turborel

// Exact numbers [PROTOCOL Values]: units, an i128, divided by ten to the
// scale, a u32. Decimal holds every one the protocol can carry, the
// whole range of the units, with no loss, and gives it to a program as
// math/big.

import (
	"errors"
	"math/big"
	"strconv"
	"strings"
)

// decimalRadix is the base of a scale.
const decimalRadix = 10

// errUnitsRange is units outside an i128's range.
var errUnitsRange = errors.New("the units are outside the range of a 128-bit integer")

// Decimal is an exact number: Units() divided by ten to the Scale(). The
// zero value is 0.
type Decimal struct {
	units int128
	scale uint32
}

// NewDecimal is the exact number units divided by ten to the scale. It
// fails, as an [*Error] of SQLSTATE 22003, if units is outside the range
// of a signed 128-bit integer, which is what the protocol carries.
func NewDecimal(units *big.Int, scale uint32) (Decimal, error) {
	bits, ok := int128OfBig(units)
	if !ok {
		return Decimal{}, outOfRange(errUnitsRange.Error())
	}

	return Decimal{units: bits, scale: scale}, nil
}

// DecimalOf is the exact number units divided by ten to the scale.
func DecimalOf(units int64, scale uint32) Decimal {
	return Decimal{units: int128Of(units), scale: scale}
}

// Units gives the number's units, its digits as an integer.
func (d Decimal) Units() *big.Int {
	return d.units.toBig()
}

// Scale gives the number's scale, the digits after its decimal point.
func (d Decimal) Scale() uint32 {
	return d.scale
}

// Rat gives the number as a fraction.
func (d Decimal) Rat() *big.Rat {
	denominator := new(big.Int).Exp(big.NewInt(decimalRadix), big.NewInt(int64(d.scale)), nil)

	return new(big.Rat).SetFrac(d.Units(), denominator)
}

// String gives the number as SQL writes it: its digits, with as many
// after the decimal point as its scale, as -12.50. A scale past 4096,
// which no SQL type has, is written as an exponent, as 125E-5000, so the
// text of a number a server sent stays as short as its fields.
func (d Decimal) String() string {
	if d.scale > maxPlainScale {
		return d.Units().String() + "E-" + strconv.FormatUint(uint64(d.scale), decimalRadix)
	}

	return pointed(d.Units(), d.scale)
}

// maxPlainScale is the greatest scale String writes with a decimal
// point.
const maxPlainScale = 4096

// pointed writes units with scale digits after a decimal point.
func pointed(units *big.Int, scale uint32) string {
	digits := new(big.Int).Abs(units).String()
	sign := ""

	if units.Sign() < 0 {
		sign = "-"
	}

	if scale == 0 {
		return sign + digits
	}

	places := int(scale)
	if len(digits) <= places {
		digits = strings.Repeat("0", places-len(digits)+1) + digits
	}

	point := len(digits) - places

	return sign + digits[:point] + "." + digits[point:]
}
