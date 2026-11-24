package turborel

import (
	"bytes"
	"reflect"
	"testing"
)

// body builds a message's body by write.
func body(write func(w *writer)) []byte {
	var w writer

	write(&w)

	return w.buf
}

// mustReply decodes a reply, failing the test if it fails.
func mustReply(t *testing.T, kind byte, b []byte) reply {
	t.Helper()

	rep, err := decodeReply(kind, b)
	if err != nil {
		t.Fatalf("decoding %c % X: %v", kind, b, err)
	}

	return rep
}

// replyAs decodes a reply that must be of type T.
func replyAs[T reply](t *testing.T, kind byte, b []byte) T {
	t.Helper()

	rep, ok := mustReply(t, kind, b).(T)
	if !ok {
		t.Fatalf("decoded %#v", rep)
	}

	return rep
}

// TestRepliesDecoded decodes a reply of every kind.
//
// Verifies: [PROTOCOL Replies] Challenge, Ready, Prepared, Description,
// Rows, Suspended, Error, Lob, Notice, Status and Pong are each read from
// their fields as the spec lays them out.
func TestRepliesDecoded(t *testing.T) {
	t.Parallel()

	for _, c := range replyCases() {
		if got := mustReply(t, c.kind, c.body); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%c read %#v, want %#v", c.kind, got, c.want)
		}
	}
}

// replyCase is a reply's kind, its body, and what it decodes to.
type replyCase struct {
	kind byte
	body []byte
	want reply
}

// replyCases are a reply of every kind, with what each decodes to.
func replyCases() []replyCase {
	key := [keyBytes]byte{1, 15: 16}
	column := Column{Named: true, Name: "ID", Type: ExactType{Precision: 9}, NotNull: true}

	return []replyCase{
		{kindChallenge, body(func(w *writer) { w.bytes([]byte("r=abc")) }), challengeReply{challenge: []byte("r=abc")}},
		{kindReady, body(func(w *writer) {
			w.str("ANN")
			w.buf = append(w.buf, key[:]...)
			w.strs([]string{"ping"})
			w.str("turborel 0.0.0 (alpha)")
		}), readyReply{authorization: "ANN", key: key, capabilities: []string{"ping"}, server: "turborel 0.0.0 (alpha)"}},
		{kindPrepared, body(func(w *writer) {
			w.u32(1)
			w.u32(1)
			w.flag(true)
			VaryingType{Length: 1 << 20}.encodeType(w)
			w.u32(1)
			column.encode(w)
		}), preparedReply{
			statement:  1,
			parameters: []Parameter{{Varying: true, Type: VaryingType{Length: 1 << 20}}},
			columns:    []Column{column},
		}},
		{kindDescription, body(func(w *writer) {
			w.u32(1)
			Column{Type: BooleanType{}}.encode(w)
		}), descriptionReply{columns: []Column{{Type: BooleanType{}}}}},
		{kindRows, body(func(w *writer) { w.u32(0) }), rowsReply{}},
		{kindSuspended, nil, suspendedReply{}},
		{kindLobReply, []byte{lobData, 2, 0, 0, 0, 'h', 'i'}, lobDataReply{data: []byte("hi")}},
		{kindLobReply, []byte{lobLocator, 9, 0, 0, 0}, lobLocatorReply{locator: 9}},
		{kindStatus, body(func(w *writer) {
			w.str("SCHEMA")
			w.str("ANN")
		}), statusReply{name: "SCHEMA", value: "ANN"}},
		{kindPong, nil, pongReply{}},
		{kindNotice, body(func(w *writer) {
			w.str("01T01")
			w.str("rolled back")
			w.u8(tailSeverity)
			w.str("NOTICE")
		}), noticeReply{notice: Notice{
			SQLState: "01T01", Message: "rolled back", Diagnostics: Diagnostics{Severity: "NOTICE"},
		}}},
	}
}

// TestReplyErrorFailure reads an Error's failure whole.
//
// Verifies: [PROTOCOL Composite fields] a failure is its SQLCODE `i32 as
// u32`, SQLSTATE, message, the rule if cited, the position if placed,
// the active flag, then its tail 7 to 15, every diagnostic in tag order.
func TestReplyErrorFailure(t *testing.T) {
	t.Parallel()

	want := &Error{
		SQLCode: -103, SQLState: "42000", Message: "table ANN.NOPE is not defined",
		Rule: "ISO/IEC 9075:1992 §6.3 SR1", Position: 14, Placed: true, Active: true,
		Diagnostics: &Diagnostics{
			Severity: "ERROR", Detail: "d", Hint: "h", Context: "trigger S.T", Schema: "ANN",
			Table: "NOPE", Column: "C", DataType: "INTEGER", Constraint: "K",
		},
	}

	failure := replyAs[errorReply](t, kindError, body(func(w *writer) { w.failure(want) })).failure
	if !reflect.DeepEqual(failure, want) {
		t.Errorf("read %#v, want %#v", failure, want)
	}

	plain := &Error{SQLCode: -919, SQLState: "08006", Message: "m"}

	failure = replyAs[errorReply](t, kindError, body(func(w *writer) { w.failure(plain) })).failure
	if !reflect.DeepEqual(failure, plain) || failure.Error() != "SQLCODE -919 (SQLSTATE 08006): m" {
		t.Errorf("read %#v", failure)
	}

	line := "SQLCODE -103 (SQLSTATE 42000): table ANN.NOPE is not defined [ISO/IEC 9075:1992 §6.3 SR1] (at character 14)"
	if got := want.Error(); got != line {
		t.Errorf("Error() = %q", got)
	}
}

// TestReplyCompleteExample reads the worked example's Complete.
//
// Verifies: [PROTOCOL A worked example] the Complete of 14 bytes, SQLCODE
// 0, SQLSTATE 00000, active 0, with an empty tail; and with `row-counts`
// its tag 6 and row count, then tag 18 and the server's three times.
func TestReplyCompleteExample(t *testing.T) {
	t.Parallel()

	frame := []byte{0x4B, 0x0E, 0x00, 0x00, 0x00, 0, 0, 0, 0, 5, 0, 0, 0, '0', '0', '0', '0', '0', 0}

	h, err := decodeFrameHeader([frameHeaderBytes]byte(frame), maxBody)
	if err != nil || h.kind != kindComplete || int(h.length) != len(frame)-frameHeaderBytes {
		t.Fatalf("header %+v, %v", h, err)
	}

	c := replyAs[completeReply](t, kindComplete, frame[frameHeaderBytes:]).completion
	if !reflect.DeepEqual(c, Completion{SQLState: "00000"}) {
		t.Errorf("read %#v", c)
	}

	counted := append(append([]byte{}, frame[frameHeaderBytes:]...), 6, 1, 0, 0, 0, 0, 0, 0, 0)
	counted = append(counted, 18)
	counted = append(counted, body(func(w *writer) { w.u64(180); w.u64(4); w.u64(0) })...)

	c = replyAs[completeReply](t, kindComplete, counted).completion
	if !c.Counted || c.Rows != 1 || c.Time == nil || *c.Time != (Timing{Total: 180, Queued: 4}) {
		t.Errorf("read %#v", c)
	}
}

// TestReplyCompleteTail reads every field of a Complete's tail.
//
// Verifies: [PROTOCOL The tail] a Complete takes tags 4 position, 5 more,
// 6 rows, 16 generated, 17 conditions, each a failure as bytes, and 18
// time, in increasing order.
func TestReplyCompleteTail(t *testing.T) {
	t.Parallel()

	condition := &Error{
		SQLCode: 0, SQLState: "01003", Message: "nulls eliminated", Diagnostics: &Diagnostics{Severity: "WARNING"},
	}
	b := body(func(w *writer) {
		w.u32(unsigned32(100))
		w.str("02000")
		w.flag(true)
		w.u8(tailPosition)
		w.u64(4711)
		w.u8(tailMore)
		w.u8(tailRows)
		w.u64(3)
		w.u8(tailGenerated)
		w.strs([]string{"3", "4"})
		w.u8(tailConditions)
		w.u32(1)
		w.bytes(body(func(inner *writer) { inner.failure(condition) }))
	})

	c := replyAs[completeReply](t, kindComplete, b).completion
	want := Completion{
		SQLCode: 100, SQLState: "02000", Active: true, Position: 4711, Committed: true, More: true,
		Rows: 3, Counted: true, Generated: []string{"3", "4"}, Conditions: []*Error{condition},
	}

	if !reflect.DeepEqual(c, want) {
		t.Errorf("read %#v, want %#v", c, want)
	}
}

// TestReplyTailRefused refuses a tail out of order, or of a tag the
// message does not take.
//
// Verifies: [PROTOCOL The tail] a tag the message does not take, or one
// not past the last, fails the connection; so does a condition whose own
// bytes are malformed.
func TestReplyTailRefused(t *testing.T) {
	t.Parallel()

	head := body(func(w *writer) {
		w.u32(0)
		w.str("00000")
		w.flag(false)
	})
	cases := map[string][]byte{
		"out of order": append(append([]byte{}, head...),
			tailRows, 0, 0, 0, 0, 0, 0, 0, 0, tailPosition, 0, 0, 0, 0, 0, 0, 0, 0),
		"repeated":       append(append([]byte{}, head...), tailMore, tailMore),
		"not taken":      append(append([]byte{}, head...), tailCursor, 0, 0, 0, 0),
		"bad condition":  append(append([]byte{}, head...), tailConditions, 1, 0, 0, 0, 1, 0, 0, 0, 0),
		"short position": append(append([]byte{}, head...), tailPosition, 1),
	}
	for name, b := range cases {
		_, err := decodeReply(kindComplete, b)
		if err == nil {
			t.Errorf("%s: decoded", name)
		}
	}

	_, err := decodeReply(kindComplete, cases["not taken"])
	assertBroken(t, err, "its tail has tag 3 out of place")

	notice := body(func(w *writer) {
		w.str("01000")
		w.str("m")
		w.u8(tailDetail)
		w.str("d")
		w.u8(tailSeverity)
		w.str("WARNING")
	})
	_, err = decodeReply(kindNotice, notice)
	assertBroken(t, err, "out of place")
}

// TestReplyRefused refuses a kind no reply has, and a reply with bytes
// left over.
//
// Verifies: [PROTOCOL Frames] a kind the reader does not know, read in
// the direction it travels, fails the connection, as do bytes left over
// after the last field and a body cut short.
func TestReplyRefused(t *testing.T) {
	t.Parallel()

	_, err := decodeReply('Q', nil)
	assertBroken(t, err, "no reply is of kind 81")

	_, err = decodeReply(kindPong, []byte{0})
	assertBroken(t, err, "left over")

	_, err = decodeReply(kindReady, []byte{0, 0, 0, 0, 1})
	assertBroken(t, err, "cut short")

	_, err = decodeReply(kindLobReply, []byte{2})
	assertBroken(t, err, "no Lob reply is of this kind")
}

// TestReplyColumnUnnamed reads a column with no name.
//
// Verifies: [PROTOCOL Composite fields] a column is its named flag, its
// name only if named, its data type and its not null flag.
func TestReplyColumnUnnamed(t *testing.T) {
	t.Parallel()

	b := body(func(w *writer) { Column{Type: UUIDType{}}.encode(w) })
	if !bytes.Equal(b, []byte{0, 27, 0}) {
		t.Errorf("wrote % X", b)
	}

	r := newReader(b, "test")
	if c := r.column(); r.finish() != nil || c.Named || c.Type != (UUIDType{}) {
		t.Errorf("read %#v", c)
	}
}

// TestKindsByDirection reads a kind in the direction it travels.
//
// Verifies: [PROTOCOL Frames] the kind is read in the direction it
// travels: `C` from a client is Close, and from the server Challenge.
func TestKindsByDirection(t *testing.T) {
	t.Parallel()

	if got := encodeRequest(closeRequest{statement: 1})[0]; got != 'C' {
		t.Errorf("Close's kind is %c", got)
	}

	if _, ok := mustReply(t, 'C', []byte{0, 0, 0, 0}).(challengeReply); !ok {
		t.Error("C from the server is not a Challenge")
	}
}

// TestValuesExampleRow reads the Rows body of the worked example.
//
// Verifies: [PROTOCOL A worked example] the Rows reply's one row holds
// the exact number 1, units 1 and scale 0, as the example's bytes give
// it.
func TestValuesExampleRow(t *testing.T) {
	t.Parallel()

	body := []byte{
		0x01, 0x00, 0x00, 0x00,
		0x01, 0x00, 0x00, 0x00,
		0x02,
		0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}

	rep, err := decodeReply(kindRows, body)
	if err != nil {
		t.Fatal(err)
	}

	rows, ok := rep.(rowsReply)
	if !ok || len(rows.rows) != 1 || !reflect.DeepEqual(rows.rows[0], Row{DecimalOf(1, 0)}) {
		t.Errorf("read %#v", rep)
	}

	var w writer

	w.frame(kindRows, func() {
		w.u32(1)
		encodeValues(&w, rows.rows[0])
	})

	if len(w.buf) != frameHeaderBytes+len(body) || w.buf[1] != 0x1D {
		t.Errorf("the frame is % X, want its length 29", w.buf)
	}
}
