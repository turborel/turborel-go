package turborel

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// complete replies a Complete of SQLCODE 0 and sqlstate, active as
// said.
func (f *fake) complete(sqlstate string, active bool) {
	f.reply(kindComplete, func(w *writer) {
		w.u32(0)
		w.str(sqlstate)
		w.flag(active)
	})
}

// openGranted opens a scripted connection granted capabilities, then
// running script.
func openGranted(t *testing.T, capabilities []string, script func(f *fake)) *Conn {
	t.Helper()

	c, err := openFake(t, Trust{}, quickConfig(DefaultOffer()), func(f *fake) {
		f.request()
		f.ready(capabilities...)
		script(f)
	})
	if err != nil {
		t.Fatal(err)
	}

	return c
}

// TestConversationOrder reads a statement's replies in their order.
//
// Verifies: [PROTOCOL Conversations] a statement's replies come Rows
// first, then Notices, then Statuses, then its last reply; a cursor read
// in parts ends with Suspended, then Complete; the server's own notice is
// told as a Notice [PROTOCOL Errors and notices].
func TestConversationOrder(t *testing.T) {
	t.Parallel()

	c := openGranted(t, []string{CapabilityNotices, CapabilityStatus}, func(f *fake) {
		f.request()
		f.reply(kindDescription, func(w *writer) { w.u32(1); Column{Type: BooleanType{}}.encode(w) })
		f.reply(kindRows, func(w *writer) { w.u32(1); encodeValues(w, []Value{Bool(true)}) })
		f.reply(kindNotice, func(w *writer) { w.str("01000"); w.str("told") })
		f.reply(kindStatus, func(w *writer) { w.str(StatusReadOnly); w.str("YES") })
		f.complete("01000", true)
	})

	o, err := c.Query(context.Background(), "VALUES TRUE")
	if err != nil || len(o.Rows) != 1 || o.Rows[0][0] != Bool(true) {
		t.Fatalf("Query gave %+v, %v", o, err)
	}

	if s, _ := c.Setting(StatusReadOnly); s != "YES" || len(c.TakeNotices()) != 1 || !c.InTransaction() {
		t.Errorf("status %q, active %v", s, c.InTransaction())
	}

	if w, ok := c.Warning(); !ok || w != "01000" {
		t.Errorf("warning %q", w)
	}
}

// TestStatementsBroken loses a connection whose server breaks off a
// statement's replies.
//
// Verifies: [PROTOCOL Conversations] a reply out of its place in a
// statement's conversation loses the connection: a Pong for a query's
// result, a Ready among its rows, Rows for a completion, a Complete for
// Prepared or a Description.
func TestStatementsBroken(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		run  func(c *Conn) error
		send func(f *fake)
	}{
		{"result", func(c *Conn) error { _, err := c.Query(context.Background(), "x"); return err },
			func(f *fake) { f.reply(kindPong, nil) }},
		{"rows", func(c *Conn) error { _, err := c.Query(context.Background(), "x"); return err },
			func(f *fake) { f.reply(kindDescription, func(w *writer) { w.u32(0) }); f.ready() }},
		{"stream", func(c *Conn) error { _, err := c.QueryInto(context.Background(), "x", &collector{}); return err },
			func(f *fake) { f.reply(kindPong, nil) }},
		{"stream rows", func(c *Conn) error { _, err := c.QueryInto(context.Background(), "x", &collector{}); return err },
			func(f *fake) { f.reply(kindDescription, func(w *writer) { w.u32(0) }); f.ready() }},
		{"prepared", func(c *Conn) error { _, err := c.Prepare(context.Background(), "x"); return err },
			func(f *fake) { f.complete("00000", false) }},
		{"completion", func(c *Conn) error { return c.Reset(context.Background()) },
			func(f *fake) { f.reply(kindRows, func(w *writer) { w.u32(0) }) }},
		{"description", func(c *Conn) error { _, err := c.DescribeCursor(context.Background(), &Statement{}, 1); return err },
			func(f *fake) { f.complete("00000", false) }},
		{"ping", func(c *Conn) error { return c.Ping(context.Background()) },
			func(f *fake) { f.complete("00000", false) }},
	}
	for _, tc := range cases {
		c := openGranted(t, Capabilities(), func(f *fake) {
			f.request()
			tc.send(f)
		})

		if err := tc.run(c); err == nil || !c.IsLost() {
			t.Errorf("%s: %v, lost %v", tc.name, err, c.IsLost())
		}
	}
}

// TestStatementsFailed gives each statement's failure as the server sent
// it, the connection going on.
//
// Verifies: [CLIENT Running statements] a statement's failure is the
// server's Error, its SQLCODE and SQLSTATE; the connection goes on after
// it [PROTOCOL Error 'E'].
func TestStatementsFailed(t *testing.T) {
	t.Parallel()

	runs := []func(c *Conn) error{
		func(c *Conn) error { _, err := c.Query(context.Background(), "x"); return err },
		func(c *Conn) error { _, err := c.QueryInto(context.Background(), "x", &collector{}); return err },
		func(c *Conn) error { _, err := c.Prepare(context.Background(), "x"); return err },
		func(c *Conn) error {
			_, err := c.Execute(context.Background(), &Statement{columns: []Column{{}}}, nil)
			return err
		},
		func(c *Conn) error { _, err := c.Execute(context.Background(), &Statement{}, nil); return err },
		func(c *Conn) error { _, err := c.OpenCursor(context.Background(), &Statement{}, nil, 1); return err },
		func(c *Conn) error { _, err := c.Fetch(context.Background(), &Statement{}, 1); return err },
		func(c *Conn) error { _, err := c.Describe(context.Background(), &Statement{}); return err },
		func(c *Conn) error { _, err := c.DescribeCursor(context.Background(), &Statement{}, 0); return err },
		func(c *Conn) error { return c.Ping(context.Background()) },
	}
	for i, run := range runs {
		c := openGranted(t, Capabilities(), func(f *fake) {
			f.request()
			f.failure(-103, "42000")
		})

		err := run(c)
		assertState(t, err, "42000")

		if c.IsLost() {
			t.Errorf("run %d lost the connection", i)
		}
	}
}

// TestStatementsNeedCapabilities fails before sending what needs a
// capability not granted.
//
// Verifies: [CLIENT What a connection asks for] a method that needs a
// capability not granted fails before it sends anything: describe,
// numbered cursors, reset, change-user, multi-statement and
// query-attributes [PROTOCOL Capabilities].
func TestStatementsNeedCapabilities(t *testing.T) {
	t.Parallel()

	c := openGranted(t, nil, func(*fake) {})
	st := &Statement{}
	runs := []func() error{
		func() error { _, err := c.Describe(context.Background(), st); return err },
		func() error { _, err := c.DescribeCursor(context.Background(), st, 0); return err },
		func() error { _, err := c.OpenCursorNumbered(context.Background(), st, 1, nil, 0); return err },
		func() error { _, err := c.FetchNumbered(context.Background(), st, 1, 0); return err },
		func() error { return c.CloseCursorNumbered(context.Background(), st, 1) },
		func() error { return c.Reset(context.Background()) },
		func() error { return c.ChangeUser(context.Background(), "B", Trust{}) },
		func() error { _, err := c.QueryMany(context.Background(), "x"); return err },
		func() error { return c.SetQueryAttributes([]Attribute{{Name: "a"}}) },
	}
	for i, run := range runs {
		assertBroken(t, run(), "did not grant")

		if c.IsLost() {
			t.Fatalf("run %d sent something", i)
		}
	}

	if err := c.SetQueryAttributes(nil); err != nil || c.requestAttributes() != nil {
		t.Errorf("no attributes: %v", err)
	}

	if _, ok := c.Warning(); ok {
		t.Error("a warning before any statement")
	}
}

// TestCompletionTold reads what the last completion told.
//
// Verifies: [CLIENT What the server tells] the rows a statement counted,
// the identity values it generated, the server's time, the commit's
// position and the conditions it raised, from its Complete's tail; none
// of them before a statement [PROTOCOL Complete 'K'].
func TestCompletionTold(t *testing.T) {
	t.Parallel()

	condition := &Error{SQLState: "01003", Message: "nulls"}
	c := openGranted(t, Capabilities(), func(f *fake) {
		f.request()
		f.reply(kindComplete, func(w *writer) {
			w.u32(0)
			w.str("00000")
			w.flag(false)
			w.u8(tailPosition)
			w.u64(4711)
			w.u8(tailRows)
			w.u64(2)
			w.u8(tailGenerated)
			w.strs([]string{"3", "4"})
			w.u8(tailConditions)
			w.u32(1)
			w.bytes(body(func(inner *writer) { inner.failure(condition) }))
			w.u8(tailTime)
			w.u64(180)
			w.u64(4)
			w.u64(0)
		})
	})

	if _, ok := c.RowsAffected(); ok || c.LastGenerated() != nil || c.ServerTime() != nil || c.Conditions() != nil {
		t.Error("told before any statement")
	}

	if _, ok := c.CommitPosition(); ok {
		t.Error("a position before any commit")
	}

	if _, err := c.Query(context.Background(), "INSERT"); err != nil {
		t.Fatal(err)
	}

	rows, _ := c.RowsAffected()
	position, _ := c.CommitPosition()

	if rows != 2 || position != 4711 || len(c.LastGenerated()) != 2 || c.ServerTime().Total != 180 ||
		c.Conditions()[0].SQLState != "01003" {
		t.Errorf("told rows %d, position %d", rows, position)
	}
}

// TestReplicaBehind gives the failure of a replica that had not applied
// the position asked for.
//
// Verifies: [PROTOCOL Errors and notices] SQLSTATE TBR01, SQLCODE -920, a
// replica that had not applied REPLICATION.READ_AFTER's position in time,
// is the statement's failure; the connection goes on.
func TestReplicaBehind(t *testing.T) {
	t.Parallel()

	c := openGranted(t, nil, func(f *fake) {
		f.request()
		f.failure(-920, "TBR01")
	})

	_, err := c.Query(context.Background(), "VALUES 1")

	var failure *Error
	if !errors.As(err, &failure) || failure.SQLCode != -920 || failure.SQLState != "TBR01" || c.IsLost() {
		t.Errorf("err = %v, lost %v", err, c.IsLost())
	}
}

// TestChangeUserSteps answers ChangeUser's challenges until Ready.
//
// Verifies: [PROTOCOL ChangeUser 'U'] ChangeUser sends the user, the
// mechanism and its first bytes, then answers each Challenge as Hello's
// exchange does; Ready renews the authorization and the cancel key, and
// the session, with no transaction active, as Reset renews it [CLIENT
// Controlling the session] (RULES.md D8); a mechanism that asks refuses a
// server that did not prove itself.
func TestChangeUserSteps(t *testing.T) {
	t.Parallel()

	mechanism := &scripted{answers: [][]byte{[]byte("a")}}
	c := openGranted(t, []string{CapabilityChangeUser}, func(f *fake) {
		f.request()
		f.complete("00000", true)

		if kind, _ := f.request(); kind != kindChangeUser {
			t.Errorf("ChangeUser came as %c", kind)
		}

		f.reply(kindChallenge, func(w *writer) { w.bytes([]byte("c")) })
		f.request()
		f.reply(kindReady, func(w *writer) {
			w.str("BOB")
			w.buf = append(w.buf, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16)
			w.strs(nil)
			w.str("turborel 0.0.0 (alpha)")
		})
	})

	if _, err := c.Query(context.Background(), "INSERT INTO T VALUES (1)"); err != nil || !c.InTransaction() {
		t.Fatalf("the statement left no transaction active: %v", err)
	}

	err := c.ChangeUser(context.Background(), "BOB", mechanism)
	if err != nil || c.Authorization() != "BOB" || c.key[0] != 1 || c.InTransaction() {
		t.Errorf("ChangeUser: %v, %q, active %v", err, c.Authorization(), c.InTransaction())
	}

	refusing := openGranted(t, []string{CapabilityChangeUser}, func(f *fake) {
		f.request()
		f.ready()
	})

	err = refusing.ChangeUser(context.Background(), "BOB", &scripted{verified: errScripted})
	if err == nil || !refusing.IsLost() {
		t.Errorf("an unproved server: %v", err)
	}
}

// TestQueryManyLost gives the connection's failure, not the statement's,
// when a text of several statements loses it.
//
// Verifies: [CLIENT Controlling the session] the outcomes of a text of
// several statements are given until one fails; a failure that loses the
// connection is the call's own.
func TestQueryManyLost(t *testing.T) {
	t.Parallel()

	c := openGranted(t, []string{CapabilityMultiStatement}, func(f *fake) {
		f.request()
		f.reply(kindComplete, func(w *writer) {
			w.u32(0)
			w.str("00000")
			w.flag(false)
			w.u8(tailMore)
		})
		f.failure(sqlcodeConnection, sqlstateConnectionFailure)
	})

	if ran, err := c.QueryMany(context.Background(), "A; B"); err == nil || ran != nil {
		t.Errorf("ran %+v, %v", ran, err)
	}
}

// TestContextOverOwnStream gives up the waits of a connection over a
// program's stream as its context is done.
//
// Verifies: [DESIGN] D2 a connection opened over a program's stream, which
// cannot reach its server again to cancel, gives its call up as the
// context is done, the connection lost, the failure caused by the
// context.
func TestContextOverOwnStream(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	c := openGranted(t, nil, func(f *fake) {
		f.request()
		cancel()
	})

	_, err := c.Query(ctx, "VALUES 1")
	if !errors.Is(err, context.Canceled) || !c.IsLost() {
		t.Errorf("err = %v, lost %v", err, c.IsLost())
	}
}

// TestXMLNodes parses an XML value's serialization for its nodes.
//
// Verifies: [CLIENT XML values] an XML value's nodes are parsed from its
// serialization as CONTENT, its white space kept: elements with their
// attributes and children, text, comments and processing instructions;
// text that is no XML content is refused (SQLSTATE 2200N).
func TestXMLNodes(t *testing.T) {
	t.Parallel()

	nodes, err := XML(`<?pi x?><a n="1"> <b/>text</a><!--c-->tail`).Nodes()
	if err != nil || len(nodes) != 4 {
		t.Fatalf("nodes %#v, %v", nodes, err)
	}

	a, ok := nodes[1].(XMLElement)
	if !ok || a.Name.Local != "a" || len(a.Attributes) != 1 || len(a.Children) != 3 || a.Children[0] != XMLText(" ") {
		t.Errorf("element %#v", nodes[1])
	}

	if nodes[0] != (XMLProcInst{Target: "pi", Inst: "x"}) || nodes[2] != XMLComment("c") || nodes[3] != XMLText("tail") {
		t.Errorf("nodes %#v", nodes)
	}

	for _, bad := range []string{"<a>", "<a></b>", "<a x=>"} {
		_, err := XML(bad).Nodes()
		assertState(t, err, sqlstateInvalidXMLContent)
	}

	if !strings.Contains(XML("<x/>").Text(), "x") {
		t.Error("Text misread")
	}
}
