package turborel

// Delimited text as a Source [CLIENT Importing]: CSV as RFC 4180 §2
// defines it, and tab-separated text, each a record a line, fields split
// at a delimiter. Read a line at a time, a record at most MaxRecord
// bytes, so a file of any size is read in bounded memory, and a quote left
// open is refused at the bound rather than read to the end of the file.
//
// CSV [RFC 4180 §2]: a record ends at a line break, CR LF or LF alone,
// the last one's optional (rules 1, 2); a header record may come first
// (rule 3); a field may be enclosed in double quotes, and must be to hold
// the delimiter, a quote or a line break, a quote in it written twice
// (rules 5-7). A quote in a field not enclosed, or anything but the
// delimiter after a closing quote, is refused. Tab-separated text has no
// quoting: a field holds no tab and no line break.
//
// A field written as the dialect's text for null, not enclosed in quotes,
// is null: by default an empty one, so ,, is a null, and ,"", the empty
// string. Text is UTF-8; a byte order mark beginning the input is passed
// over.

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

// DefaultMaxRecord is the longest record, in bytes, a Delimited reads
// unless told otherwise.
const DefaultMaxRecord = 16 * 1024 * 1024

// The bytes delimited text is read by.
const (
	comma     = ','
	tab       = '\t'
	quoteByte = '"'
	lf        = '\n'
	cr        = '\r'
	maxASCII  = 0x7F
)

// byteOrderMark is UTF-8's byte order mark.
func byteOrderMark() []byte { return []byte{0xEF, 0xBB, 0xBF} }

// Dialect says how delimited text is written: its delimiter, whether a
// field may be quoted, whether a header record comes first, its text for
// null, and its longest record.
type Dialect struct {
	delimiter byte
	quoted    bool
	header    bool
	null      string
	maxRecord int
}

// CSV is the dialect of CSV: comma-separated, quoted, no header, an empty
// field null.
func CSV() Dialect {
	return Dialect{delimiter: comma, quoted: true, maxRecord: DefaultMaxRecord}
}

// TSV is the dialect of tab-separated text: no quoting, no header, an
// empty field null.
func TSV() Dialect {
	d := CSV()
	d.delimiter, d.quoted = tab, false

	return d
}

// WithDelimiter gives the dialect split at delimiter, one ASCII character
// other than a quote or a line break.
func (d Dialect) WithDelimiter(delimiter rune) (Dialect, error) {
	if delimiter > maxASCII || delimiter == quoteByte || delimiter == cr || delimiter == lf {
		return d, misused(fmt.Sprintf("a delimiter is one ASCII character, not a quote or a line break: %q", delimiter))
	}

	d.delimiter = byte(delimiter) //nolint:gosec // G115: checked above to be ASCII.

	return d, nil
}

// WithHeader gives the dialect with a header record first, which names
// the fields.
func (d Dialect) WithHeader() Dialect {
	d.header = true

	return d
}

// WithNull gives the dialect whose text for null is null.
func (d Dialect) WithNull(null string) Dialect {
	d.null = null

	return d
}

// WithMaxRecord gives the dialect whose longest record is bytes long.
func (d Dialect) WithMaxRecord(bytes int) Dialect {
	d.maxRecord = bytes

	return d
}

// scan is where a scan of a record's bytes is, to find whether a line
// break ends the record or lies within a quoted field.
type scan uint8

const (
	scanFieldStart scan = iota
	scanPlain
	scanQuoted
	scanQuoteInQuoted
)

// next gives the scan after b.
func (s scan) next(b, delimiter byte) scan {
	isQuote, split := b == quoteByte, b == delimiter

	switch s {
	case scanQuoted:
		if isQuote {
			return scanQuoteInQuoted
		}

		return scanQuoted
	case scanPlain:
		if split {
			return scanFieldStart
		}

		return scanPlain
	case scanFieldStart, scanQuoteInQuoted:
	}

	switch {
	case split:
		return scanFieldStart
	case isQuote:
		return scanQuoted
	default:
		return scanPlain
	}
}

// field is a field read: its text, and whether it was enclosed in quotes.
type field struct {
	text   string
	quoted bool
}

// Delimited reads delimited text as a [Source], a record at a time.
type Delimited struct {
	input         *bufio.Reader
	dialect       Dialect
	bytes         []byte
	lines         uint64
	records       uint64
	headerPending bool
	started       bool
	done          bool
}

// NewDelimited reads input as dialect says.
func NewDelimited(input io.Reader, dialect Dialect) *Delimited {
	return &Delimited{input: bufio.NewReader(input), dialect: dialect, headerPending: dialect.header}
}

// Headings gives the header record's names, of a dialect with a header;
// nil otherwise.
func (d *Delimited) Headings() ([]Heading, error) {
	return d.header()
}

// Record gives the next record, each field a [Char], or [Null] if it is
// the dialect's text for null and not enclosed in quotes; nil at the end.
func (d *Delimited) Record() (*Record, error) {
	if _, err := d.header(); err != nil {
		return nil, err
	}

	if d.done {
		return nil, nil //nolint:nilnil // The end of the source is no record and no failure.
	}

	number := d.records + 1

	fields, line, err := d.fields(number)
	if err != nil || fields == nil {
		return nil, err
	}

	d.records = number

	values := make([]Value, len(fields))
	for i, f := range fields {
		if !f.quoted && f.text == d.dialect.null {
			values[i] = Null{}
		} else {
			values[i] = Char(f.text)
		}
	}

	return &Record{Values: values, Position: Position{Record: number, Line: line, HasLine: true}}, nil
}

// fields reads the next record's fields, the record numbered record: and
// the line it began on; nil at the end.
func (d *Delimited) fields(record uint64) ([]field, uint64, error) {
	line := d.lines + 1
	position := Position{Record: record, Line: line, HasLine: true}

	more, message := d.raw()
	if message == "" && more {
		fields, why := split(d.bytes, d.dialect)
		if why == "" {
			return fields, line, nil
		}

		message = why
	}

	d.done = true

	if message != "" {
		return nil, 0, &SourceError{Position: position, Message: message}
	}

	return nil, 0, nil
}

// raw reads a record's bytes, a line at a time until no quoted field is
// open: whether there was one, or why it cannot be read.
func (d *Delimited) raw() (bool, string) {
	d.bytes = d.bytes[:0]
	at := scanFieldStart

	for {
		start := len(d.bytes)

		line, err := d.line(d.dialect.maxRecord - len(d.bytes) + 1)
		if err != nil {
			return false, "[DESIGN] cannot read: " + err.Error()
		}

		d.bytes = append(d.bytes, line...)
		if len(d.bytes) > d.dialect.maxRecord {
			return false, fmt.Sprintf("[DESIGN] the record is longer than %d bytes", d.dialect.maxRecord)
		}

		if len(line) == 0 {
			if at == scanQuoted {
				return false, unclosedQuote
			}

			return len(d.bytes) > 0, ""
		}

		at = d.scanned(at, d.bytes[start:])
		d.lines++

		// A line break in a quoted field does not end the record.
		if at != scanQuoted {
			d.ended()

			return true, ""
		}
	}
}

// scanned gives the scan after line, of a quoted dialect; of another,
// no field is quoted, so a line break always ends the record.
func (d *Delimited) scanned(at scan, line []byte) scan {
	if !d.dialect.quoted {
		return at
	}

	for _, b := range line {
		at = at.next(b, d.dialect.delimiter)
	}

	return at
}

// line reads up to and through the next LF, at most room bytes.
func (d *Delimited) line(room int) ([]byte, error) {
	var line []byte

	for len(line) < room {
		b, err := d.input.ReadByte()
		if errors.Is(err, io.EOF) {
			return line, nil
		}

		if err != nil {
			return nil, err //nolint:wrapcheck // Its text is given in the source's failure, as the reference gives it.
		}

		line = append(line, b)
		if b == lf {
			break
		}
	}

	return line, nil
}

// ended takes the record's line break off, and a byte order mark
// beginning the input.
func (d *Delimited) ended() {
	if n := len(d.bytes); n > 0 && d.bytes[n-1] == lf {
		d.bytes = d.bytes[:n-1]
		if n := len(d.bytes); n > 0 && d.bytes[n-1] == cr {
			d.bytes = d.bytes[:n-1]
		}
	}

	if !d.started && bytes.HasPrefix(d.bytes, byteOrderMark()) {
		d.bytes = d.bytes[len(byteOrderMark()):]
	}

	d.started = true
}

// header reads the header record, if one is pending.
func (d *Delimited) header() ([]Heading, error) {
	if !d.headerPending {
		return nil, nil
	}

	d.headerPending = false

	fields, _, err := d.fields(0)
	if err != nil {
		return nil, err
	}

	if fields == nil {
		return nil, &SourceError{Position: Position{Line: 1, HasLine: true}, Message: "[DESIGN] there is no header"}
	}

	headings := make([]Heading, len(fields))
	for i, f := range fields {
		headings[i] = Heading{Name: f.text}
	}

	return headings, nil
}

// unclosedQuote is the message of a quoted field not closed.
const unclosedQuote = "[RFC 4180 §2 rule 5] a quoted field is not closed"

// split splits a record's bytes into its fields; or says why it cannot.
func split(record []byte, dialect Dialect) ([]field, string) {
	if !dialect.quoted {
		return splitPlain(record, dialect.delimiter)
	}

	var fields []field

	for at := 0; ; at++ {
		f, end, why := readField(record, at, dialect.delimiter, len(fields))
		if why != "" {
			return nil, why
		}

		fields = append(fields, f)
		at = end

		if at >= len(record) {
			return fields, ""
		}

		if record[at] != dialect.delimiter {
			return nil, fmt.Sprintf("[RFC 4180 §2 rule 5] field %d: a quoted field's closing quote is followed by more",
				len(fields))
		}
	}
}

// splitPlain splits a record of a dialect with no quoting at each
// delimiter.
func splitPlain(record []byte, delimiter byte) ([]field, string) {
	parts := bytes.Split(record, []byte{delimiter})
	fields := make([]field, len(parts))

	for n, part := range parts {
		text, why := fieldText(part, n)
		if why != "" {
			return nil, why
		}

		fields[n] = field{text: text}
	}

	return fields, ""
}

// readField reads field n from record[at], enclosed in quotes or not:
// it, and where it ends.
func readField(record []byte, at int, delimiter byte, n int) (field, int, string) {
	isQuoted := at < len(record) && record[at] == quoteByte

	var (
		written []byte
		why     string
	)

	if isQuoted {
		written, at, why = enclosed(record, at+1, n)
	} else {
		written, at, why = plain(record, at, delimiter, n)
	}

	if why != "" {
		return field{}, 0, why
	}

	text, why := fieldText(written, n)

	return field{text: text, quoted: isQuoted}, at, why
}

// plain reads a field not enclosed in quotes from record[at], up to the
// delimiter: its bytes, and where it ends.
func plain(record []byte, at int, delimiter byte, n int) ([]byte, int, string) {
	start := at
	for at < len(record) && record[at] != delimiter {
		if record[at] == quoteByte {
			return nil, 0, fmt.Sprintf("[RFC 4180 §2 rule 5] field %d: a quote in a field not enclosed in quotes", n+1)
		}

		at++
	}

	return record[start:at], at, ""
}

// enclosed reads a field enclosed in quotes from record[at], after its
// opening quote: its bytes, a quote written twice once, and where it ends.
func enclosed(record []byte, at, n int) ([]byte, int, string) {
	var written []byte

	for at < len(record) {
		b := record[at]
		at++

		if b == quoteByte {
			if at >= len(record) || record[at] != quoteByte {
				return written, at, ""
			}

			at++
		}

		written = append(written, b)
	}

	return nil, 0, fmt.Sprintf("field %d: %s", n+1, unclosedQuote)
}

// fieldText gives a field's bytes as text; or says field n is not UTF-8.
func fieldText(b []byte, n int) (string, string) {
	if !utf8.Valid(b) {
		return "", fmt.Sprintf("[DESIGN] field %d is not UTF-8", n+1)
	}

	return string(b), ""
}
