package turborel

import (
	"context"
	"errors"
	"testing"
)

// TestConnectionStatements reads the connection statements.
//
// Verifies: [CLIENT The SQL-agent] CONNECT TO a server [AS a name] [USER
// a user], or TO DEFAULT; SET CONNECTION a name or DEFAULT; DISCONNECT a
// name, DEFAULT, ALL or CURRENT; each name a literal, a character string
// one's parts joined across line breaks, its introducer passed over.
func TestConnectionStatements(t *testing.T) {
	t.Parallel()

	good := []struct {
		sql    string
		kind   statementKind
		object disconnectObject
		server string
		name   string
		user   string
	}{
		{"connect to 'db:7422' as 'Main' user 'ann'", statementConnect, 0, "db:7422", "Main", "ann"},
		{"CONNECT TO 'a''b'\n  'c' -- the server\n", statementConnect, 0, "a'bc", "", ""},
		{"CONNECT TO _UTF8'x' USER N'y'", statementConnect, 0, "x", "", "y"},
		{"CONNECT /* hi */ TO DEFAULT", statementConnectDefault, 0, "", "", ""},
		{"SET CONNECTION 'Main'", statementSetConnection, 0, "", "Main", ""},
		{"SET CONNECTION DEFAULT", statementSetConnection, 0, "", "", ""},
		{"DISCONNECT 'Main'", statementDisconnect, disconnectNamed, "", "Main", ""},
		{"DISCONNECT DEFAULT", statementDisconnect, disconnectDefault, "", "", ""},
		{"DISCONNECT ALL", statementDisconnect, disconnectAll, "", "", ""},
		{"disconnect current", statementDisconnect, disconnectCurrent, "", "", ""},
	}

	text := func(l *literal) string {
		if l == nil {
			return ""
		}

		return l.text
	}

	for _, c := range good {
		s, ok, err := parseConnectionStatement(c.sql)
		if err != nil || !ok || s.kind != c.kind || s.object != c.object || text(s.server) != c.server ||
			text(s.name) != c.name || text(s.user) != c.user {
			t.Errorf("%q gave %+v, %v, %v", c.sql, s, ok, err)
		}
	}
}

// TestConnectionStatementsRefused reads other statements as none, and
// refuses one that does not parse.
//
// Verifies: [CLIENT The SQL-agent] any statement but CONNECT, SET
// CONNECTION and DISCONNECT is none of them; one that does not parse is
// refused, saying where (SQLSTATE 2A000).
func TestConnectionStatementsRefused(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{"SELECT 1", "SET SCHEMA 'A'", "", "  ", "'x'", "SET"} {
		if _, ok, err := parseConnectionStatement(sql); ok || err != nil {
			t.Errorf("%q was read as one: %v", sql, err)
		}
	}

	for sql, want := range map[string]string{
		"CONNECT 'x'":            "expected TO, found 'x'",
		"CONNECT TO ann":         "expected a <literal>, found ANN",
		"SET CONNECTION":         "expected a <literal>, found end of statement",
		"DISCONNECT ;":           `expected a <literal>, found ";"`,
		"CONNECT TO 'x":          "unterminated <character string literal>",
		"CONNECT TO 'x' /* open": "a <comment> is not closed",
		"CONNECT TO \"\"":        "an <identifier> is malformed",
	} {
		_, _, err := parseConnectionStatement(sql)
		if failure, ok := errors.AsType[*Error](err); !ok || failure.SQLState != sqlstateSyntax || failure.Message != want {
			t.Errorf("%q gave %v", sql, err)
		}
	}
}

// TestAgentRefusesBadNames refuses a connection statement's bad names
// before connecting.
//
// Verifies: [CLIENT The SQL-agent] a name that is no character string
// literal is refused (SQLSTATE 2A000); a user that is no authorization
// identifier (28000); a connection name that is no identifier (2E000);
// with no connection, a statement other than CONNECT has no current
// connection (08003).
func TestAgentRefusesBadNames(t *testing.T) {
	t.Parallel()

	never := ConnectorFunc(func(context.Context, *string, *string) (*Conn, error) {
		t.Error("the agent connected")

		return nil, errDiskGone
	})
	a := NewAgent(never)
	ctx := context.Background()

	for sql, state := range map[string]string{
		"CONNECT TO 7":                    sqlstateSyntax,
		"CONNECT TO 's' USER 'two words'": sqlstateInvalidAuthorization,
		"CONNECT TO 's' AS '1st'":         sqlstateInvalidConnectionName,
		"SET CONNECTION 'x'":              sqlstateConnectionNotExist,
		"DISCONNECT CURRENT":              sqlstateConnectionNotExist,
		"DISCONNECT DEFAULT":              sqlstateConnectionNotExist,
		"DISCONNECT 'x'":                  sqlstateConnectionNotExist,
		"DISCONNECT 7":                    sqlstateSyntax,
		"SELECT 1":                        sqlstateConnectionNotExist,
	} {
		_, err := a.Query(ctx, sql)
		assertState(t, err, state)
	}

	if _, err := a.QueryInto(ctx, "SELECT 1", nil); err == nil {
		t.Error("a statement ran with no connection")
	}

	if _, ok := a.CurrentName(); ok || a.IsLost() || a.Terminate() != nil {
		t.Error("an agent of no connection")
	}
}

// servedTwice serves two queries, then Terminate.
func servedTwice(f *fake) {
	f.expect(kindQuery)
	f.done(false)
	f.expect(kindQuery)
	f.done(false)
	f.expect(kindTerminate)
}

// TestAgentConnections makes, chooses and ends scripted connections.
//
// Verifies: [CLIENT The SQL-agent] CONNECT makes a connection through the
// connector, named by its address when given no name, made current;
// SET CONNECTION chooses one; every other statement goes to the current
// one, its rows to a sink too; DISCONNECT ends one, a failure to end it a
// warning (01002); DISCONNECT ALL ends every one; Terminate ends those
// left, giving the first failure.
func TestAgentConnections(t *testing.T) {
	t.Parallel()

	scripts := []func(f *fake){servedTwice, func(*fake) {}}
	next := 0
	connector := ConnectorFunc(func(context.Context, *string, *string) (*Conn, error) {
		next++

		return openGranted(t, nil, scripts[next-1]), nil //nolint:contextcheck // A scripted server's connection.
	})
	a := NewAgent(connector)
	ctx := context.Background()

	mustAgent(t, a, "CONNECT TO 'one'")

	if _, err := a.QueryInto(ctx, "VALUES 1", &collector{}); err != nil {
		t.Fatal(err)
	}

	mustAgent(t, a, "CONNECT TO 'two' AS 'b'")
	mustAgent(t, a, "SET CONNECTION 'one'")

	if name, ok := a.CurrentName(); !ok || name != "ONE" {
		t.Errorf("the current connection is %q", name)
	}

	if _, err := a.Query(ctx, "VALUES 2"); err != nil {
		t.Fatal(err)
	}

	if out, err := a.Query(ctx, "DISCONNECT CURRENT"); err != nil || out.Completion.SQLState != sqlstateSuccess {
		t.Errorf("DISCONNECT gave %v, %v", out, err)
	}

	if out, err := a.Query(ctx, "DISCONNECT ALL"); err != nil || out.Completion.SQLState != sqlstateDisconnectError {
		t.Errorf("DISCONNECT ALL of a connection that fails to end gave %v, %v", out, err)
	}

	again := ConnectedAgent(connector, openGranted(t, nil, func(*fake) {}))
	if err := again.Terminate(); err == nil {
		t.Error("a connection that fails to end ended")
	}
}

// TestAgentRefusals refuses to leave a transaction and says why a
// connection was not made.
//
// Verifies: [CLIENT The SQL-agent] CONNECT and SET CONNECTION refuse to
// leave the current connection's active transaction (0A001), DISCONNECT
// to end one (25000); a name in use is refused (08002); a server that
// cannot be reached gives 08001, one that refuses 08004; a binary string
// literal is no name (2A000).
func TestAgentRefusals(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	active := openGranted(t, nil, func(f *fake) {
		f.expect(kindQuery)
		f.done(true)
	})
	refusals := []error{broken("unreachable"), &Error{SQLCode: sqlcodeConnection, SQLState: sqlstateInvalidAuthorization}}
	connector := ConnectorFunc(func(context.Context, *string, *string) (*Conn, error) {
		err := refusals[0]
		refusals = refusals[1:]

		return nil, err
	})
	a := ConnectedAgent(connector, active)

	for _, c := range [][2]string{
		{"CONNECT TO DEFAULT", sqlstateNameInUse},
		{"CONNECT TO 'a'", sqlstateUnableToConnect},
		{"CONNECT TO 'b'", sqlstateConnectionRejected},
		{"CONNECT TO X'00'", sqlstateSyntax},
	} {
		_, err := a.Query(ctx, c[0])
		assertState(t, err, c[1])
	}

	mustAgent(t, a, "INSERT")

	for _, c := range [][2]string{
		{"CONNECT TO 'c'", sqlstateMultipleServerTxn},
		{"SET CONNECTION DEFAULT", sqlstateMultipleServerTxn},
		{"DISCONNECT DEFAULT", sqlstateInvalidTransactionState},
	} {
		_, err := a.Query(ctx, c[0])
		assertState(t, err, c[1])
	}
}
