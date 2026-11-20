package turborel

// The names an import writes into its SQL [CLIENT Importing]: a table's
// name read as SQL reads a <table name> [ISO/IEC 9075:1992 §5.4], of one
// to three <identifier>s separated by periods, each written delimited; a
// heading read as an <identifier> if it is one, a regular one in upper
// case, else taken as it is. The words are read as turborel's lexer reads
// them [ISO/IEC 9075:1992 §5.2]: a <regular identifier> of letters,
// digits and underscores, in upper case, not a <reserved word>; a
// <delimited identifier> in double quotes, "" for one, its case kept;
// separated by spaces, line breaks and comments.

import (
	"strings"
	"unicode"
)

// reservedWords are turborel's <reserved word>s, which no <regular
// identifier> is [ISO/IEC 9075:1992 §5.2 SR11].
const reservedWords = `
ABSOLUTE ACTION ADD ALL ALLOCATE ALTER AND ANY ARE AS ASC ASSERTION AT AUTHORIZATION AVG BEGIN
BETWEEN BIT BIT_LENGTH BOTH BY CASCADE CASCADED CASE CAST CATALOG CHAR CHARACTER CHAR_LENGTH
CHARACTER_LENGTH CHECK CLOSE COALESCE COLLATE COLLATION COLUMN COMMIT CONNECT CONNECTION CONSTRAINT
CONSTRAINTS CONTINUE CONVERT CORRESPONDING COUNT CREATE CROSS CURRENT CURRENT_DATE CURRENT_TIME
CURRENT_TIMESTAMP CURRENT_USER CURSOR DATE DAY DEALLOCATE DEC DECIMAL DECLARE DEFAULT DEFERRABLE
DEFERRED DELETE DESC DESCRIBE DESCRIPTOR DIAGNOSTICS DISCONNECT DISTINCT DOMAIN DOUBLE DROP ELSE END
ESCAPE EXCEPT EXCEPTION EXEC EXECUTE EXISTS EXTERNAL EXTRACT FALSE FETCH FIRST FLOAT FOR FOREIGN
FOUND FROM FULL GET GLOBAL GO GOTO GRANT GROUP HAVING HOUR IDENTITY IMMEDIATE IN INDICATOR INITIALLY
INNER INPUT INSENSITIVE INSERT INT INTEGER INTERSECT INTERVAL INTO IS ISOLATION JOIN KEY LANGUAGE
LAST LEADING LEFT LEVEL LIKE LOCAL LOWER MATCH MAX MIN MINUTE MODULE MONTH NAMES NATIONAL NATURAL
NCHAR NEXT NO NOT NULL NULLIF NUMERIC OCTET_LENGTH OF ON ONLY OPEN OPTION OR ORDER OUTER OUTPUT
OVERLAPS PAD PARTIAL POSITION PRECISION PREPARE PRESERVE PRIMARY PRIOR PRIVILEGES PROCEDURE PUBLIC
READ REAL REFERENCES RELATIVE RESTRICT REVOKE RIGHT ROLLBACK ROWS SCHEMA SCROLL SECOND SECTION
SELECT SESSION SESSION_USER SET SIZE SMALLINT SOME SPACE SQL SQLCODE SQLERROR SQLSTATE SUBSTRING SUM
SYSTEM_USER TABLE TEMPORARY THEN TIME TIMESTAMP TIMEZONE_HOUR TIMEZONE_MINUTE TO TRAILING
TRANSACTION TRANSLATE TRANSLATION TRIM TRUE UNION UNIQUE UNKNOWN UPDATE UPPER USAGE USER USING VALUE
VALUES VARCHAR VARYING VIEW WHEN WHENEVER WHERE WITH WORK WRITE YEAR ZONE
`

// pseudoColumns are the names of node and edge tables' pseudo-columns,
// each $ then a name, which turborel reads as one identifier.
const pseudoColumns = " $NODE_ID $EDGE_ID $FROM_ID $TO_ID "

// The bounds of a name.
const (
	// maxIdentifierLength is the most characters of an <identifier>
	// [ISO/IEC 9075:1992 §5.2 SR8, SR9].
	maxIdentifierLength = 128
	// maxNameParts is the most <identifier>s of a table's name: catalog,
	// schema and table.
	maxNameParts = 3
)

// The kinds of the words of a name.
type wordKind uint8

const (
	wordIdentifier wordKind = iota
	wordReserved
	wordPeriod
)

// word is one <token> of a name: an identifier, its text as SQL reads
// it; a reserved word, in upper case; or a period.
type word struct {
	kind wordKind
	text string
}

// isReserved says whether upper, in upper case, is a reserved word.
func isReserved(upper string) bool {
	return strings.Contains(strings.ReplaceAll(reservedWords, "\n", " ")+" ", " "+upper+" ")
}

// identifierStart says whether c may begin a <regular identifier>: a
// letter, as Unicode's Alphabetic property has it.
func identifierStart(c rune) bool {
	return unicode.IsLetter(c) || unicode.Is(unicode.Nl, c) || unicode.Is(unicode.Other_Alphabetic, c)
}

// identifierPart says whether c may follow in a <regular identifier>.
func identifierPart(c rune) bool {
	return identifierStart(c) || c >= '0' && c <= '9' || c == '_'
}

// upper gives s in upper case, ß as SS as Unicode's full mapping has it.
func upper(s string) string {
	return strings.ToUpper(strings.ReplaceAll(s, "ß", "SS"))
}

// nameWords splits text into its words; false if a <token> of it is no
// word of a name, or it does not lex.
func nameWords(text string) ([]word, bool) {
	s := []rune(text)

	var words []word

	for at := 0; at < len(s); {
		next, skipped, ok := skipSeparator(s, at)
		if !ok {
			return nil, false
		}

		if skipped {
			at = next

			continue
		}

		w, next, ok := nameWord(s, at)
		if !ok {
			return nil, false
		}

		words = append(words, w)
		at = next
	}

	return words, true
}

// skipSeparator skips a space, a line break or a comment at s[at]: where it
// ends, whether there was one, and false if a comment is not closed.
func skipSeparator(s []rune, at int) (int, bool, bool) {
	switch {
	case s[at] == ' ' || s[at] == '\n':
		return at + 1, true, true
	case s[at] == '\r' && at+1 < len(s) && s[at+1] == '\n':
		return at + len("\r\n"), true, true
	case s[at] == '-' && at+1 < len(s) && s[at+1] == '-':
		for i := at; i < len(s); i++ {
			if s[i] == '\n' {
				return i + 1, true, true
			}
		}

		return 0, false, false
	case s[at] == '/' && at+1 < len(s) && s[at+1] == '*':
		return bracketed(s, at)
	default:
		return at, false, true
	}
}

// bracketed skips a bracketed comment at s[at], nested ones within it.
func bracketed(s []rune, at int) (int, bool, bool) {
	depth := 0

	for i := at; i+1 < len(s); i++ {
		switch {
		case s[i] == '/' && s[i+1] == '*':
			depth++
			i++
		case s[i] == '*' && s[i+1] == '/':
			depth--
			i++

			if depth == 0 {
				return i + 1, true, true
			}
		}
	}

	return 0, false, false
}

// nameWord reads the word at s[at]: it, and where it ends.
func nameWord(s []rune, at int) (word, int, bool) {
	c := s[at]

	switch {
	case c == '$':
		return pseudoWord(s, at)
	case otherToken(s, at):
		return word{}, 0, false
	case identifierStart(c):
		return regularWord(s, at)
	case c == '"':
		return delimitedWord(s, at)
	case c == '.' && (at+1 >= len(s) || s[at+1] < '0' || s[at+1] > '9'):
		return word{kind: wordPeriod, text: "."}, at + 1, true
	default:
		return word{}, 0, false
	}
}

// pseudoWord reads the name of a pseudo-column at s[at], $ then its name.
func pseudoWord(s []rune, at int) (word, int, bool) {
	end := at + 1
	for end < len(s) && identifierPart(s[end]) {
		end++
	}

	name := "$" + upper(string(s[at+1:end]))

	return word{text: name}, end, strings.Contains(pseudoColumns, " "+name+" ")
}

// otherToken says whether a <token> at s[at] that a letter or an
// underscore begins is no word of a name: a national or binary string
// literal, or an introducer.
func otherToken(s []rune, at int) bool {
	if at+1 >= len(s) {
		return false
	}

	switch s[at] {
	case 'N', 'n', 'X', 'x':
		return s[at+1] == '\''
	case '_':
		return unicode.IsLetter(s[at+1])
	default:
		return false
	}
}

// regularWord reads a <regular identifier> or a reserved word at s[at].
func regularWord(s []rune, at int) (word, int, bool) {
	end, length := at, 0

	for end < len(s) && identifierPart(s[end]) {
		if s[end] != '_' {
			length++
		}

		end++
	}

	text := upper(string(s[at:end]))
	if isReserved(text) {
		return word{kind: wordReserved, text: text}, end, true
	}

	return word{text: text}, end, length <= maxIdentifierLength
}

// delimitedWord reads a <delimited identifier> at s[at].
func delimitedWord(s []rune, at int) (word, int, bool) {
	var body []rune

	for i := at + 1; i < len(s); i++ {
		switch {
		case s[i] == '\n' || s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n':
			return word{}, 0, false
		case s[i] != '"':
			body = append(body, s[i])
		case i+1 < len(s) && s[i+1] == '"':
			body = append(body, '"')
			i++
		default:
			ok := len(body) > 0 && len(body) <= maxIdentifierLength

			return word{text: string(body)}, i + 1, ok
		}
	}

	return word{}, 0, false
}

// delimited gives name as a <delimited identifier>, a double quote in it
// twice.
func delimited(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// tableName gives text, a table's name, each of its identifiers
// delimited; false if it is not a <table name>.
func tableName(text string) (string, bool) {
	words, ok := nameWords(text)
	if !ok || len(words) == 0 {
		return "", false
	}

	var parts []string

	for i, w := range words {
		want := wordIdentifier
		if i%2 == 1 {
			want = wordPeriod
		}

		if w.kind != want {
			return "", false
		}

		if w.kind == wordIdentifier {
			parts = append(parts, delimited(w.text))
		}
	}

	if len(words)%2 == 0 || len(parts) > maxNameParts {
		return "", false
	}

	return strings.Join(parts, "."), true
}

// columnName gives the name of a column a heading names: an identifier
// as SQL reads it, a reserved word in upper case, else the heading as it
// is.
func columnName(heading string) string {
	words, ok := nameWords(heading)
	if !ok || len(words) != 1 || words[0].kind == wordPeriod {
		return heading
	}

	return words[0].text
}
