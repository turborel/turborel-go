package turborel

import (
	"strings"
	"testing"
)

// TestTableNames reads a table's name as SQL reads a <table name>.
//
// Verifies: [CLIENT Importing] an import's table is named as SQL names
// it: one to three identifiers separated by periods, a regular one in
// upper case, a delimited one as it is, spaces, line breaks and comments
// between them; each written delimited; a reserved word, a literal, or
// more than three parts is no table's name.
func TestTableNames(t *testing.T) {
	t.Parallel()

	good := []struct{ text, want string }{
		{"staff", `"STAFF"`},
		{`ann . "Staff ""A"""`, `"ANN"."Staff ""A"""`},
		{"c.s.t", `"C"."S"."T"`},
		{"s/* a /* nested */ comment */.t -- the table\n", `"S"."T"`},
		{"s\r\n.t", `"S"."T"`},
		{"straße", `"STRASSE"`},
		{"$node_id", `"$NODE_ID"`},
		{"t_1", `"T_1"`},
		{strings.Repeat("a", maxIdentifierLength) + "_", `"` + strings.Repeat("A", maxIdentifierLength) + `_"`},
	}
	for _, c := range good {
		if got, ok := tableName(c.text); !ok || got != c.want {
			t.Errorf("%q gave %q, %v", c.text, got, ok)
		}
	}

	bad := []string{
		"", "select", "a.b.c.d", "a..b", "a.", ".a", "a b", "a.1", "'a'", "N'a'", "x'00'", "_utf8", `""`,
		"\"a\nb\"", `"a`, "a -- no line break", "/* open", "a\tb", "$other", "1a",
		strings.Repeat("a", maxIdentifierLength+1), `"` + strings.Repeat("a", maxIdentifierLength+1) + `"`,
	}
	for _, text := range bad {
		if got, ok := tableName(text); ok {
			t.Errorf("%q gave %q", text, got)
		}
	}
}

// TestColumnNames reads a heading as a column's name.
//
// Verifies: [CLIENT Importing] a heading that is one identifier names
// its column as SQL reads it, a regular one in upper case; a reserved
// word in upper case; any other heading as it is.
func TestColumnNames(t *testing.T) {
	t.Parallel()

	cases := []struct{ heading, want string }{
		{"id", "ID"},
		{`"Full Name"`, "Full Name"},
		{"select", "SELECT"},
		{"first name", "first name"},
		{"x-y", "x-y"},
		{"a.b", "a.b"},
		{".", "."},
		{"1st", "1st"},
		{"", ""},
	}
	for _, c := range cases {
		if got := columnName(c.heading); got != c.want {
			t.Errorf("%q gave %q", c.heading, got)
		}
	}
}
