package turborel

import (
	"bytes"
	"testing"
)

// framed is a frame of kind around the body write builds, its length
// counted by hand.
func framed(kind byte, write func(w *writer)) []byte {
	b := body(write)

	return append(appendFrameHeader(nil, frameHeader{kind: kind, length: count32(len(b))}), b...)
}

// assertRequest checks the bytes a request encodes to.
func assertRequest(t *testing.T, req request, want []byte) {
	t.Helper()

	if got := encodeRequest(req); !bytes.Equal(got, want) {
		t.Errorf("%T wrote\n% X\nwant\n% X", req, got, want)
	}
}

// TestRequestsWorkedExample encodes the worked example's requests byte
// for byte.
//
// Verifies: [PROTOCOL A worked example] the Hello of `trust` as ANN
// asking for `ping` is 41 bytes, with no tail; Ping is a header alone;
// the Query of `VALUES 1` with `ends` set is as the example prints it.
func TestRequestsWorkedExample(t *testing.T) {
	t.Parallel()

	hello := []byte{
		0x48, 0x24, 0x00, 0x00, 0x00,
		0x01, 0x00, 0x00, 0x00,
		0x03, 0x00, 0x00, 0x00, 0x41, 0x4E, 0x4E,
		0x05, 0x00, 0x00, 0x00, 0x74, 0x72, 0x75, 0x73, 0x74,
		0x00, 0x00, 0x00, 0x00,
		0x01, 0x00, 0x00, 0x00,
		0x04, 0x00, 0x00, 0x00, 0x70, 0x69, 0x6E, 0x67,
	}
	assertRequest(t, helloRequest{user: "ANN", mechanism: "trust", capabilities: []string{"ping"}}, hello)

	if len(hello) != 41 {
		t.Errorf("the example's Hello is %d bytes", len(hello))
	}

	assertRequest(t, pingRequest{}, []byte{0x47, 0x00, 0x00, 0x00, 0x00})

	query := []byte{
		0x51, 0x0D, 0x00, 0x00, 0x00,
		0x08, 0x00, 0x00, 0x00, 0x56, 0x41, 0x4C, 0x55, 0x45, 0x53, 0x20, 0x31,
		0x01,
	}
	assertRequest(t, queryRequest{sql: "VALUES 1", ends: true}, query)
}

// TestRequestsTails writes each request's tail only where it says
// something, in increasing order of tag.
//
// Verifies: [PROTOCOL The tail] Hello takes tag 1 attributes, Query tags 1
// attributes and 2 multi, Execute tags 1 and 3 cursor, Fetch and Close
// tag 3; a request with nothing to say has an empty tail, its first form
// exactly.
func TestRequestsTails(t *testing.T) {
	t.Parallel()

	attributes := []Attribute{{Name: "traceparent", Value: "00-1"}}
	putAttributes := func(w *writer) {
		w.u8(tailAttributes)
		w.u32(1)
		w.str("traceparent")
		w.str("00-1")
	}

	hello := helloRequest{user: "U", mechanism: "trust", attributes: attributes}
	assertRequest(t, hello, framed(kindHello, func(w *writer) {
		w.u32(1)
		w.str("U")
		w.str("trust")
		w.bytes(nil)
		w.strs(nil)
		putAttributes(w)
	}))
	assertRequest(t, queryRequest{sql: "S", attributes: attributes, multi: true}, framed(kindQuery, func(w *writer) {
		w.str("S")
		w.flag(false)
		putAttributes(w)
		w.u8(tailMulti)
	}))

	execute := executeRequest{
		statement: 7, values: []Value{DecimalOf(2, 0)}, rows: 100, ends: true, attributes: attributes, cursor: 2,
	}
	assertRequest(t, execute,
		framed(kindExecute, func(w *writer) {
			w.u32(7)
			encodeValues(w, []Value{DecimalOf(2, 0)})
			w.u32(100)
			w.flag(true)
			putAttributes(w)
			w.u8(tailCursor)
			w.u32(2)
		}))
}

// TestRequestsEmptyTails writes no tail where a request has nothing to
// say.
//
// Verifies: [PROTOCOL The tail] a field is written only where it says
// something, so a request with an empty tail is its first form exactly;
// Fetch and Close take tag 3, cursor.
func TestRequestsEmptyTails(t *testing.T) {
	t.Parallel()

	assertRequest(t, executeRequest{statement: 7}, framed(kindExecute, func(w *writer) {
		w.u32(7)
		w.u32(0)
		w.u32(0)
		w.flag(false)
	}))
	assertRequest(t, fetchRequest{statement: 7, rows: 2, cursor: 3}, framed(kindFetch, func(w *writer) {
		w.u32(7)
		w.u32(2)
		w.u8(tailCursor)
		w.u32(3)
	}))
	assertRequest(t, closeRequest{statement: 7}, framed(kindClose, func(w *writer) { w.u32(7) }))
	assertRequest(t, closeRequest{statement: 7, cursor: 1}, framed(kindClose, func(w *writer) {
		w.u32(7)
		w.u8(tailCursor)
		w.u32(1)
	}))
}

// TestRequestsFields encodes every other request from its fields.
//
// Verifies: [PROTOCOL Requests] Answer, Prepare, Deallocate, Lob's five
// operations, Cancel, Terminate, Describe, Reset, ChangeUser, CopyIn,
// CopyData and CopyEnd each write their kind and their fields in the
// order the spec lists them.
func TestRequestsFields(t *testing.T) {
	t.Parallel()

	key := [keyBytes]byte{1, 2, 15: 9}
	niklaus := copyDataOf([]Row{{DecimalOf(7, 0), Char("Niklaus")}, {Null{}, Null{}}})
	cases := []struct {
		req  request
		want []byte
	}{
		{answerRequest{answer: []byte("c=biws")}, framed(kindAnswer, func(w *writer) { w.bytes([]byte("c=biws")) })},
		{prepareRequest{statement: 1, sql: "SELECT ID FROM T"}, framed(kindPrepare, func(w *writer) {
			w.u32(1)
			w.str("SELECT ID FROM T")
		})},
		{deallocateRequest{statement: 1}, framed(kindDeallocate, func(w *writer) { w.u32(1) })},
		{lobReadRequest{locator: 4, from: 10, count: 262144, characters: true}, framed(kindLobRequest, func(w *writer) {
			w.u8(lobRead)
			w.u32(4)
			w.u64(10)
			w.u64(262144)
			w.flag(true)
		})},
		{lobCreateRequest{kind: BinaryLOB}, []byte{'L', 2, 0, 0, 0, lobCreate, 1}},
		{lobAppendRequest{locator: 4, octets: []byte{7}}, []byte{'L', 10, 0, 0, 0, lobAppend, 4, 0, 0, 0, 1, 0, 0, 0, 7}},
		{lobFreeRequest{locator: 4}, []byte{'L', 5, 0, 0, 0, lobFree, 4, 0, 0, 0}},
		{lobHoldRequest{locator: 4}, []byte{'L', 5, 0, 0, 0, lobHold, 4, 0, 0, 0}},
		{cancelRequest{key: key}, append([]byte{'Z', 16, 0, 0, 0}, key[:]...)},
		{terminateRequest{}, []byte{'X', 0, 0, 0, 0}},
		{describeRequest{statement: 3}, []byte{'Y', 9, 0, 0, 0, 0, 3, 0, 0, 0, 0, 0, 0, 0}},
		{describeRequest{ofCursor: true, statement: 3, cursor: 2}, []byte{'Y', 9, 0, 0, 0, 1, 3, 0, 0, 0, 2, 0, 0, 0}},
		{resetRequest{}, []byte{'W', 0, 0, 0, 0}},
		{changeUserRequest{user: "BOB", mechanism: "trust"}, framed(kindChangeUser, func(w *writer) {
			w.str("BOB")
			w.str("trust")
			w.bytes(nil)
		})},
		{copyInRequest{table: "T", columns: []string{"ID", "NAME"}, ends: true}, framed(kindCopyIn, func(w *writer) {
			w.str("T")
			w.strs([]string{"ID", "NAME"})
			w.flag(true)
		})},
		{niklaus, framed(kindCopyData, func(w *writer) {
			w.u32(2)
			encodeValues(w, []Value{DecimalOf(7, 0), Char("Niklaus")})
			encodeValues(w, []Value{Null{}, Null{}})
		})},
		{copyEndRequest{abandon: true}, []byte{'N', 1, 0, 0, 0, 1}},
	}
	for _, c := range cases {
		assertRequest(t, c.req, c.want)
	}
}
