package turborel

import (
	"errors"
	"math/big"
	"testing"
)

// assertOutOfRange checks that err is a number refused, SQLSTATE 22003.
func assertOutOfRange(t *testing.T, err error) {
	t.Helper()

	var failure *Error
	if !errors.As(err, &failure) || failure.SQLState != sqlstateOutOfRange || failure.SQLCode != sqlcodeData {
		t.Errorf("err = %v, want SQLSTATE 22003", err)
	}
}

// bigOf is the integer text writes.
func bigOf(t *testing.T, text string) *big.Int {
	t.Helper()

	n, ok := new(big.Int).SetString(text, decimalRadix)
	if !ok {
		t.Fatalf("no integer %q", text)
	}

	return n
}

// TestExactFullRange round-trips the units of an exact number at the
// limits of an i128.
//
// Verifies: [PROTOCOL Values] an exact number's units, an i128, reach
// Decimal with no loss across its whole range, -2^127 to 2^127 - 1, its
// scale a u32 to its greatest, and units outside it are refused.
func TestExactFullRange(t *testing.T) {
	t.Parallel()

	least, greatest := int128Limits()
	for _, units := range []*big.Int{least, greatest, big.NewInt(0), big.NewInt(-1), bigOf(t, "18446744073709551616")} {
		d, err := NewDecimal(units, 4294967295)
		if err != nil {
			t.Fatalf("NewDecimal(%v): %v", units, err)
		}

		var w writer

		d.encodeValue(&w)

		r := newReader(w.buf, "test")

		back, ok := r.value().(Decimal)
		if !ok || r.finish() != nil || back.Units().Cmp(units) != 0 || back.Scale() != 4294967295 {
			t.Errorf("%v read back as %v scale %d", units, back.Units(), back.Scale())
		}
	}

	_, err := NewDecimal(new(big.Int).Add(greatest, big.NewInt(1)), 0)
	assertOutOfRange(t, err)

	_, err = NewDecimal(new(big.Int).Sub(least, big.NewInt(1)), 0)
	assertOutOfRange(t, err)
}

// TestExactText writes an exact number as SQL does.
//
// Verifies: [PROTOCOL Values] the number is the units divided by ten to
// the scale: -1250 of scale 2 is -12.50, 5 of scale 3 is 0.005, and a
// scale past any SQL type's is written with an exponent.
func TestExactText(t *testing.T) {
	t.Parallel()

	cases := map[string]Decimal{
		"-12.50":    DecimalOf(-1250, 2),
		"0.005":     DecimalOf(5, 3),
		"7":         DecimalOf(7, 0),
		"-0.1":      DecimalOf(-1, 1),
		"125E-5000": DecimalOf(125, 5000),
	}
	for want, d := range cases {
		if got := d.String(); got != want {
			t.Errorf("String() = %q, want %q", got, want)
		}
	}

	if DecimalOf(-1250, 2).Rat().Cmp(big.NewRat(-25, 2)) != 0 {
		t.Error("Rat() of -12.50 is not -25/2")
	}
}

// TestDecFloatForm keeps a DECFLOAT in its one form.
//
// Verifies: [PROTOCOL Values] a decimal floating-point value's
// coefficient and exponent are made as turborel's DecFloat::new makes
// them: trailing zeros taken into the exponent, at most 34 digits, the
// adjusted exponent within -6143 and 6144; outside it, refused.
func TestDecFloatForm(t *testing.T) {
	t.Parallel()

	d, err := NewDecFloat(big.NewInt(1500), -3)
	if err != nil || d.Coefficient().Int64() != 15 || d.Exponent() != -1 || d.String() != "15E-1" {
		t.Errorf("1500E-3 = %v, %v", d, err)
	}

	zero, err := NewDecFloat(big.NewInt(0), 9)
	if err != nil || zero.Exponent() != 0 || zero.Coefficient().Sign() != 0 {
		t.Errorf("0E9 = %v, %v", zero, err)
	}

	if _, err := NewDecFloat(big.NewInt(1), 6144); err != nil {
		t.Errorf("1E6144: %v", err)
	}

	for _, c := range []struct {
		coefficient *big.Int
		exponent    int32
	}{
		{big.NewInt(10), 6144},
		{big.NewInt(1), -6144},
		{big.NewInt(10), 2147483647},
		{bigOf(t, "99999999999999999999999999999999999"), 6110},
	} {
		_, err := NewDecFloat(c.coefficient, c.exponent)
		assertOutOfRange(t, err)
	}
}

// TestDecFloatRounded rounds a coefficient past 34 digits as turborel
// does.
//
// Verifies: [PROTOCOL Values] a coefficient of more than 34 digits is
// rounded half to even to 34, as turborel's DecFloat::new rounds it:
// exactly half rounds to the even digit, more than half up, a sticky
// digit below the half up, and 35 nines up to the next power of ten.
func TestDecFloatRounded(t *testing.T) {
	t.Parallel()

	cases := []struct {
		coefficient, want string
		exponent          int32
	}{
		{"12345678901234567890123456789012345", "1234567890123456789012345678901234", 1},
		{"12345678901234567890123456789012355", "1234567890123456789012345678901236", 1},
		{"12345678901234567890123456789012346", "1234567890123456789012345678901235", 1},
		{"123456789012345678901234567890123451", "1234567890123456789012345678901235", 2},
		{"-12345678901234567890123456789012345", "-1234567890123456789012345678901234", 1},
		{"99999999999999999999999999999999999", "1", 35},
		{"170141183460469231731687303715884105727", "1701411834604692317316873037158841", 5},
	}
	for _, c := range cases {
		d, err := NewDecFloat(bigOf(t, c.coefficient), 0)
		if err != nil || d.Coefficient().String() != c.want || d.Exponent() != c.exponent {
			t.Errorf("%s rounded to %vE%d, %v; want %sE%d",
				c.coefficient, d.Coefficient(), d.Exponent(), err, c.want, c.exponent)
		}
	}
}

// TestDecFloatChecked refuses fields out of the one form.
//
// Verifies: [PROTOCOL Values] a DECFLOAT read with trailing zeros, zero
// of another exponent, or more than 34 digits is malformed, as the
// reference client finds it.
func TestDecFloatChecked(t *testing.T) {
	t.Parallel()

	for _, d := range []DecFloat{
		{coefficient: int128Of(10)},
		{exponent: 1},
		{coefficient: int128Of(1), exponent: 6145},
	} {
		if d.check() == nil {
			t.Errorf("%+v passed", d)
		}
	}
}

// TestInt128Narrowing says whether an int128 fits an int64.
//
// Verifies: [PROTOCOL Values] an i128 is two's complement: -1 and the
// int64 limits fit 64 bits, one past them does not.
func TestInt128Narrowing(t *testing.T) {
	t.Parallel()

	fits := func(text string) bool {
		x, ok := int128OfBig(bigOf(t, text))
		if !ok {
			t.Fatalf("%s outside an int128", text)
		}

		return x.fitsInt64()
	}

	for text, want := range map[string]bool{
		"-1": true, "9223372036854775807": true, "-9223372036854775808": true,
		"9223372036854775808": false, "-9223372036854775809": false,
	} {
		if fits(text) != want {
			t.Errorf("%s fits an int64: %v, want %v", text, !want, want)
		}
	}

	if !int128Of(0).isZero() || int128Of(-1).isZero() {
		t.Error("isZero misread")
	}
}
