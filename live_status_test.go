package turborel

import (
	"bytes"
	"context"
	"net"
	"slices"
	"sync"
	"testing"

	"github.com/turborel/turborel-go/internal/testserver"
)

// recording is a connection that keeps every octet it reads.
type recording struct {
	net.Conn

	mu   sync.Mutex
	read bytes.Buffer
}

func (r *recording) Read(b []byte) (int, error) {
	n, err := r.Conn.Read(b)

	r.mu.Lock()
	r.read.Write(b[:n])
	r.mu.Unlock()

	return n, err //nolint:wrapcheck // The connection's own failure, as a stream gives it.
}

// statuses takes the names of the Status replies read since the last
// call, in order.
func (r *recording) statuses(t *testing.T) []string {
	t.Helper()

	r.mu.Lock()
	defer r.mu.Unlock()

	var names []string

	for r.read.Len() > 0 {
		kind, body, err := readFrame(&r.read, maxBody, false)
		if err != nil {
			t.Fatal(err)
		}

		if kind == kindStatus {
			names = append(names, newReader(body, "reply").str())
		}
	}

	return names
}

// statusOrder is the order the page's table gives the names in.
func statusOrder() []string {
	return []string{
		StatusServerVersion, StatusAuthorization, StatusSchema, StatusReadOnly, StatusServerReadOnly, StatusCatalog,
		StatusOwner,
	}
}

// TestLiveStatusOrder reads the Status replies in the page's order.
//
// Verifies: [PROTOCOL Status 'T'] at login before Ready, and after Reset
// before its reply, the server sends one of each name in the page's
// table, in its order, then the session's own settings, SESSION.TIME_ZONE
// and TRANSACTION.ISOLATION among them; after a statement only what it
// changed, none of a statement that changed nothing, the first after the
// login too.
func TestLiveStatusOrder(t *testing.T) {
	t.Parallel()

	s := liveServer(t)
	ctx := context.Background()

	stream, err := (&net.Dialer{}).DialContext(ctx, "tcp", s.Address)
	if err != nil {
		t.Fatal(err)
	}

	r := &recording{Conn: stream}

	c, err := Open(ctx, r, testserver.Owner, Trust{}, nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = c.Close() })
	assertStatusOrder(t, r.statuses(t))

	for _, sql := range []string{"VALUES 1", "CREATE SCHEMA STATUS_ORDER", "COMMIT", "VALUES 1"} {
		mustRun(t, c, sql)

		if names := r.statuses(t); len(names) != 0 && sql == "VALUES 1" {
			t.Errorf("%s, which changed nothing, told %v", sql, names)
		}
	}

	mustRun(t, c, "SET SCHEMA 'STATUS_ORDER'")

	if names := r.statuses(t); !slices.Equal(names, []string{StatusSchema}) {
		t.Errorf("SET SCHEMA told %v", names)
	}

	if err := c.Reset(ctx); err != nil {
		t.Fatal(err)
	}

	assertStatusOrder(t, r.statuses(t))
}

// assertStatusOrder checks that names are the page's table, in order,
// then the session's settings.
func assertStatusOrder(t *testing.T, names []string) {
	t.Helper()

	order := statusOrder()
	if len(names) < len(order) || !slices.Equal(names[:len(order)], order) ||
		!slices.Contains(names[len(order):], "SESSION.TIME_ZONE") ||
		!slices.Contains(names[len(order):], "TRANSACTION.ISOLATION") {
		t.Errorf("told %v", names)
	}
}

// TestLiveCallOutputs gives a CALL's output parameters as a row.
//
// Verifies: [PROTOCOL Prepared 'P'] a CALL's parameters are its inputs
// alone, an INOUT `?` among them, and its columns one for each `?` of an
// OUT or INOUT parameter, in the procedure's order, named as each;
// executed, its outputs' values come as one row of those columns before
// its Complete, null of a row not found [PROTOCOL Execute 'E']; a CALL of
// none completes as another statement does [CLIENT Prepared statements].
func TestLiveCallOutputs(t *testing.T) {
	t.Parallel()

	c := liveSchema(t, "CALL_TEST")
	mustRun(t, c, "CREATE TABLE CALL_TEST.T (K INTEGER)")
	mustRun(t, c, "INSERT INTO CALL_TEST.T VALUES (1), (2), (3)")
	mustRun(t, c, "CREATE PROCEDURE CALL_TEST.FIND (IN WANTED INTEGER, OUT GOT INTEGER) READS SQL DATA "+
		"SELECT K * 10 INTO GOT FROM CALL_TEST.T WHERE K = WANTED")
	mustRun(t, c, "CREATE PROCEDURE CALL_TEST.BUMP (INOUT N INTEGER, OUT SEEN VARCHAR(5)) READS SQL DATA "+
		"SELECT N + MAX(K), 'X' INTO N, SEEN FROM CALL_TEST.T")
	mustRun(t, c, "CREATE PROCEDURE CALL_TEST.ADD_ROW (IN K INTEGER) MODIFIES SQL DATA INSERT INTO CALL_TEST.T VALUES (K)")
	mustRun(t, c, "COMMIT")

	find := prepareCall(t, c, "CALL CALL_TEST.FIND(?, ?)", 1, []string{"GOT"})
	assertCall(t, c, find, DecimalOf(2, 0), "ROW(20)")
	assertCall(t, c, find, DecimalOf(9, 0), "ROW(NULL)")

	bump := prepareCall(t, c, "CALL CALL_TEST.BUMP(?, ?)", 1, []string{"N", "SEEN"})
	assertCall(t, c, bump, DecimalOf(4, 0), "ROW(7, 'X')")

	add := prepareCall(t, c, "CALL CALL_TEST.ADD_ROW(?)", 1, nil)
	if out, err := c.Execute(context.Background(), add, []Value{DecimalOf(4, 0)}); err != nil || out.IsQuery() {
		t.Errorf("a CALL of no outputs gave %+v, %v", out, err)
	}
}

// prepareCall prepares sql, a CALL, checking its parameters and columns.
func prepareCall(t *testing.T, c *Conn, sql string, parameters int, columns []string) *Statement {
	t.Helper()

	st, err := c.Prepare(context.Background(), sql)
	if err != nil {
		t.Fatal(err)
	}

	names := make([]string, 0, len(st.Columns()))
	for _, column := range st.Columns() {
		names = append(names, column.Name)
	}

	if len(st.Parameters()) != parameters || !slices.Equal(names, columns) {
		t.Errorf("%s: %d parameters, columns %v", sql, len(st.Parameters()), names)
	}

	return st
}

// assertCall executes the CALL st with value, checking the row of its
// outputs.
func assertCall(t *testing.T, c *Conn, st *Statement, value Value, want string) {
	t.Helper()

	out, err := c.Execute(context.Background(), st, []Value{value})
	if err != nil || len(out.Rows) != 1 || valueText(out.Rows[0]) != want {
		t.Errorf("the CALL gave %+v, %v", out, err)
	}
}
