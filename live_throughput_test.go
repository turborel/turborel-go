package turborel

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/turborel/turborel-go/internal/testserver"
)

// TestLivePipeline runs a batch of requests in one round trip.
//
// Verifies: [CLIENT Pipelines] queued queries and executions run, one
// outcome each in order, a failure its own; under autocommit each request
// is its own transaction; skipping on error, a batch is one transaction,
// the requests after a failure rolled back and skipped, the rest
// committed as it ends [PROTOCOL Requests].
func TestLivePipeline(t *testing.T) {
	t.Parallel()

	c := liveSchema(t, "PIPE_TEST")
	mustRun(t, c, "CREATE TABLE PIPE_TEST.T (K INTEGER PRIMARY KEY)")
	mustRun(t, c, "COMMIT")
	c.SetAutocommit(true)

	insert, err := c.Prepare(context.Background(), "INSERT INTO PIPE_TEST.T VALUES (?)")
	if err != nil {
		t.Fatal(err)
	}

	out, err := c.Pipeline().Execute(insert, []Value{DecimalOf(1, 0)}).Execute(insert, []Value{DecimalOf(1, 0)}).
		Query("INSERT INTO PIPE_TEST.T VALUES (2)").Query("SELECT K FROM PIPE_TEST.T ORDER BY K").Run(context.Background())
	if err != nil || out[1].Failure == nil || out[0].Outcome == nil || len(out[3].Outcome.Rows) != 2 {
		t.Fatalf("a batch gave %+v, %v", out, err)
	}

	out, err = c.Pipeline().OnError(OnErrorSkip).Query("INSERT INTO PIPE_TEST.T VALUES (3)").
		Query("INSERT INTO PIPE_TEST.T VALUES (1)").Query("INSERT INTO PIPE_TEST.T VALUES (4)").Run(context.Background())
	if err != nil || out[0].Outcome == nil || out[1].Failure == nil || !out[2].Skipped || c.InTransaction() {
		t.Fatalf("a skipping batch gave %+v, %v", out, err)
	}

	if got := valueText(mustRun(t, c, "SELECT K FROM PIPE_TEST.T ORDER BY K").Rows[2]); got != "ROW(3)" {
		t.Errorf("after the batches the table's third row is %s", got)
	}
}

// liveSource makes the live server's connections as settings say.
func liveSource(t *testing.T) *Settings {
	t.Helper()

	return liveSettings(t, "", "tls_mode=verify-full")
}

// TestLivePool hands out, resets and takes back connections.
//
// Verifies: [CLIENT Pools] a pool made of settings gives a connection,
// made while fewer than max are; given back, it is reset by the
// protocol's Reset, autocommit off, then the reset statements run, and
// handed out again, checked by Ping; with max made and none back, get
// fails within wait (SQLSTATE 08001).
func TestLivePool(t *testing.T) {
	t.Parallel()

	config := DefaultPoolConfig()
	config.Max, config.Wait = 1, 50*time.Millisecond
	config.Reset = []string{"SET TIME ZONE INTERVAL '+03:00' HOUR TO MINUTE"}
	pool := NewPool(liveSource(t), config)

	first, err := pool.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	mustRun(t, first.Conn, "SET TIME ZONE INTERVAL '+05:00' HOUR TO MINUTE")
	first.SetAutocommit(true)
	conn := first.Conn

	_, err = pool.Get(context.Background())
	assertUnable(t, "a pool of one handed out", err)

	first.Release()
	first.Release()

	if made, idle := pool.Counts(); made != 1 || idle != 1 {
		t.Errorf("made %d, idle %d", made, idle)
	}

	again, err := pool.Get(context.Background())
	if err != nil || again.Conn != conn || again.Autocommit() {
		t.Fatalf("handed out %v, %v", again, err)
	}

	if zone, _ := again.Setting("SESSION.TIME_ZONE"); zone != "+03:00" {
		t.Errorf("the zone after the reset is %s", zone)
	}

	again.Release()
}

// TestLivePoolExpiry closes connections past their lifetime or idle past
// their timeout.
//
// Verifies: [CLIENT Pools] one past its lifetime is closed as it is
// given back, one idle past idle_timeout as get meets it, another made.
func TestLivePoolExpiry(t *testing.T) {
	t.Parallel()

	config := DefaultPoolConfig()
	config.MaxLifetime = time.Nanosecond
	pool := NewPool(liveSource(t), config)

	old, err := pool.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	old.Release()

	if made, idle := pool.Counts(); made != 0 || idle != 0 {
		t.Errorf("past its lifetime: made %d, idle %d", made, idle)
	}

	pool.config.MaxLifetime, pool.config.IdleTimeout = 0, time.Nanosecond

	stale, err := pool.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	stale.Release()

	fresh, err := pool.Get(context.Background())
	if err != nil || fresh.Conn == stale.Conn {
		t.Errorf("an idle connection past its timeout was handed out: %v", err)
	}

	fresh.Release()
}

// TestLivePoolByStatements checks and resets connections of a server
// that grants neither ping nor reset.
//
// Verifies: [CLIENT Pools] a pool made of anything that makes a
// connection; of a server that grants neither ping nor reset the check
// is ROLLBACK and the reset one pipeline, its transaction rolled back,
// its prepared statements destroyed, its schema and zone its login's;
// beyond max_idle a connection is closed as it is given back; a lost
// one is closed.
func TestLivePoolByStatements(t *testing.T) {
	t.Parallel()

	offer := Offer{Capabilities: []string{CapabilityStatus}}
	source := PoolSourceFunc(func(ctx context.Context) (*Conn, error) {
		return ConnectTo(ctx, liveTLS(t, 0, false), testserver.Owner, Trust{}, &Config{Offer: &offer})
	})
	config := DefaultPoolConfig()
	config.MaxIdle = 1
	pool := NewPool(source, config)

	a, err := pool.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	b, err := pool.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := a.Prepare(context.Background(), "VALUES 1"); err != nil {
		t.Fatal(err)
	}

	mustRun(t, a.Conn, "CREATE SCHEMA POOL_STATEMENTS")
	mustRun(t, a.Conn, "SET SCHEMA 'POOL_STATEMENTS'")

	a.Release()
	b.Release()

	if made, idle := pool.Counts(); made != 1 || idle != 1 {
		t.Errorf("beyond max_idle: made %d, idle %d", made, idle)
	}

	c, err := pool.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if schema, _ := c.Setting(StatusSchema); schema != testserver.Owner || c.InTransaction() || c.nextStatement != 0 {
		t.Errorf("after the reset: schema %s, active %v", schema, c.InTransaction())
	}

	_ = c.Close()
	c.Release()
}

// TestPoolSourceFails gives get the failure of making a connection.
//
// Verifies: [CLIENT Pools] a failure to make a connection is get's, its
// place given up.
func TestPoolSourceFails(t *testing.T) {
	t.Parallel()

	config := DefaultPoolConfig()
	refusing := NewPool(PoolSourceFunc(func(context.Context) (*Conn, error) { return nil, errDiskGone }), config)
	if _, err := refusing.Get(context.Background()); !errors.Is(err, errDiskGone) {
		t.Errorf("a source that fails gave %v", err)
	}

	if made, _ := refusing.Counts(); made != 0 {
		t.Errorf("a failed make kept its place: %d", made)
	}
}

// TestLiveAgent runs connection statements and sends the rest on.
//
// Verifies: [CLIENT The SQL-agent] a program's connections, each named,
// one current; CONNECT through the connector, to the address and as the
// user it names, the connection made current, its name, or else its
// address's, in use refused (08002); SET CONNECTION; DISCONNECT, of the
// current connection leaving none current; every other statement sent to
// the current connection; none left while a transaction is active (0A001,
// 25000); a server that cannot be reached 08001, one that refuses 08004.
func TestLiveAgent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	connector := liveConnector(t)

	first, err := connector(ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	a := ConnectedAgent(connector, first)
	t.Cleanup(func() { _ = a.Terminate() })

	mustAgent(t, a, "CONNECT TO 'db' AS 'other' USER 'ANN'")

	for _, c := range [][2]string{
		{"CONNECT TO 'db' AS 'other'", sqlstateNameInUse},
		{"CONNECT TO DEFAULT", sqlstateNameInUse},
		{"CONNECT TO 'nowhere'", sqlstateUnableToConnect},
		{"CONNECT TO 'db' USER 'BOB'", sqlstateConnectionRejected},
	} {
		_, err := a.Query(ctx, c[0])
		assertState(t, err, c[1])
	}

	if name, ok := a.CurrentName(); !ok || name != "OTHER" {
		t.Errorf("the current connection is %q", name)
	}

	if out, err := a.Query(ctx, "VALUES 1"); err != nil || len(out.Rows) != 1 {
		t.Errorf("a statement on the current connection gave %v", err)
	}

	_, err = a.Query(ctx, "SET CONNECTION DEFAULT")
	assertState(t, err, sqlstateMultipleServerTxn)

	_, err = a.Query(ctx, "DISCONNECT 'other'")
	assertState(t, err, sqlstateInvalidTransactionState)

	mustAgent(t, a, "COMMIT")
	assertDisconnects(t, a)
}

// assertDisconnects chooses the default connection, ends it, then ends
// every one.
func assertDisconnects(t *testing.T, a *Agent) {
	t.Helper()

	mustAgent(t, a, "SET CONNECTION DEFAULT")

	if _, ok := a.CurrentName(); ok {
		t.Error("the default connection has a name")
	}

	mustAgent(t, a, "DISCONNECT CURRENT")

	if _, err := a.Current(); err == nil {
		t.Error("a connection is current after its own was ended")
	}

	mustAgent(t, a, "SET CONNECTION 'other'")
	mustAgent(t, a, "DISCONNECT ALL")
}

// liveConnector connects to the live server as the owner, refusing a
// server named nowhere as unreachable and a user not the owner.
func liveConnector(t *testing.T) ConnectorFunc {
	t.Helper()

	return func(ctx context.Context, server, user *string) (*Conn, error) {
		if server != nil && *server == "nowhere" {
			return nil, broken("cannot connect")
		}

		if user != nil && *user != testserver.Owner {
			return nil, &Error{SQLCode: sqlcodeConnection, SQLState: sqlstateInvalidAuthorization, Message: "refused"}
		}

		return ConnectTo(ctx, liveTLS(t, 0, false), testserver.Owner, Trust{}, nil)
	}
}

// mustAgent runs sql on the agent a, failing the test if it fails.
func mustAgent(t *testing.T, a *Agent, sql string) {
	t.Helper()

	if _, err := a.Query(context.Background(), sql); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}
