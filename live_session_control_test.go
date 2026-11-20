package turborel

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/turborel/turborel-go/internal/testserver"
)

// warningQuery raises the warning 01003, a null left out of a set
// function.
const warningQuery = "SELECT SUM(A) FROM (VALUES (1), (CAST(NULL AS INTEGER))) AS T (A)"

// TestLiveConditions tells a statement's warnings one way: in its
// Complete with conditions, else as Notices.
//
// Verifies: [PROTOCOL Errors and notices] a statement's conditions are
// told one way: in its Complete, tag 17, with conditions; else as
// Notices before its last reply, with notices; each a failure whose
// severity is WARNING [CLIENT What the server tells], notices and
// conditions granted [PROTOCOL Capabilities].
func TestLiveConditions(t *testing.T) {
	t.Parallel()

	c := liveConnect(t, liveTLS(t, 0, false), Trust{}, nil)
	mustRun(t, c, warningQuery)

	conditions := c.Conditions()
	if len(conditions) != 1 || conditions[0].SQLState != "01003" || conditions[0].Diagnostics == nil ||
		conditions[0].Diagnostics.Severity != "WARNING" {
		t.Errorf("conditions %#v", conditions)
	}

	if state, ok := c.Warning(); !ok || state != "01003" {
		t.Errorf("warning %q, %v", state, ok)
	}

	offer := DefaultOffer()
	offer.Capabilities = withoutCapability(offer.Capabilities, CapabilityConditions)

	n := liveConnect(t, liveTLS(t, 0, false), Trust{}, &Config{Offer: &offer})
	mustRun(t, n, warningQuery)

	notices := n.TakeNotices()
	if len(notices) != 1 || notices[0].SQLState != "01003" || len(n.Conditions()) != 0 {
		t.Errorf("notices %#v", notices)
	}
}

// withoutCapability gives capabilities without name.
func withoutCapability(capabilities []string, name string) []string {
	var kept []string

	for _, c := range capabilities {
		if c != name {
			kept = append(kept, c)
		}
	}

	return kept
}

// TestLiveDiagnostics reads what a failure is about.
//
// Verifies: [PROTOCOL Errors and notices] with diagnostics a failure's
// tail says its severity, ERROR, and what it is about: a duplicate key
// names its constraint, table and schema, and its detail gives the key
// [CLIENT What the server tells], diagnostics granted [PROTOCOL
// Capabilities].
func TestLiveDiagnostics(t *testing.T) {
	t.Parallel()

	c := liveSchema(t, "DIAGNOSTICS_TEST")
	mustRun(t, c, "CREATE TABLE DIAGNOSTICS_TEST.T (ID INT CONSTRAINT DIAGNOSTICS_TEST.KEY_ID PRIMARY KEY)")
	mustRun(t, c, "INSERT INTO DIAGNOSTICS_TEST.T VALUES (1)")

	_, err := c.Query(context.Background(), "INSERT INTO DIAGNOSTICS_TEST.T VALUES (1)")

	var failure *Error
	if !errors.As(err, &failure) || failure.Diagnostics == nil {
		t.Fatalf("a duplicate key gave %#v", err)
	}

	d := failure.Diagnostics
	about := Diagnostics{Severity: "ERROR", Constraint: "KEY_ID", Table: "T", Schema: "DIAGNOSTICS_TEST", Detail: d.Detail}
	if *d != about || d.Detail == "" {
		t.Errorf("diagnostics %#v", d)
	}

	if !failure.Active {
		t.Error("the failure says no transaction is active")
	}
}

// TestLiveReset puts a session in a transaction back as it was at login.
//
// Verifies: [PROTOCOL Reset 'W'] Reset rolls the transaction back, its
// prepared statements and settings gone, its user and cancel key kept;
// a Notice of 01T01 comes first, of severity NOTICE, then a Status of
// each setting, then a Complete that says no transaction is active
// [CLIENT Controlling the session] [PROTOCOL Conversations], reset
// granted [PROTOCOL Capabilities].
func TestLiveReset(t *testing.T) {
	t.Parallel()

	c := liveSchema(t, "RESET_TEST")
	mustRun(t, c, "CREATE TABLE RESET_TEST.T (A INT)")
	mustRun(t, c, "COMMIT")
	mustRun(t, c, "SET TIME ZONE INTERVAL '+03:00' HOUR TO MINUTE")
	mustRun(t, c, "INSERT INTO RESET_TEST.T VALUES (1)")

	st, err := c.Prepare(context.Background(), "VALUES 1")
	if err != nil {
		t.Fatal(err)
	}

	if err := c.Reset(context.Background()); err != nil {
		t.Fatal(err)
	}

	notices := c.TakeNotices()
	if len(notices) != 1 || notices[0].SQLState != "01T01" || notices[0].Diagnostics.Severity != "NOTICE" {
		t.Errorf("notices %#v", notices)
	}

	if zone, _ := c.Setting("SESSION.TIME_ZONE"); zone != "+00:00" || c.InTransaction() {
		t.Errorf("zone %q, active %v", zone, c.InTransaction())
	}

	_, err = c.Execute(context.Background(), st, nil)
	assertState(t, err, "26000")

	if rows := mustRun(t, c, "SELECT A FROM RESET_TEST.T"); len(rows.Rows) != 0 {
		t.Error("Reset kept the transaction's row")
	}
}

// TestLiveChangeUser authenticates the connection again as another user.
//
// Verifies: [PROTOCOL ChangeUser 'U'] ChangeUser authenticates the
// connection again, with the same Challenge and Answer steps as Hello's,
// SCRAM's here; accepted, the session is renewed under the new user and a
// new cancel key, Ready keeping the capabilities, no transaction active,
// as after Reset (RULES.md D8); refused, the connection fails [CLIENT
// Controlling the session], change-user granted [PROTOCOL Capabilities].
func TestLiveChangeUser(t *testing.T) {
	t.Parallel()

	owner := liveConnect(t, liveTLS(t, 0, false), Trust{}, nil)
	mustRun(t, owner, "CREATE USER CHANGED PASSWORD 'changed''s'")

	c := liveConnect(t, liveTLS(t, 0, false), Trust{}, nil)
	key := c.key

	mustRun(t, c, "VALUES 1")

	if !c.InTransaction() {
		t.Fatal("the query left no transaction active")
	}

	if err := c.ChangeUser(context.Background(), "CHANGED", NewScram("changed's")); err != nil {
		t.Fatal(err)
	}

	if c.Authorization() != "CHANGED" || c.key == key || len(c.GrantedCapabilities()) == 0 || c.InTransaction() {
		t.Errorf("authorization %q, key renewed %v, active %v", c.Authorization(), c.key != key, c.InTransaction())
	}

	err := c.ChangeUser(context.Background(), testserver.Owner, NewScram("wrong"))
	assertState(t, err, sqlstateInvalidAuthorization)

	if !c.IsLost() {
		t.Error("a refused ChangeUser kept the connection")
	}
}

// TestLiveQueryAttributes carries attributes on every later statement.
//
// Verifies: [CLIENT Controlling the session] query attributes set are
// carried by every later statement, read by QUERY_ATTRIBUTE, until set
// again [PROTOCOL The tail], query-attributes granted [PROTOCOL
// Capabilities].
func TestLiveQueryAttributes(t *testing.T) {
	t.Parallel()

	c := liveConnect(t, liveTLS(t, 0, false), Trust{}, nil)
	if err := c.SetQueryAttributes([]Attribute{{Name: "traceparent", Value: "00-abc"}}); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		row := mustRun(t, c, "VALUES QUERY_ATTRIBUTE('traceparent')").Rows[0]
		if row[0] != Char("00-abc") {
			t.Errorf("QUERY_ATTRIBUTE gave %#v", row[0])
		}
	}

	if err := c.SetQueryAttributes(nil); err != nil {
		t.Fatal(err)
	}

	if row := mustRun(t, c, "VALUES QUERY_ATTRIBUTE('traceparent')").Rows[0]; row[0] != (Null{}) {
		t.Errorf("an attribute set aside gave %#v", row[0])
	}
}

// TestLiveQueryMany runs several statements as one request.
//
// Verifies: [CLIENT Controlling the session] a text of several statements
// runs as one request, each in turn, until one fails: each outcome, and
// the failure that stopped the rest [PROTOCOL Query 'Q'],
// multi-statement granted [PROTOCOL Capabilities].
func TestLiveQueryMany(t *testing.T) {
	t.Parallel()

	c := liveConnect(t, liveTLS(t, 0, false), Trust{}, nil)

	ran, err := c.QueryMany(context.Background(), "VALUES 1; VALUES 2; SELECT A FROM NOPE; VALUES 3")
	if err != nil {
		t.Fatal(err)
	}

	if len(ran.Outcomes) != 2 || ran.Outcomes[1].Rows[0][0] != DecimalOf(2, 0) || ran.Failure == nil ||
		ran.Failure.SQLState != "2A000" {
		t.Errorf("ran %+v", ran)
	}

	if empty, err := c.QueryMany(context.Background(), ""); err != nil || len(empty.Outcomes) != 1 {
		t.Errorf("text of no statement gave %+v, %v", empty, err)
	}
}

// TestLiveXMLAndUUID reads and writes XML and UUID values.
//
// Verifies: [CLIENT XML values] an XML value comes as its serialization,
// its nodes parsed from it, and a parameter is made of its text; one the
// server cannot serialize fails the fetch (SQLSTATE 2200W); a UUID comes
// as its 16 octets, whatever the program registered, its text given, and
// a parameter is made of its octets or text [CLIENT UUIDs].
func TestLiveXMLAndUUID(t *testing.T) {
	t.Parallel()

	c := liveConnect(t, liveTLS(t, 0, false), Trust{}, nil)

	st, err := c.Prepare(context.Background(), "VALUES (CAST(? AS XML), CAST(? AS UUID))")
	if err != nil {
		t.Fatal(err)
	}

	id, _ := ParseUUID("919108f7-52d1-4320-9bac-f847db4148a8")

	got, err := c.Execute(context.Background(), st, []Value{XML(`<order no="18"><item>jam</item></order>`), id})
	if err != nil {
		t.Fatal(err)
	}

	order, ok := got.Rows[0][0].(XML)

	nodes, nodesErr := order.Nodes()
	if !ok || nodesErr != nil || len(nodes) != 1 || order.Text() != `<order no="18"><item>jam</item></order>` {
		t.Errorf("XML %#v, nodes %v, %v", got.Rows[0][0], nodes, nodesErr)
	}

	if u, ok := got.Rows[0][1].(UUID); !ok || u.String() != id.String() {
		t.Errorf("UUID %#v", got.Rows[0][1])
	}

	_, err = c.Query(context.Background(), "VALUES (XMLQUERY('attribute a {1}' RETURNING SEQUENCE BY REF EMPTY ON EMPTY))")
	assertState(t, err, "2200W")
}

// TestLiveLocatorKeepsTransaction keeps the transaction of a result that
// sent a large object by its locator.
//
// Verifies: [PROTOCOL Query 'Q'] a result that sent a large object by its
// locator keeps its transaction, even with `ends` set, since the locator
// lasts only as long as it does.
func TestLiveLocatorKeepsTransaction(t *testing.T) {
	t.Parallel()

	c := liveSchema(t, "LOCATOR_TEST")
	mustRun(t, c, "CREATE TABLE LOCATOR_TEST.T (B BLOB(1M))")

	st, err := c.Prepare(context.Background(), "INSERT INTO LOCATOR_TEST.T VALUES (?)")
	if err != nil {
		t.Fatal(err)
	}

	long := LOB{Kind: BinaryLOB, Octets: make([]byte, 100_000)}
	if _, err := c.Execute(context.Background(), st, []Value{long}); err != nil {
		t.Fatal(err)
	}

	mustRun(t, c, "COMMIT")
	c.SetAutocommit(true)

	rows := mustRun(t, c, "SELECT B FROM LOCATOR_TEST.T")
	if _, ok := rows.Rows[0][0].(Locator); !ok || !c.InTransaction() {
		t.Errorf("read %T, active %v", rows.Rows[0][0], c.InTransaction())
	}
}

// TestLiveContextCancels cancels a statement on the server as its context
// is done.
//
// Verifies: [DESIGN] D2 a context done while a statement runs cancels it
// on the server, by the protocol's Cancel; the call's failure, SQLSTATE
// TB920, has the context's error as its cause, and the connection goes on
// [PROTOCOL Cancel 'Z'].
func TestLiveContextCancels(t *testing.T) {
	t.Parallel()

	c := liveConnect(t, TCP(liveServer(t).Address), Trust{}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := c.Query(ctx, slowQuery)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want it caused by the context", err)
	}

	assertState(t, err, "TB920")

	if c.IsLost() {
		t.Fatal("cancelling lost the connection")
	}

	if row := mustRun(t, c, "VALUES 'after'").Rows[0]; row[0] != Char("after") {
		t.Errorf("after the cancel: %#v", row)
	}
}

// TestLiveFatal ends the connection of a user dropped.
//
// Verifies: [PROTOCOL Errors and notices] a failure that ends the
// connection is of severity FATAL: a user dropped has its next statement
// refused, its connection closed (SQLSTATE 28000), and the connection
// is lost [PROTOCOL Error 'E'].
func TestLiveFatal(t *testing.T) {
	t.Parallel()

	owner := liveConnect(t, liveTLS(t, 0, false), Trust{}, nil)
	mustRun(t, owner, "CREATE USER DROPPED PASSWORD 'dropped''s'")

	c, err := ConnectTo(context.Background(), liveTLS(t, 0, false), "DROPPED", Password("dropped's"), nil)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = c.Close() }()

	mustRun(t, owner, "DROP USER DROPPED")

	_, err = c.Query(context.Background(), "VALUES 1")

	var failure *Error
	if !errors.As(err, &failure) || failure.SQLState != sqlstateInvalidAuthorization || failure.Diagnostics == nil ||
		failure.Diagnostics.Severity != "FATAL" || !c.IsLost() {
		t.Errorf("err = %#v, lost %v", err, c.IsLost())
	}
}
