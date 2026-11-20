package turborel

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// rowOf writes Rows of one row of one value, its bytes value.
func rowOf(value []byte) func(w *writer) {
	return func(w *writer) {
		w.u32(1)
		w.u32(1)
		w.buf = append(w.buf, value...)
	}
}

// jsonOnWire is a JSON value's bytes on the wire, of document.
func jsonOnWire(document []byte) []byte {
	return append(binary.LittleEndian.AppendUint32([]byte{tagJSON}, count32(len(document))), document...)
}

// tooDeep is a JSON document of arrays nested past 128.
func tooDeep() []byte {
	const node = 13

	deep := make([]byte, 0, 1+node*(maxJSONNesting+1))
	deep = append(deep, jsonVersion)

	for range maxJSONNesting + 1 {
		deep = append(deep, jsonArray, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	}

	return deep
}

// characterColumn writes a Description of one CHARACTER column whose
// units byte is units.
func characterColumn(units byte) func(w *writer) {
	return func(w *writer) {
		w.u32(1)
		w.flag(true)
		w.str("C")
		w.u8(tagCharacter)
		w.u32(1)
		w.u8(units)
		w.flag(false)
	}
}

// badCondition writes a Complete whose one condition is malformed, saying
// more replies follow if more, a transaction active if active.
func badCondition(more, active bool) func(w *writer) {
	return func(w *writer) {
		w.u32(0)
		w.str("00000")
		w.flag(active)
		w.mark(tailMore, more)
		w.u8(tailConditions)
		w.u32(1)
		w.bytes([]byte{0xFF})
	}
}

// step is a reply a scripted server sends.
type step struct {
	kind byte
	body func(w *writer)
}

// badThenOwn runs a statement answered by replies, then one answered by
// its own Complete of no data: the first fails, of no class 08, and the
// second takes its own reply.
func badThenOwn(t *testing.T, what string, replies []step) {
	t.Helper()

	c := openGranted(t, nil, func(f *fake) {
		f.request()

		for _, s := range replies {
			f.reply(s.kind, s.body)
		}

		f.request()
		f.complete("02000", false)
	})

	_, err := c.Query(context.Background(), "SELECT V FROM T")
	if failure, ok := errors.AsType[*Error](err); !ok || endsConnection(failure) || c.IsLost() {
		t.Errorf("%s: %v", what, err)
	}

	if out, err := c.Query(context.Background(), "DELETE FROM T"); err != nil || out.Completion.SQLState != "02000" {
		t.Errorf("%s: the next statement took %v, %v", what, out, err)
	}
}

// TestBadValueReadPast fails a statement whose reply holds a value the
// driver cannot take, and reads past the rest of its replies.
//
// Verifies: [PROTOCOL Values and fields] a value whose body its reader
// cannot take leaves its frame read whole and the stream in step: damaged
// JSON (SQLSTATE TB916), JSON nested past 128 (0A000), a truth byte not 0
// or 1, a units byte not 0 or 1, a malformed condition, and value tags 18
// and 19 (RULES.md D8) each fail the statement with the value's own
// failure, of no class 08, the rest of its replies read past to its last,
// a Complete that says more follow among them, so the next statement
// takes its own reply [CLIENT Running statements].
func TestBadValueReadPast(t *testing.T) {
	t.Parallel()

	description := step{kindDescription, func(w *writer) { encodeColumns(w, nil) }}
	end := step{kindComplete, func(w *writer) { w.u32(0); w.str("00000"); w.flag(false) }}
	truth := step{kindRows, rowOf([]byte{tagBoolean, 1})}

	cases := map[string][]step{
		"damaged JSON":  {description, {kindRows, rowOf(jsonOnWire([]byte{jsonVersion, 0xEE}))}, truth, end},
		"JSON too deep": {description, {kindRows, rowOf(jsonOnWire(tooDeep()))}, end},
		"truth 2":       {description, {kindRows, rowOf([]byte{tagBoolean, 2})}, end},
		"units 7":       {{kindDescription, characterColumn(7)}, truth, end},
		"tag 18":        {description, {kindRows, rowOf([]byte{tagCLOB, 0})}, end},
		"tag 19":        {description, {kindRows, rowOf([]byte{tagBLOB, 0})}, end},
		"condition":     {{kindComplete, badCondition(false, false)}},
		"more to come":  {{kindComplete, badCondition(true, false)}, end},
	}

	for what, replies := range cases {
		badThenOwn(t, what, replies)
	}

	_, err := decodeReply(kindRows, append([]byte{1, 0, 0, 0, 1, 0, 0, 0}, jsonOnWire(tooDeep())...))
	assertFailure(t, err, sqlcodeBeyond, sqlstateFeatureNotSupported, "nest too deep")

	_, err = decodeReply(kindRows, []byte{1, 0, 0, 0, 1, 0, 0, 0, tagBoolean, 2})
	assertFailure(t, err, sqlcodeCorrupt, sqlstateCorrupt, "2 is not a truth value")
}

// TestBadValueOfCursorAndDescribe reads past only a request's own
// replies.
//
// Verifies: [PROTOCOL Values and fields] a cursor's batch holding a bad
// value is read past up to its Suspended, and the statement after takes
// its own reply; a Describe of a cursor, answered by a Description alone,
// reads nothing past it; a Complete with a bad condition still tells
// whether a transaction is active [CLIENT Running statements].
func TestBadValueOfCursorAndDescribe(t *testing.T) {
	t.Parallel()

	c := openGranted(t, []string{CapabilityDescribe}, func(f *fake) {
		f.sqlOf()
		f.preparedOf(nil, []Column{{Named: true, Name: "V", Type: BooleanType{}}})
		f.expect(kindExecute)
		f.reply(kindRows, rowOf([]byte{tagBoolean, 3}))
		f.reply(kindSuspended, nil)
		f.expect(kindDescribe)
		f.reply(kindDescription, characterColumn(9))
		f.expect(kindQuery)
		f.complete("02000", false)
		f.expect(kindQuery)
		f.reply(kindComplete, badCondition(false, true))
	})

	ctx := context.Background()

	st, err := c.Prepare(ctx, "SELECT V FROM T")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := c.OpenCursor(ctx, st, nil, 1); err == nil || c.IsLost() {
		t.Errorf("a bad batch gave %v", err)
	}

	if _, err := c.DescribeCursor(ctx, st, 0); err == nil || c.IsLost() {
		t.Errorf("a bad Description gave %v", err)
	}

	if out, err := c.Query(ctx, "DELETE FROM T"); err != nil || out.Completion.SQLState != "02000" {
		t.Errorf("the next statement took %v, %v", out, err)
	}

	if _, err := c.Query(ctx, "COMMIT"); err == nil || !c.InTransaction() {
		t.Errorf("a bad condition gave %v, active %v", err, c.InTransaction())
	}
}

// TestMalformedFrameLoses loses the connection of a malformed frame.
//
// Verifies: [PROTOCOL Values and fields] a malformed frame, a flag
// neither 0 nor 1 or bytes after its last field, loses the connection
// (SQLSTATE 08006), and the next statement is told so.
func TestMalformedFrameLoses(t *testing.T) {
	t.Parallel()

	for what, body := range map[string]func(w *writer){
		"a flag of 2":         func(w *writer) { w.u32(0); w.str("00000"); w.u8(2) },
		"a byte past its end": func(w *writer) { w.u32(0); w.str("00000"); w.flag(false); w.u8(0xFF); w.u8(0) },
	} {
		c := openGranted(t, nil, func(f *fake) {
			f.request()
			f.reply(kindComplete, body)
		})

		_, err := c.Query(context.Background(), "DELETE FROM T")
		assertBroken(t, err, "")

		if _, again := c.Query(context.Background(), "DELETE FROM T"); !c.IsLost() || again == nil {
			t.Errorf("%s: the connection went on", what)
		}
	}
}

// TestTerminateReadsPast reads past a Notice and a Status to Terminate's
// Error.
//
// Verifies: [CLIENT Connecting] Terminate reads past a Notice and a
// Status before the Error that ends it, and gives the Error; a Notice
// before the close ends it well.
func TestTerminateReadsPast(t *testing.T) {
	t.Parallel()

	notice := step{kindNotice, func(w *writer) { w.str("01000"); w.str("a warning") }}
	status := step{kindStatus, func(w *writer) { w.str(StatusReadOnly); w.str("NO") }}
	failing := step{kindError, func(w *writer) {
		w.failure(&Error{SQLCode: sqlcodeConnection, SQLState: "40002", Message: "the commit failed"})
	}}

	for what, replies := range map[string][]step{"error": {notice, status, failing}, "close": {notice}} {
		c := openGranted(t, nil, func(f *fake) {
			f.expect(kindTerminate)

			for _, s := range replies {
				f.reply(s.kind, s.body)
			}
		})

		err := c.Terminate()
		if what == "error" {
			assertState(t, err, "40002")
		} else if err != nil {
			t.Errorf("a notice before the close gave %v", err)
		}
	}
}

// silentServer serves a login granting ping, then reads every request and
// answers none, nor closes, until the test ends.
func silentServer(t *testing.T) net.Conn {
	t.Helper()

	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })

	go func() {
		r := bufio.NewReader(server)
		if _, _, err := readFrame(r, maxBody, false); err != nil {
			return
		}

		var w writer

		w.frame(kindReady, func() {
			w.str("ANN")
			w.buf = append(w.buf, make([]byte, keyBytes)...)
			w.strs([]string{CapabilityPing})
			w.str("turborel 0.0.0 (alpha)")
		})

		if _, err := server.Write(w.buf); err != nil {
			return
		}

		_, _ = io.Copy(io.Discard, r)
	}()

	return client
}

// TestBoundedStreamOpened bounds a program's stream that takes
// deadlines.
//
// Verifies: [CLIENT Timeouts] a stream of the program's own that bounds
// itself, opened by Open, is bounded as its Timeouts say, its read
// timeout settable as a connection's; Terminate on it waits at most its
// read timeout for a server that never answers nor closes, told the
// server did not answer in time [CLIENT Connecting].
func TestBoundedStreamOpened(t *testing.T) {
	t.Parallel()

	const short = 50 * time.Millisecond

	c, err := Open(context.Background(), silentServer(t), "ANN", Trust{}, quickConfig(DefaultOffer()))
	if err != nil {
		t.Fatal(err)
	}

	c.SetReadTimeout(short)

	err = c.Ping(context.Background())
	if c.ReadTimeout() != short {
		t.Errorf("the read timeout is %v", c.ReadTimeout())
	}

	assertBroken(t, err, "did not answer in time")

	ending, err := Open(context.Background(), silentServer(t), "ANN", Trust{}, &Config{Timeouts: Timeouts{Read: short}})
	if err != nil {
		t.Fatal(err)
	}

	assertBroken(t, ending.Terminate(), "did not answer in time")
}
