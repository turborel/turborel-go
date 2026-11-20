package turborel

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

// got is a field as read: its text, or nil for null.
type got = *string

// f is s as a field read.
func f(s string) got { return &s }

// readDelimited gives the fields of each record of text, written as
// dialect says.
func readDelimited(text io.Reader, dialect Dialect) ([][]got, error) {
	source := NewDelimited(text, dialect)

	var all [][]got

	for {
		record, err := source.Record()
		if err != nil {
			return nil, err
		}

		if record == nil {
			return all, nil
		}

		fields := make([]got, len(record.Values))

		for i, v := range record.Values {
			if s, ok := v.(Char); ok {
				fields[i] = f(string(s))
			}
		}

		all = append(all, fields)
	}
}

// assertFields checks the fields of each record of text.
func assertFields(t *testing.T, text string, dialect Dialect, want [][]got) {
	t.Helper()

	fields, err := readDelimited(strings.NewReader(text), dialect)
	if err != nil || !reflect.DeepEqual(fields, want) {
		t.Errorf("%q gave %v, %v; want %v", text, show(fields), err, show(want))
	}
}

// show writes fields readably.
func show(fields [][]got) string {
	var b strings.Builder

	for _, record := range fields {
		b.WriteString("[")

		for _, field := range record {
			if field == nil {
				b.WriteString("<null> ")
			} else {
				b.WriteString("\"" + *field + "\" ")
			}
		}

		b.WriteString("]")
	}

	return b.String()
}

// delimitedError gives the failure reading text gives.
func delimitedError(t *testing.T, text string, dialect Dialect) *SourceError {
	t.Helper()

	_, err := readDelimited(strings.NewReader(text), dialect)

	failure, ok := errors.AsType[*SourceError](err)
	if !ok {
		t.Fatalf("%q gave %v", text, err)
	}

	return failure
}

// TestCSVRecordsAndFields reads CSV's records and fields.
//
// Verifies: [CLIENT Importing] CSV's records end at CR LF or LF, the
// last's optional; fields split at commas, spaces kept; a field in
// quotes holds commas, line breaks and quotes written twice; an empty
// field is null, one of empty quotes the empty string; an empty line is a
// record of one null field.
func TestCSVRecordsAndFields(t *testing.T) {
	t.Parallel()

	csv := CSV()
	assertFields(t, "a,b\r\nc,d\n", csv, [][]got{{f("a"), f("b")}, {f("c"), f("d")}})
	assertFields(t, "a,b\nc,d", csv, [][]got{{f("a"), f("b")}, {f("c"), f("d")}})
	assertFields(t, " a , b ", csv, [][]got{{f(" a "), f(" b ")}})
	assertFields(t, `"a,b","say ""hi"""`, csv, [][]got{{f("a,b"), f(`say "hi"`)}})
	assertFields(t, "\"one\r\ntwo\nthree\",x\n", csv, [][]got{{f("one\r\ntwo\nthree"), f("x")}})
	assertFields(t, `,"",`, csv, [][]got{{nil, f(""), nil}})
	assertFields(t, "a\n\nb\n", csv, [][]got{{f("a")}, {nil}, {f("b")}})
	assertFields(t, `""""`, csv, [][]got{{f(`"`)}})
	assertFields(t, "", csv, nil)
	assertFields(t, "é,\U0001F600", csv, [][]got{{f("é"), f("\U0001F600")}})
}

// TestCSVNullTextAndByteOrderMark reads the text for null and passes a
// byte order mark over.
//
// Verifies: [CLIENT Importing] the text for null is null only not in
// quotes; an empty field is then the empty string. A byte order mark
// beginning the input is passed over, and nowhere else.
func TestCSVNullTextAndByteOrderMark(t *testing.T) {
	t.Parallel()

	assertFields(t, `NULL,"NULL",,null`, CSV().WithNull("NULL"), [][]got{{nil, f("NULL"), f(""), f("null")}})
	assertFields(t, "\uFEFFa,b\n\uFEFFc", CSV(), [][]got{{f("a"), f("b")}, {f("\uFEFFc")}})
}

// TestCSVHeaderAndPositions reads a header once and says where each
// record began.
//
// Verifies: [CLIENT Importing] a header names the fields, read once,
// whether or not asked for; records are numbered from the first after
// it, and each says the line it began on, past the line breaks of quoted
// fields; a header missing is refused.
func TestCSVHeaderAndPositions(t *testing.T) {
	t.Parallel()

	text := "ID,\"FULL NAME\"\n1,\"Ada\nLovelace\"\n2,Grace\n"
	source := NewDelimited(strings.NewReader(text), CSV().WithHeader())

	headings, err := source.Headings()
	if err != nil || len(headings) != 2 || headings[0] != (Heading{Name: "ID"}) ||
		headings[1] != (Heading{Name: "FULL NAME"}) {
		t.Fatalf("headings %v, %v", headings, err)
	}

	if again, err := source.Headings(); again != nil || err != nil {
		t.Errorf("headings read twice: %v, %v", again, err)
	}

	var positions []Position

	for record, err := source.Record(); record != nil || err != nil; record, err = source.Record() {
		if err != nil {
			t.Fatal(err)
		}

		positions = append(positions, record.Position)
	}

	want := []Position{{Record: 1, Line: 2, HasLine: true}, {Record: 2, Line: 4, HasLine: true}}
	if !reflect.DeepEqual(positions, want) {
		t.Errorf("positions %v", positions)
	}

	assertFields(t, text, CSV().WithHeader(), [][]got{{f("1"), f("Ada\nLovelace")}, {f("2"), f("Grace")}})

	_, err = NewDelimited(strings.NewReader(""), CSV().WithHeader()).Headings()
	if err == nil || err.Error() != "record 0, line 1: [DESIGN] there is no header" {
		t.Errorf("a header missing: %v", err)
	}

	if none, err := NewDelimited(strings.NewReader("a"), CSV()).Headings(); none != nil || err != nil {
		t.Errorf("a header not asked for: %v, %v", none, err)
	}
}

// TestCSVErrorsSayWhere refuses malformed CSV, saying where.
//
// Verifies: [CLIENT Importing] a quote in a field not quoted, anything
// but the delimiter after a closing quote, a quote left open at the end
// of the input, and a field not UTF-8 are refused, saying the record, the
// line it began on, and the field; the source then gives nothing more.
func TestCSVErrorsSayWhere(t *testing.T) {
	t.Parallel()

	cases := []struct {
		text    string
		dialect Dialect
		want    string
	}{
		{"a\nb,c\"d\n", CSV(), "record 2, line 2: [RFC 4180 §2 rule 5] field 2: a quote in a field not enclosed in quotes"},
		{
			"\"a\"b,c", CSV(),
			"record 1, line 1: [RFC 4180 §2 rule 5] field 1: a quoted field's closing quote is followed by more",
		},
		{"a\nb\n\"c\nd\ne\n", CSV(), "record 3, line 3: [RFC 4180 §2 rule 5] a quoted field is not closed"},
		{"\"c", CSV(), "record 1, line 1: [RFC 4180 §2 rule 5] a quoted field is not closed"},
		{"ok,\xFF\xFE", CSV(), "record 1, line 1: [DESIGN] field 2 is not UTF-8"},
		{"ok\t\xFF", TSV(), "record 1, line 1: [DESIGN] field 2 is not UTF-8"},
	}
	for _, c := range cases {
		if got := delimitedError(t, c.text, c.dialect).Error(); got != c.want {
			t.Errorf("%q gave %q", c.text, got)
		}
	}

	source := NewDelimited(strings.NewReader("\"a\"b\nc\n"), CSV())
	if _, err := source.Record(); err == nil {
		t.Error("a malformed record was read")
	}

	if record, err := source.Record(); record != nil || err != nil {
		t.Errorf("a record after an error: %v, %v", record, err)
	}
}

// failingReader is a reader that fails.
type failingReader struct{}

var errDiskGone = errors.New("the disk is gone")

func (failingReader) Read([]byte) (int, error) { return 0, errDiskGone }

// endless is a reader of one byte forever.
type endless byte

func (e endless) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = byte(e)
	}

	return len(b), nil
}

// TestRecordsAreBounded refuses a record longer than the bound once the
// bound is read.
//
// Verifies: [CLIENT Importing] a record longer than the bound is refused
// once the bound is read, so an endless input, or a quote never closed,
// is read in bounded memory; a record of the bound is read. The default
// bound is the protocol's longest body. A reader's failure is told.
func TestRecordsAreBounded(t *testing.T) {
	t.Parallel()

	_, err := readDelimited(endless('a'), CSV().WithMaxRecord(1024))
	failure, ok := errors.AsType[*SourceError](err)
	if !ok || failure.Message != "[DESIGN] the record is longer than 1024 bytes" {
		t.Errorf("an endless record: %v", err)
	}

	open := io.MultiReader(bytes.NewReader(bytes.Repeat([]byte{'"'}, 10000)), endless('\n'))
	if _, err := readDelimited(open, CSV().WithMaxRecord(4096)); err == nil ||
		!strings.HasSuffix(err.Error(), "the record is longer than 4096 bytes") {
		t.Errorf("a quote never closed: %v", err)
	}

	assertFields(t, "abcd\n", CSV().WithMaxRecord(5), [][]got{{f("abcd")}})

	if _, err := readDelimited(strings.NewReader("abcde\n"), CSV().WithMaxRecord(5)); err == nil {
		t.Error("a record past the bound was read")
	}

	if DefaultMaxRecord != maxBody {
		t.Errorf("the default bound is %d", DefaultMaxRecord)
	}

	if _, err := readDelimited(failingReader{}, CSV()); err == nil ||
		!strings.HasSuffix(err.Error(), "[DESIGN] cannot read: the disk is gone") {
		t.Errorf("a reader's failure: %v", err)
	}
}

// TestTabSeparatedRecords splits tab-separated text at tabs.
//
// Verifies: [CLIENT Importing] tab-separated text splits at tabs, with no
// quoting, its records at line breaks; an empty field is null.
func TestTabSeparatedRecords(t *testing.T) {
	t.Parallel()

	assertFields(t, "a\tb\r\n\"c\t\"d\"\n", TSV(), [][]got{{f("a"), f("b")}, {f(`"c`), f(`"d"`)}})
	assertFields(t, "a\t\tb,c", TSV(), [][]got{{f("a"), nil, f("b,c")}})
	assertFields(t, "a\t\\N", TSV().WithNull(`\N`), [][]got{{f("a"), nil}})
}

// TestDelimiters splits at a delimiter of the program's.
//
// Verifies: [CLIENT Importing] a delimiter is one ASCII character, not a
// quote, CR or LF; fields then split at it.
func TestDelimiters(t *testing.T) {
	t.Parallel()

	semicolon, err := CSV().WithDelimiter(';')
	if err != nil {
		t.Fatal(err)
	}

	assertFields(t, `a;"b;c";d,e`, semicolon, [][]got{{f("a"), f("b;c"), f("d,e")}})

	pipe, err := TSV().WithDelimiter('|')
	if err != nil {
		t.Fatal(err)
	}

	assertFields(t, "a|b\tc", pipe, [][]got{{f("a"), f("b\tc")}})

	for _, wrong := range []rune{'"', '\n', '\r', 'é'} {
		if _, err := CSV().WithDelimiter(wrong); err == nil ||
			!strings.HasPrefix(err.Error(), "[DESIGN] a delimiter is one ASCII character") {
			t.Errorf("%q: %v", wrong, err)
		}
	}
}
