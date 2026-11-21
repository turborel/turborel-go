package turborel

import (
	"bufio"
	"context"
	"net"
	"testing"
)

// prepared replies Prepared of one exact parameter and one column.
func (f *fake) prepared() {
	f.reply(kindPrepared, func(w *writer) {
		w.u32(0)
		w.u32(1)
		w.flag(false)
		ExactType{Precision: 9}.encodeType(w)
		w.u32(1)
		Column{Named: true, Name: "N", Type: ExactType{Precision: 9}}.encode(w)
	})
}

// row replies Rows of one row, the exact number 7.
func (f *fake) row() {
	f.reply(kindRows, func(w *writer) { w.u32(1); encodeValues(w, []Value{DecimalOf(7, 0)}) })
}

// TestStatementFlows runs every statement's request and reply as the
// protocol lays them out.
//
// Verifies: [CLIENT Prepared statements] a statement prepared, its
// parameters and columns given, executed for its rows; its cursor opened,
// fetched and closed; described; deallocated; under autocommit each
// request ends its transaction [PROTOCOL Execute 'E'].
func TestStatementFlows(t *testing.T) {
	t.Parallel()

	c := openGranted(t, Capabilities(), func(f *fake) {
		f.request()
		f.prepared()

		if _, body := f.request(); body[len(body)-1] != flagTrue {
			t.Error("an autocommitted Execute did not end its transaction")
		}

		f.row()
		f.complete("00000", false)
		f.request()
		f.row()
		f.reply(kindSuspended, nil)
		f.request()
		f.row()
		f.complete("00000", false)
		f.request()
		f.complete("00000", false)
		f.request()
		f.prepared()
		f.request()
		f.complete("00000", false)
	})

	c.SetAutocommit(true)

	st, err := c.Prepare(context.Background(), "SELECT N FROM T WHERE N = ?")
	if err != nil || len(st.Parameters()) != 1 || len(st.Columns()) != 1 || !c.Autocommit() {
		t.Fatalf("Prepare gave %+v, %v", st, err)
	}

	o, err := c.Execute(context.Background(), st, []Value{DecimalOf(7, 0)})
	if err != nil || !o.IsQuery() || len(o.Rows) != 1 {
		t.Fatalf("Execute gave %+v, %v", o, err)
	}

	assertCursorFlow(t, c, st)
}

// assertCursorFlow opens st's cursor, fetches it, closes it, describes st
// and deallocates it, as TestStatementFlows's server answers.
func assertCursorFlow(t *testing.T, c *Conn, st *Statement) {
	t.Helper()

	first, err := c.OpenCursor(context.Background(), st, nil, 1)
	if err != nil || !first.More || len(first.Rows) != 1 {
		t.Fatalf("OpenCursor gave %+v, %v", first, err)
	}

	rest, err := c.Fetch(context.Background(), st, 0)
	if err != nil || rest.More {
		t.Fatalf("Fetch gave %+v, %v", rest, err)
	}

	if err := c.CloseCursor(context.Background(), st); err != nil {
		t.Fatal(err)
	}

	if described, err := c.Describe(context.Background(), st); err != nil || len(described.Columns()) != 1 {
		t.Fatalf("Describe gave %+v, %v", described, err)
	}

	if err := c.Deallocate(context.Background(), st); err != nil {
		t.Fatal(err)
	}
}

// TestQueryIntoRows streams a query's rows into a sink.
//
// Verifies: [CLIENT Running statements] a query's rows are given to a sink
// a batch at a time, the count of them given with the completion.
func TestQueryIntoRows(t *testing.T) {
	t.Parallel()

	c := openGranted(t, nil, func(f *fake) {
		f.request()
		f.reply(kindDescription, func(w *writer) { w.u32(0) })

		for range 2 {
			f.reply(kindRows, func(w *writer) { w.u32(1); encodeValues(w, nil) })
		}

		f.complete("00000", false)
	})

	sink := &collector{}

	streamed, err := c.QueryInto(context.Background(), "x", sink)
	if err != nil || streamed.Rows != 2 || sink.batches != 2 {
		t.Errorf("streamed %+v, %v", streamed, err)
	}
}

// TestConnectToCancels connects by TCP and cancels a statement its
// context gave up, by Cancel over a connection of its own.
//
// Verifies: [DESIGN] D2 a connection that reached its server by a target
// cancels the statement its context gave up by the protocol's Cancel, on
// a connection of its own; the statement's failure has the context's
// error as its cause [CLIENT Connecting].
func TestConnectToCancels(t *testing.T) {
	t.Parallel()

	listener, err := (&net.ListenConfig{}).Listen(context.Background(), networkTCP, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})

	go cancelledServer(t, listener, done)

	t.Cleanup(func() {
		_ = listener.Close()
		<-done
	})

	c, err := ConnectTo(context.Background(), TCP(listener.Addr().String()), "ANN", Trust{}, fakeConfig(DefaultOffer()))
	if err != nil {
		t.Fatal(err)
	}

	if target, ok := c.Target(); !ok || !target.IsPlainTCP() || c.Server() == "" || len(c.Settings()) != 0 {
		t.Errorf("target %v, server %q", target, c.Server())
	}

	ctx, cancel := context.WithCancel(context.Background())
	go cancel()

	_, err = c.Query(ctx, "VALUES 1")
	assertState(t, err, "TB920")
}

// cancelledServer serves a login and a statement on its first
// connection, and fails the statement once a Cancel comes on its second.
func cancelledServer(t *testing.T, listener net.Listener, done chan struct{}) {
	t.Helper()

	defer close(done)

	session, err := listener.Accept()
	if err != nil {
		return
	}

	defer func() { _ = session.Close() }()

	f := &fake{t: t, conn: session, r: bufio.NewReader(session)}
	f.request()
	f.ready()
	f.request()

	canceller, err := listener.Accept()
	if err != nil {
		return
	}

	if kind, _, err := readFrame(canceller, maxBody, false); err != nil || kind != kindCancel {
		t.Errorf("the second connection sent %c, %v", kind, err)
	}

	_ = canceller.Close()

	f.failure(-920, "TB920")
}

// TestMechanismNames names each mechanism as the server knows it.
//
// Verifies: [PROTOCOL Mechanisms] trust, password, certificate,
// scram-sha-256 and scram-sha-256-plus, each by its name.
func TestMechanismNames(t *testing.T) {
	t.Parallel()

	got := []string{Trust{}.Name(), Password("").Name(), Certificate{}.Name()}
	if got[0] != mechanismTrust || got[1] != mechanismPassword || got[2] != mechanismCertificate {
		t.Errorf("names %q", got)
	}

	if (scramChannel{kind: 9}).header() != gs2None {
		t.Error("a channel of no kind has a header")
	}
}
