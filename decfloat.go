package turborel

// Decimal floating-point numbers [PROTOCOL Values]: a coefficient, an
// i128, times ten to an exponent, an i32. turborel keeps each in one
// form, its coefficient with no trailing zeros (0 of exponent 0), of at
// most 34 digits, its adjusted exponent within decimal128's range; a
// value in any other form is malformed, as the reference client finds it.

import (
	"errors"
	"math/big"
)

// The limits of a DECFLOAT value, as turborel's: decimal128's.
const (
	// maxDecFloatDigits is the most digits of a coefficient,
	// MAX_DECFLOAT_PRECISION.
	maxDecFloatDigits = 34
	// maxDecFloatExponent is the greatest adjusted exponent, that of the
	// leading digit.
	maxDecFloatExponent = 6144
	// minDecFloatExponent is the least adjusted exponent.
	minDecFloatExponent = -6143
)

// The ways a DECFLOAT's fields are not in its one form.
var (
	errDecFloatDigits   = errors.New("a DECFLOAT's coefficient has more than 34 digits")
	errDecFloatExponent = errors.New("a DECFLOAT's adjusted exponent is out of range")
	errDecFloatForm     = errors.New("a DECFLOAT's coefficient has trailing zeros, or zero an exponent")
)

// DecFloat is a decimal floating-point number: Coefficient() times ten to
// the Exponent(). The zero value is 0.
type DecFloat struct {
	coefficient int128
	exponent    int32
}

// NewDecFloat is coefficient times ten to the exponent, its trailing
// zeros taken into the exponent. It fails, as an [*Error] of SQLSTATE
// 22003, if the coefficient has more than 34 significant digits or the
// number is outside DECFLOAT's range.
func NewDecFloat(coefficient *big.Int, exponent int32) (DecFloat, error) {
	c := new(big.Int).Set(coefficient)
	e := int64(exponent)
	ten := big.NewInt(decimalRadix)

	for c.Sign() != 0 && new(big.Int).Rem(c, ten).Sign() == 0 {
		c.Quo(c, ten)
		e++
	}

	if c.Sign() == 0 {
		return DecFloat{}, nil
	}

	if e > maxInt32 {
		return DecFloat{}, outOfRange(errDecFloatExponent.Error())
	}

	bits, ok := int128OfBig(c)
	if !ok {
		return DecFloat{}, outOfRange(errDecFloatDigits.Error())
	}

	// e began as an int32 and only grew, and was checked against the
	// greatest int32 above.
	d := DecFloat{coefficient: bits, exponent: int32(e)} //nolint:gosec // G115: within an int32, as said above.
	if err := d.check(); err != nil {
		return DecFloat{}, outOfRange(err.Error())
	}

	return d, nil
}

// maxInt32 is the greatest int32, an exponent's bound.
const maxInt32 = 1<<31 - 1

// Coefficient gives the number's digits, signed, with no trailing zeros.
func (d DecFloat) Coefficient() *big.Int {
	return d.coefficient.toBig()
}

// Exponent gives the power of ten the coefficient is multiplied by.
func (d DecFloat) Exponent() int32 {
	return d.exponent
}

// String gives the number as its coefficient and exponent, as 15E-1.
func (d DecFloat) String() string {
	return d.Coefficient().String() + "E" + big.NewInt(int64(d.exponent)).String()
}

// check says whether d is in DECFLOAT's one form.
func (d DecFloat) check() error {
	c := d.Coefficient()
	if c.Sign() == 0 {
		if d.exponent != 0 {
			return errDecFloatForm
		}

		return nil
	}

	if new(big.Int).Rem(c, big.NewInt(decimalRadix)).Sign() == 0 {
		return errDecFloatForm
	}

	digits := len(new(big.Int).Abs(c).String())
	if digits > maxDecFloatDigits {
		return errDecFloatDigits
	}

	adjusted := int64(d.exponent) + int64(digits) - 1
	if adjusted < minDecFloatExponent || adjusted > maxDecFloatExponent {
		return errDecFloatExponent
	}

	return nil
}
