package turborel

// An SQL-agent [CLIENT The SQL-agent]: a client's connections to servers,
// each named, one current, and the statements that make, choose and end
// them, CONNECT, SET CONNECTION and DISCONNECT [ISO/IEC 9075:1992 §15],
// which it runs itself; every other statement goes to the current
// connection's session. A Connector makes a connection to a server an
// address names, as the user a statement names, proved as the program
// says.

import (
	"context"
	"slices"
	"strings"
)

// The SQLSTATEs of an agent's connection statements.
const (
	sqlstateSuccess                 = "00000"
	sqlstateDisconnectError         = "01002"
	sqlstateNameInUse               = "08002"
	sqlstateConnectionRejected      = "08004"
	sqlstateMultipleServerTxn       = "0A001"
	sqlstateInvalidConnectionName   = "2E000"
	sqlstateInvalidTransactionState = "25000"
	sqlstateSyntax                  = "42000"
)

// The SQLCODEs of an agent's failures, as turborel's codes give them.
const (
	sqlcodeSyntax           int32 = -102
	sqlcodeSyntaxRule       int32 = -103
	sqlcodePrivilege        int32 = -104
	sqlcodeTransactionState int32 = -917
)

// Connector makes an agent's connections.
type Connector interface {
	// Connect makes a connection to the server named, an address,
	// or for nil the default server; as user, or for nil the user the
	// connector takes.
	Connect(ctx context.Context, server, user *string) (*Conn, error)
}

// ConnectorFunc is a function that makes an agent's connections, as a
// Connector.
type ConnectorFunc func(ctx context.Context, server, user *string) (*Conn, error)

// Connect calls f.
func (f ConnectorFunc) Connect(ctx context.Context, server, user *string) (*Conn, error) {
	return f(ctx, server, user)
}

// held is a connection of an agent: its name, nil for the default
// connection.
type held struct {
	name *string
	conn *Conn
}

// Agent is an SQL-agent's connections.
type Agent struct {
	connector Connector
	held      []held
	current   int
}

// NewAgent gives an agent with no connection.
func NewAgent(connector Connector) *Agent {
	return &Agent{connector: connector, current: -1}
}

// ConnectedAgent gives an agent whose default connection, current, is
// conn: as a client begins, connected.
func ConnectedAgent(connector Connector, conn *Conn) *Agent {
	return &Agent{connector: connector, held: []held{{conn: conn}}, current: 0}
}

// exception is a connection exception of sqlstate.
func exception(sqlstate, message, rule string) *Error {
	code := sqlcodeConnection

	switch sqlstate[:2] {
	case classAuthorization:
		code = sqlcodePrivilege
	case "0A":
		code = sqlcodeBeyond
	}

	return &Error{SQLCode: code, SQLState: sqlstate, Message: message, Rule: rule}
}

// Current gives the current connection. It fails with SQLSTATE 08003 if
// there is none.
func (a *Agent) Current() (*Conn, error) {
	if a.current < 0 || a.current >= len(a.held) {
		return nil, exception(sqlstateConnectionNotExist, "there is no current connection", "ISO/IEC 9075:1992 §15.3 GR9")
	}

	return a.held[a.current].conn, nil
}

// CurrentName gives the current connection's name; false for the default
// one or for no connection.
func (a *Agent) CurrentName() (string, bool) {
	if a.current < 0 || a.held[a.current].name == nil {
		return "", false
	}

	return *a.held[a.current].name, true
}

// IsLost says whether the current connection is lost: none is not.
func (a *Agent) IsLost() bool {
	c, err := a.Current()

	return err == nil && c.IsLost()
}

// Query runs sql: a connection statement itself, its completion; any
// other in the current connection's session, as Conn.Query.
func (a *Agent) Query(ctx context.Context, sql string) (*Outcome, error) {
	done, ok, err := a.ConnectionStatement(ctx, sql)
	if err != nil {
		return nil, err
	}

	if ok {
		return &Outcome{Completion: *done}, nil
	}

	c, err := a.Current()
	if err != nil {
		return nil, err
	}

	return c.Query(ctx, sql)
}

// QueryInto runs sql as Query does, a query's rows given to sink, as
// Conn.QueryInto.
func (a *Agent) QueryInto(ctx context.Context, sql string, sink Sink) (*Streamed, error) {
	done, ok, err := a.ConnectionStatement(ctx, sql)
	if err != nil {
		return nil, err
	}

	if ok {
		return &Streamed{Completion: *done}, nil
	}

	c, err := a.Current()
	if err != nil {
		return nil, err
	}

	return c.QueryInto(ctx, sql, sink)
}

// ConnectionStatement runs sql if it is a connection statement: its
// completion, and true; false if it is another.
func (a *Agent) ConnectionStatement(ctx context.Context, sql string) (*Completion, bool, error) {
	statement, ok, err := parseConnectionStatement(sql)
	if err != nil || !ok {
		return nil, false, err
	}

	var done *Completion

	switch statement.kind {
	case statementConnect:
		done, err = a.connect(ctx, statement.server, statement.name, statement.user)
	case statementConnectDefault:
		done, err = a.connect(ctx, nil, nil, nil)
	case statementSetConnection:
		done, err = a.setConnection(statement.name)
	case statementDisconnect:
		done, err = a.disconnect(statement)
	}

	return done, err == nil, err
}

// Terminate ends every connection, as the client ends. It fails with the
// first failure ending one; every one is ended still.
func (a *Agent) Terminate() error {
	var first error

	for _, h := range a.held {
		if err := h.conn.Terminate(); err != nil && first == nil {
			first = err
		}
	}

	a.held, a.current = nil, -1

	return first
}

// notInTransaction refuses to leave the current connection's active
// transaction: there are no transactions of more than one server.
func (a *Agent) notInTransaction() error {
	if c, err := a.Current(); err == nil && c.InTransaction() {
		return exception(sqlstateMultipleServerTxn,
			"the current connection's transaction is active, and none spans servers", "ISO/IEC 9075:1992 §15.1 GR1, §15.2 GR1")
	}

	return nil
}

// find gives the place of the connection named name, nil for the default
// one; -1 if none is held.
func (a *Agent) find(name *string) int {
	return slices.IndexFunc(a.held, func(h held) bool {
		return h.name == nil && name == nil || h.name != nil && name != nil && *h.name == *name
	})
}

// connect runs CONNECT TO server [AS name] [USER user], or for no server
// TO DEFAULT: the connection made current, the one current before
// dormant.
func (a *Agent) connect(ctx context.Context, server, name, user *literal) (*Completion, error) {
	if err := a.notInTransaction(); err != nil {
		return nil, err
	}

	userText, err := textOfLiteral(user)
	if err != nil {
		return nil, err
	}

	if userText != nil && !isIdentifier(*userText) {
		return nil, exception(sqlstateInvalidAuthorization, "'"+*userText+"' is not an <authorization identifier>",
			"ISO/IEC 9075:1992 §15.1 GR3")
	}

	address, err := textOfLiteral(server)
	if err != nil {
		return nil, err
	}

	identified, err := connectionName(address, name)
	if err != nil {
		return nil, err
	}

	if a.find(identified) >= 0 {
		what := "the default connection"
		if identified != nil {
			what = "the connection " + *identified
		}

		return nil, exception(sqlstateNameInUse, what+" is established", "ISO/IEC 9075:1992 §15.1 GR7")
	}

	c, err := a.connector.Connect(ctx, address, userText)
	if err != nil {
		failure := asError(err)

		state := sqlstateConnectionRejected
		if failure.SQLState == sqlstateConnectionFailure {
			state = sqlstateUnableToConnect
		}

		return nil, exception(state, failure.Message, "ISO/IEC 9075:1992 §15.1 GR10, GR11")
	}

	a.held = append(a.held, held{name: identified, conn: c})
	a.current = len(a.held) - 1

	return &Completion{SQLState: sqlstateSuccess}, nil
}

// setConnection runs SET CONNECTION name, or DEFAULT for nil: that
// connection current, the one current before dormant.
func (a *Agent) setConnection(name *literal) (*Completion, error) {
	if err := a.notInTransaction(); err != nil {
		return nil, err
	}

	text, err := textOfLiteral(name)
	if err != nil {
		return nil, err
	}

	if text != nil {
		if id, ok := identifierOf(*text); ok {
			text = &id
		}
	}

	at := a.find(text)
	if at < 0 {
		what := "no default connection"
		if text != nil {
			what = "no connection " + *text
		}

		return nil, exception(sqlstateConnectionNotExist, "there is "+what, "ISO/IEC 9075:1992 §15.2 GR2")
	}

	a.current = at

	return &Completion{SQLState: sqlstateSuccess}, nil
}

// disconnect runs DISCONNECT: each connection it names ended; none
// current after, if the current one was.
func (a *Agent) disconnect(statement connectionStatement) (*Completion, error) {
	ended, err := a.disconnected(statement)
	if err != nil {
		return nil, err
	}

	for _, i := range ended {
		if a.held[i].conn.InTransaction() {
			return nil, &Error{
				SQLCode: sqlcodeTransactionState, SQLState: sqlstateInvalidTransactionState,
				Message: "a connection's transaction is active", Rule: "ISO/IEC 9075:1992 §15.3 GR6",
			}
		}
	}

	var current *string

	currentHeld := a.current >= 0
	if currentHeld {
		current = a.held[a.current].name
	}

	warned := false
	kept := a.held[:0:0]

	for i, h := range a.held {
		if slices.Contains(ended, i) {
			warned = h.conn.Terminate() != nil || warned
		} else {
			kept = append(kept, h)
		}
	}

	a.held, a.current = kept, -1
	if currentHeld {
		a.current = a.find(current)
	}

	if warned {
		return &Completion{SQLState: sqlstateDisconnectError}, nil
	}

	return &Completion{SQLState: sqlstateSuccess}, nil
}

// disconnected gives the places of the connections DISCONNECT names.
func (a *Agent) disconnected(statement connectionStatement) ([]int, error) {
	missing := func(what string) error {
		return exception(sqlstateConnectionNotExist, "there is "+what, "ISO/IEC 9075:1992 §15.3 GR1-GR3")
	}

	switch statement.object {
	case disconnectAll:
		all := make([]int, len(a.held))
		for i := range all {
			all[i] = i
		}

		return all, nil
	case disconnectCurrent:
		if a.current < 0 {
			return nil, missing("no current connection")
		}

		return []int{a.current}, nil
	case disconnectDefault:
		if at := a.find(nil); at >= 0 {
			return []int{at}, nil
		}

		return nil, missing("no default connection")
	case disconnectNamed:
	}

	text, err := textOfLiteral(statement.name)
	if err != nil {
		return nil, err
	}

	name := *text
	if id, ok := identifierOf(name); ok {
		name = id
	}

	if at := a.find(&name); at >= 0 {
		return []int{at}, nil
	}

	return nil, missing("no connection " + name)
}

// connectionName gives the name of a connection CONNECT makes: name's,
// else its address's, as an identifier; nil of neither.
func connectionName(address *string, name *literal) (*string, error) {
	named := address

	if name != nil {
		var err error
		if named, err = textOfLiteral(name); err != nil {
			return nil, err
		}
	}

	if named == nil {
		return nil, nil //nolint:nilnil // The default connection has no name, and that is no failure.
	}

	id, ok := identifierOf(*named)
	if !ok {
		return nil, exception(sqlstateInvalidConnectionName, "'"+*named+"' is not an <identifier>",
			"ISO/IEC 9075:1992 §15.1 GR6")
	}

	return &id, nil
}

// isIdentifier says whether s is an <authorization identifier>: one
// identifier.
func isIdentifier(s string) bool {
	_, ok := identifierOf(s)

	return ok
}

// identifierOf gives s as an <identifier>, if it is one: a regular
// identifier in upper case, or a delimited one's body.
func identifierOf(s string) (string, bool) {
	words, ok := nameWords(s)
	if !ok || len(words) != 1 || words[0].kind != wordIdentifier {
		return "", false
	}

	return words[0].text, true
}

// textOfLiteral gives the character string a connection statement's
// simple value specification is, its spaces trimmed: nil of none.
func textOfLiteral(l *literal) (*string, error) {
	if l == nil {
		return nil, nil //nolint:nilnil // No value is named, and that is no failure.
	}

	if !l.character {
		return nil, &Error{
			SQLCode: sqlcodeSyntaxRule, SQLState: sqlstateSyntax,
			Message: "a client's connection statement names by character string literals",
		}
	}

	text := strings.Trim(l.text, " ")

	return &text, nil
}
