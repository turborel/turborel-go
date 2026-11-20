package turborel

import (
	"reflect"
	"testing"
)

// TestStringAssignment assigns strings to character string types as
// INSERT does.
//
// Verifies: [CLIENT Importing] a string for a character string column is
// assigned to it as INSERT assigns: to CHARACTER padded with spaces, or
// cut of spaces beyond its length; to CHARACTER VARYING kept, or cut of
// spaces beyond; to CHARACTER LARGE OBJECT likewise, as a large object
// held whole; in octets of a length in octets, whole characters; another
// value, or a string for another type, as it is.
func TestStringAssignment(t *testing.T) {
	t.Parallel()

	good := []struct {
		s    string
		t    DataType
		want Value
	}{
		{"ab", CharacterType{Length: 4}, Char("ab  ")},
		{"abc   ", CharacterType{Length: 4}, Char("abc ")},
		{"éé", CharacterType{Length: 5, Units: Octets}, Char("éé ")},
		{"ab", VaryingType{Length: 4}, Char("ab")},
		{"ab  ", VaryingType{Length: 3}, Char("ab ")},
		{"é  ", VaryingType{Length: 3, Units: Octets}, Char("é ")},
		{"ab  ", CLOBType{Length: 2}, LOB{Kind: CharacterLOB, Octets: []byte("ab")}},
		{"ab", CLOBType{Length: 9}, LOB{Kind: CharacterLOB, Octets: []byte("ab")}},
		{"7", ExactType{Precision: 1}, Char("7")},
	}
	for _, c := range good {
		if got, err := assignString(c.s, c.t); err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q to %#v gave %#v, %v", c.s, c.t, got, err)
		}
	}
}

// TestStringTooLong refuses a string too long for its type.
//
// Verifies: [CLIENT Importing] a string of more than spaces beyond its
// character string type's length is refused (SQLSTATE 22001), its
// message and rule as turborel's assign gives them, its data type told.
func TestStringTooLong(t *testing.T) {
	t.Parallel()

	bad := []struct {
		s       string
		t       DataType
		message string
		rule    string
		written string
	}{
		{"abcde", CharacterType{Length: 4}, "a value of 5 characters, not spaces beyond 4, does not fit CHARACTER(4)",
			"ISO/IEC 9075:1992 §13.8 GR4b iii, §9.2 GR3b ii", "CHARACTER(4)"},
		{"ééx", VaryingType{Length: 4, Units: Octets},
			"a value of 5 octets, not spaces beyond 4, does not fit CHARACTER VARYING(4 OCTETS)",
			"ISO/IEC 9075:1992 §13.8 GR4b iii, §9.2 GR3e ii", "CHARACTER VARYING(4 OCTETS)"},
		{"abc", CLOBType{Length: 2},
			"a large object longer than CHARACTER LARGE OBJECT(2), not padding beyond it, does not fit it",
			"ISO/IEC 9075:1992 §13.8 GR4b iii; ISO/IEC 9075-2:2023 §9.2 GR3e ii", "CHARACTER LARGE OBJECT(2)"},
	}
	for _, c := range bad {
		_, err := assignString(c.s, c.t)
		if err == nil || err.SQLCode != sqlcodeData || err.SQLState != sqlstateRightTruncation || err.Message != c.message ||
			err.Rule != c.rule || err.Diagnostics.DataType != c.written || err.Diagnostics.Severity != severityError {
			t.Errorf("%q to %#v gave %#v", c.s, c.t, err)
		}
	}

	values, err := assigned([]Value{Char("a"), DecimalOf(1, 0), Char("b")},
		[]Parameter{{Type: CharacterType{Length: 2}}, {Type: ExactType{Precision: 1}}})
	if err != nil || !reflect.DeepEqual(values, []Value{Char("a "), DecimalOf(1, 0), Char("b")}) {
		t.Errorf("assigned gave %#v, %v", values, err)
	}

	if _, err := assigned([]Value{Char("abc")}, []Parameter{{Type: CharacterType{Length: 2}}}); err == nil {
		t.Error("a string too long was assigned")
	}
}
