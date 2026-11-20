package turborel

import (
	"context"
	"crypto/tls"
	"net"
	"testing"
	"time"

	"github.com/turborel/turborel-go/internal/testserver"
)

// slowQuery runs long enough to be cancelled: a cross join of a system
// view with itself, thrice.
const slowQuery = "SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS A, INFORMATION_SCHEMA.COLUMNS B, " +
	"INFORMATION_SCHEMA.COLUMNS C, INFORMATION_SCHEMA.COLUMNS D"

// TestLiveCancel cancels a statement running, from another goroutine.
//
// Verifies: [PROTOCOL Cancel 'Z'] Cancel, the key Ready gave, is the first
// and only message of a connection of its own, closed with no reply; the
// statement cancelled fails with SQLCODE -920, SQLSTATE TB920.
func TestLiveCancel(t *testing.T) {
	t.Parallel()

	c := liveConnect(t, TCP(liveServer(t).Address), Trust{}, nil)

	canceller, ok := c.Canceller()
	if !ok {
		t.Fatal("no canceller")
	}

	done := make(chan error, 1)
	go func() {
		_, err := liveQuery(t, c, slowQuery)
		done <- err
	}()

	var err error

	for range 50 {
		time.Sleep(20 * time.Millisecond)

		if cancelErr := canceller.Cancel(context.Background()); cancelErr != nil {
			t.Fatalf("Cancel: %v", cancelErr)
		}

		select {
		case err = <-done:
		default:
			continue
		}

		break
	}

	failure, ok := err.(*Error) //nolint:errorlint // The statement's own failure is the server's *Error.
	if !ok || failure.SQLCode != -920 || failure.SQLState != "TB920" {
		t.Errorf("the cancelled statement gave %v", err)
	}
}

// TestLiveTerminate ends a session, its transaction committed, and gives
// a commit's failure.
//
// Verifies: [PROTOCOL Terminate 'X'] Terminate ends the session, its
// transaction as SESSION.COMMIT_AT_END says, committing it; a commit that
// fails, of a deferred constraint not satisfied, is sent as an Error
// before the close, and Terminate gives it.
func TestLiveTerminate(t *testing.T) {
	t.Parallel()

	target := liveTLS(t, 0, false)
	c := liveConnect(t, target, Trust{}, nil)
	mustQuery(t, c, "CREATE SCHEMA TERMINATE_TEST")
	mustQuery(t, c, "CREATE TABLE TERMINATE_TEST.T "+
		"(A INT CONSTRAINT TERMINATE_TEST.POSITIVE CHECK (A > 0) INITIALLY DEFERRED)")
	mustQuery(t, c, "COMMIT")
	mustQuery(t, c, "INSERT INTO TERMINATE_TEST.T VALUES (1)")

	if !c.InTransaction() {
		t.Error("no transaction is active after an INSERT")
	}

	if err := c.Terminate(); err != nil {
		t.Fatalf("Terminate: %v", err)
	}

	d := liveConnect(t, target, Trust{}, nil)
	if rows := mustQuery(t, d, "SELECT A FROM TERMINATE_TEST.T"); len(rows) != 1 {
		t.Errorf("the transaction Terminate ended left %d rows", len(rows))
	}

	mustQuery(t, d, "INSERT INTO TERMINATE_TEST.T VALUES (0)")
	assertState(t, d.Terminate(), "40002")
}

// TestLiveRefusals refuses what the server refuses as a connection opens.
//
// Verifies: [PROTOCOL Opening a connection] a version other than 1 is
// refused (SQLSTATE 08004), a user name that is not an identifier and a
// mechanism the server does not serve before any exchange (SQLSTATE
// 28000); each such Error ends the connection.
func TestLiveRefusals(t *testing.T) {
	t.Parallel()

	s := liveServer(t)
	raw, err := (&net.Dialer{}).DialContext(context.Background(), networkTCP, s.Address)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = raw.Close() }()

	hello := encodeRequest(helloRequest{user: testserver.Owner, mechanism: mechanismTrust})
	hello[frameHeaderBytes] = 2

	if _, err := raw.Write(hello); err != nil {
		t.Fatal(err)
	}

	kind, b, err := readFrame(raw, maxBody, false)
	if err != nil {
		t.Fatal(err)
	}

	rep, err := decodeReply(kind, b)
	if e, ok := rep.(errorReply); err != nil || !ok || e.failure.SQLState != "08004" || !endsConnection(e.failure) {
		t.Errorf("version 2: %#v, %v", rep, err)
	}

	_, err = Connect(context.Background(), s.Address, "not an identifier", Trust{})
	assertState(t, err, sqlstateInvalidAuthorization)

	_, err = Connect(context.Background(), s.Address, testserver.Owner, unserved{})
	assertState(t, err, sqlstateInvalidAuthorization)
}

// unserved is a mechanism no server serves.
type unserved struct{ Trust }

// Name is a mechanism's name no server knows.
func (unserved) Name() string { return "unserved" }

// TestLiveOwnStream binds SCRAM to a TLS stream the program opened.
//
// Verifies: [CLIENT Connecting] a program that opens a TLS stream of its
// own and gives it to Open binds its mechanism to it first, so the
// exchange is SCRAM-SHA-256-PLUS; a connection over such a stream has no
// canceller and no target.
func TestLiveOwnStream(t *testing.T) {
	t.Parallel()

	s := liveServer(t)

	config, err := Trusting(s.Files.CA)
	if err != nil {
		t.Fatal(err)
	}

	config.ServerName = "localhost"

	dialed, err := (&tls.Dialer{Config: config}).DialContext(context.Background(), networkTCP, s.Address)
	if err != nil {
		t.Fatal(err)
	}

	stream, ok := dialed.(*tls.Conn)
	if !ok {
		t.Fatalf("dialed %T", dialed)
	}

	scram := NewScram(testserver.OwnerPassword)
	scram.Bind(ChannelOf(stream))

	c, err := Open(context.Background(), stream, testserver.Owner, scram, nil)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = c.Close() }()

	_, hasCanceller := c.Canceller()
	_, hasTarget := c.Target()

	if scram.Name() != mechanismScramPlus || hasCanceller || hasTarget {
		t.Errorf("%s, canceller %v, target %v", scram.Name(), hasCanceller, hasTarget)
	}
}
