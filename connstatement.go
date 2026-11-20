package turborel

// The connection statements an SQL-agent runs itself [ISO/IEC 9075:1992
// §15], read as turborel's parser reads them:
//
//	<connect statement>    ::= CONNECT TO { DEFAULT | <SQL-server name> [ AS <connection name> ] [ USER <user name> ] }
//	<set connection statement> ::= SET CONNECTION { DEFAULT | <connection name> }
//	<disconnect statement> ::= DISCONNECT { DEFAULT | ALL | CURRENT | <connection name> }
//
// each name a <simple value specification>, which a client takes only as
// a character string literal. A statement is one if its first word is
// CONNECT or DISCONNECT, or its first two SET CONNECTION.

import "strings"

// The kinds of connection statement.
type statementKind uint8

const (
	statementConnect statementKind = iota
	statementConnectDefault
	statementSetConnection
	statementDisconnect
)

// What a DISCONNECT names.
type disconnectObject uint8

const (
	disconnectNamed disconnectObject = iota
	disconnectDefault
	disconnectAll
	disconnectCurrent
)

// literal is a simple value specification: a literal's text, and whether
// it is a character string literal.
type literal struct {
	text      string
	character bool
}

// connectionStatement is a connection statement read.
type connectionStatement struct {
	kind   statementKind
	server *literal
	name   *literal
	user   *literal
	object disconnectObject
}

// The kinds of a connection statement's tokens.
type tokenKind uint8

const (
	tokenWord tokenKind = iota
	tokenString
	tokenNumber
	tokenOther
	tokenEnd
)

// token is a connection statement's token, and where it begins.
type token struct {
	kind tokenKind
	text string
	at   int
}

// describeToken gives a token as turborel's parser names it.
func describeToken(t token) string {
	switch t.kind {
	case tokenString:
		return "'" + t.text + "'"
	case tokenEnd:
		return "end of statement"
	case tokenOther:
		return `"` + t.text + `"`
	case tokenWord, tokenNumber:
	}

	return t.text
}

// parseConnectionStatement reads sql as a connection statement: false if
// it is another statement.
func parseConnectionStatement(sql string) (connectionStatement, bool, error) {
	s := []rune(sql)

	first, at := leadingWord(s, 0)
	second, _ := leadingWord(s, at)

	if first != "CONNECT" && first != "DISCONNECT" && (first != "SET" || second != "CONNECTION") {
		return connectionStatement{}, false, nil
	}

	tokens, err := connectionTokens(s)
	if err != nil {
		return connectionStatement{}, false, err
	}

	p := &statementParser{tokens: tokens}

	statement, err := p.statement()

	return statement, err == nil, err
}

// leadingWord gives the word, in upper case, that begins s at at, after
// separators, and where it ends; empty if none.
func leadingWord(s []rune, at int) (string, int) {
	for at < len(s) {
		next, skipped, ok := skipSeparator(s, at)
		if !ok || !skipped {
			break
		}

		at = next
	}

	end := at
	for end < len(s) && identifierPart(s[end]) {
		end++
	}

	if end == at || !identifierStart(s[at]) {
		return "", at
	}

	return upper(string(s[at:end])), end
}

// connectionTokens splits a connection statement into its tokens, ended
// by tokenEnd.
func connectionTokens(s []rune) ([]token, error) {
	var tokens []token

	for at := 0; at < len(s); {
		next, skipped, ok := skipSeparator(s, at)
		if !ok {
			return nil, lexical(at, "a <comment> is not closed")
		}

		if skipped {
			at = next

			continue
		}

		t, end, err := connectionToken(s, at)
		if err != nil {
			return nil, err
		}

		tokens = append(tokens, t)
		at = end
	}

	return append(tokens, token{kind: tokenEnd, at: len(s)}), nil
}

// connectionToken reads the token at s[at].
func connectionToken(s []rune, at int) (token, int, error) {
	c := s[at]

	switch {
	case c == '\'':
		return stringToken(s, at)
	case otherToken(s, at):
		return prefixedToken(s, at)
	case isDigit(c):
		return spanToken(s, at, at, tokenNumber, func(r rune) bool { return isDigit(r) || r == '.' })
	case identifierStart(c) || c == '"':
		w, end, ok := nameWord(s, at)
		if !ok {
			return token{}, 0, lexical(at, "an <identifier> is malformed")
		}

		return token{kind: tokenWord, text: w.text, at: at}, end, nil
	}

	return token{kind: tokenOther, text: string(c), at: at}, at + 1, nil
}

// isDigit says whether c is a decimal digit.
func isDigit(c rune) bool { return c >= '0' && c <= '9' }

// prefixedToken reads a token a letter or an underscore begins that is no
// word: a national character string literal, a binary string literal, a
// literal of no character string, or an introducer.
func prefixedToken(s []rune, at int) (token, int, error) {
	switch s[at] {
	case 'N', 'n':
		return stringToken(s, at+1)
	case 'X', 'x':
		t, end, err := stringToken(s, at+1)
		t.kind = tokenNumber

		return t, end, err
	}

	return spanToken(s, at, at+1, tokenOther, func(r rune) bool { return identifierPart(r) || r == '.' })
}

// spanToken reads a token of kind beginning at s[at], from s[from] each
// character in reaches.
func spanToken(s []rune, at, from int, kind tokenKind, in func(rune) bool) (token, int, error) {
	end := from
	for end < len(s) && in(s[end]) {
		end++
	}

	return token{kind: kind, text: string(s[at:end]), at: at}, end, nil
}

// stringToken reads a character string literal beginning at s[at], its
// parts separated by separators holding a line break joined.
func stringToken(s []rune, at int) (token, int, error) {
	var text strings.Builder

	start := at

	for {
		end, ok := quotedPart(s, at+1, &text)
		if !ok {
			return token{}, 0, lexical(start, "unterminated <character string literal>")
		}

		at = end

		next, joined := continuedString(s, at)
		if !joined {
			return token{kind: tokenString, text: text.String(), at: start}, at, nil
		}

		at = next
	}
}

// quotedPart reads a quoted part's characters from s[at] into text, a
// quote written twice once: where it ends, after its closing quote;
// false if it is not closed.
func quotedPart(s []rune, at int, text *strings.Builder) (int, bool) {
	for at < len(s) {
		if s[at] != '\'' {
			text.WriteRune(s[at])
			at++

			continue
		}

		if at+1 < len(s) && s[at+1] == '\'' {
			text.WriteRune('\'')

			at += len("''")

			continue
		}

		return at + 1, true
	}

	return 0, false
}

// continuedString says whether a character string literal goes on after
// s[at], separators holding a line break before its next quoted part
// [ISO/IEC 9075:1992 §5.3 SR4]: where that part begins.
func continuedString(s []rune, at int) (int, bool) {
	broken := false

	for at < len(s) {
		next, skipped, ok := skipSeparator(s, at)
		if !ok || !skipped {
			break
		}

		broken = broken || strings.ContainsRune(string(s[at:next]), '\n')
		at = next
	}

	return at, broken && at < len(s) && s[at] == '\''
}

// lexical is a token that cannot be read at character at.
func lexical(at int, message string) *Error {
	return &Error{
		SQLCode: sqlcodeLexical, SQLState: sqlstateSyntax, Message: message, Position: len64(at), Placed: true,
	}
}

// sqlcodeLexical is the SQLCODE of a token that cannot be read.
const sqlcodeLexical int32 = -101

// statementParser reads a connection statement's tokens.
type statementParser struct {
	tokens []token
	at     int
}

// peek gives the token at hand.
func (p *statementParser) peek() token { return p.tokens[p.at] }

// accept takes the word word, if it is at hand.
func (p *statementParser) accept(word string) bool {
	if t := p.peek(); t.kind == tokenWord && t.text == word && isReserved(word) {
		p.at = min(p.at+1, len(p.tokens)-1)

		return true
	}

	return false
}

// expect takes the word word, or fails.
func (p *statementParser) expect(word string) error {
	if p.accept(word) {
		return nil
	}

	return p.syntax("expected " + word)
}

// syntax is a statement that does not parse at the token at hand.
func (p *statementParser) syntax(message string) *Error {
	t := p.peek()

	return &Error{
		SQLCode: sqlcodeSyntax, SQLState: sqlstateSyntax, Message: message + ", found " + describeToken(t),
		Position: len64(t.at), Placed: true,
	}
}

// value reads a simple value specification: a literal.
func (p *statementParser) value() (*literal, error) {
	t := p.peek()

	if t.kind == tokenOther && strings.HasPrefix(t.text, "_") && p.tokens[p.at+1].kind == tokenString {
		p.at++
		t = p.peek()
	}

	switch t.kind {
	case tokenString, tokenNumber:
		p.at++

		return &literal{text: t.text, character: t.kind == tokenString}, nil
	case tokenWord:
		if t.text == "TRUE" || t.text == "FALSE" {
			p.at++

			return &literal{text: t.text}, nil
		}
	case tokenOther, tokenEnd:
	}

	return nil, p.syntax("expected a <literal>")
}

// statement reads a connection statement.
func (p *statementParser) statement() (connectionStatement, error) {
	switch {
	case p.accept("CONNECT"):
		return p.connect()
	case p.accept("SET"):
		if err := p.expect("CONNECTION"); err != nil {
			return connectionStatement{}, err
		}

		if p.accept("DEFAULT") {
			return connectionStatement{kind: statementSetConnection}, nil
		}

		name, err := p.value()

		return connectionStatement{kind: statementSetConnection, name: name}, err
	default:
		if err := p.expect("DISCONNECT"); err != nil {
			return connectionStatement{}, err
		}

		return p.disconnect()
	}
}

// connect reads the rest of CONNECT.
func (p *statementParser) connect() (connectionStatement, error) {
	if err := p.expect("TO"); err != nil {
		return connectionStatement{}, err
	}

	if p.accept("DEFAULT") {
		return connectionStatement{kind: statementConnectDefault}, nil
	}

	s := connectionStatement{kind: statementConnect}

	var err error
	if s.server, err = p.value(); err != nil {
		return s, err
	}

	if p.accept("AS") {
		if s.name, err = p.value(); err != nil {
			return s, err
		}
	}

	if p.accept("USER") {
		s.user, err = p.value()
	}

	return s, err
}

// disconnect reads the rest of DISCONNECT.
func (p *statementParser) disconnect() (connectionStatement, error) {
	s := connectionStatement{kind: statementDisconnect}

	switch {
	case p.accept("DEFAULT"):
		s.object = disconnectDefault
	case p.accept("ALL"):
		s.object = disconnectAll
	case p.accept("CURRENT"):
		s.object = disconnectCurrent
	default:
		var err error

		s.name, err = p.value()

		return s, err
	}

	return s, nil
}
