package turborel

// A string assigned to a character string type as INSERT assigns it
// [ISO/IEC 9075:1992 §13.8 GR4b iii], as turborel's assign does it: to
// CHARACTER, padded with spaces to its length, or cut of spaces beyond
// it [§9.2 GR3b]; to CHARACTER VARYING, kept, or cut of spaces beyond its
// maximum length [§9.2 GR3e]; to CHARACTER LARGE OBJECT, kept, or cut of
// spaces beyond its maximum length [ISO/IEC 9075-2:2023 §9.2 GR3e]. A
// string longer, of more than spaces beyond, is refused (SQLSTATE 22001).
// A length in octets counts octets, whole characters of no more.

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// The rules a string's assignment cites.
const (
	insertAssigns   = "ISO/IEC 9075:1992 §13.8 GR4b iii"
	assignCharacter = insertAssigns + ", §9.2 GR3b ii"
	assignVarying   = insertAssigns + ", §9.2 GR3e ii"
	assignLOB       = insertAssigns + "; ISO/IEC 9075-2:2023 §9.2 GR3e ii"
)

// sqlstateRightTruncation is the SQLSTATE of a string too long for its
// type: class 22, data exception, subclass 001.
const sqlstateRightTruncation = "22001"

// space is the character a character string is padded with.
const space = " "

// assigned gives values as the parameters params take them: each string
// for a character string parameter assigned to its type, the rest as
// they are.
func assigned(values []Value, params []Parameter) ([]Value, *Error) {
	out := make([]Value, len(values))

	for i, v := range values {
		s, isString := v.(Char)
		if !isString || i >= len(params) {
			out[i] = v

			continue
		}

		given, err := assignString(string(s), params[i].Type)
		if err != nil {
			return nil, err
		}

		out[i] = given
	}

	return out, nil
}

// assignString gives s assigned to t, if t is a character string type;
// else s as it is.
func assignString(s string, t DataType) (Value, *Error) {
	switch t := t.(type) {
	case CharacterType:
		kept, err := fitString(s, uint64(t.Length), t.Units, writtenType("CHARACTER", uint64(t.Length), t.Units),
			assignCharacter)
		if err != nil {
			return nil, err
		}

		return Char(kept + strings.Repeat(space, clampInt(uint64(t.Length)-countUnits(kept, t.Units)))), nil
	case VaryingType:
		kept, err := fitString(s, uint64(t.Length), t.Units, writtenType("CHARACTER VARYING", uint64(t.Length), t.Units),
			assignVarying)
		if err != nil {
			return nil, err
		}

		return Char(kept), nil
	case CLOBType:
		kept, err := fitLOB(s, t)
		if err != nil {
			return nil, err
		}

		return LOB{Kind: CharacterLOB, Octets: []byte(kept)}, nil
	default:
		return Char(s), nil
	}
}

// countUnits gives the characters, or octets, of s.
func countUnits(s string, units CharUnits) uint64 {
	if units == Octets {
		return len64(len(s))
	}

	return len64(utf8.RuneCountInString(s))
}

// prefixUnits gives the longest start of s of no more than most units,
// whole characters.
func prefixUnits(s string, most uint64, units CharUnits) string {
	if units == Octets {
		end := clampInt(min(most, len64(len(s))))
		for end > 0 && end < len(s) && !utf8.RuneStart(s[end]) {
			end--
		}

		return s[:end]
	}

	n := uint64(0)
	for at := range s {
		if n == most {
			return s[:at]
		}

		n++
	}

	return s
}

// fitString gives s fitted to length, cut of spaces beyond it; a string
// with more than spaces beyond is refused.
func fitString(s string, length uint64, units CharUnits, written, rule string) (string, *Error) {
	count := countUnits(s, units)
	if count <= length {
		return s, nil
	}

	kept := prefixUnits(s, length, units)
	if strings.Trim(s[len(kept):], space) == "" {
		return kept, nil
	}

	unit := "characters"
	if units == Octets {
		unit = "octets"
	}

	message := "a value of " + strconv.FormatUint(count, decimalRadix) + " " + unit + ", not spaces beyond " +
		strconv.FormatUint(length, decimalRadix) + ", does not fit " + written

	return "", truncated(message, rule, written)
}

// fitLOB gives s fitted to the CHARACTER LARGE OBJECT type t, cut of
// spaces beyond its maximum length.
func fitLOB(s string, t CLOBType) (string, *Error) {
	if countUnits(s, t.Units) <= t.Length {
		return s, nil
	}

	kept := prefixUnits(s, t.Length, t.Units)
	if strings.Trim(s[len(kept):], space) == "" {
		return kept, nil
	}

	written := writtenType("CHARACTER LARGE OBJECT", t.Length, t.Units)

	return "", truncated("a large object longer than "+written+", not padding beyond it, does not fit it", assignLOB,
		written)
}

// truncated is the failure of a string too long for the type written,
// as turborel's assign gives it.
func truncated(message, rule, written string) *Error {
	return &Error{
		SQLCode: sqlcodeData, SQLState: sqlstateRightTruncation, Message: message, Rule: rule,
		Diagnostics: &Diagnostics{Severity: severityError, DataType: written},
	}
}

// severityError is the severity of a failure that does not end the
// connection.
const severityError = "ERROR"

// writtenType gives a character string type as SQL writes it, as
// CHARACTER(5) or CHARACTER(5 OCTETS).
func writtenType(name string, length uint64, units CharUnits) string {
	if units == Octets {
		return name + "(" + strconv.FormatUint(length, decimalRadix) + " OCTETS)"
	}

	return name + "(" + strconv.FormatUint(length, decimalRadix) + ")"
}
