package turborel

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

// lobData replies a Lob reply of data.
func (f *fake) lobData(data []byte) {
	f.reply(kindLobReply, func(w *writer) {
		w.u8(lobData)
		w.bytes(data)
	})
}

// lobLocator replies a Lob reply of a locator.
func (f *fake) lobLocator(locator uint32) {
	f.reply(kindLobReply, func(w *writer) {
		w.u8(lobLocator)
		w.u32(locator)
	})
}

// counted replies a Complete counting rows.
func (f *fake) counted(rows uint64) {
	f.reply(kindComplete, func(w *writer) {
		w.u32(0)
		w.str("00000")
		w.flag(true)
		w.u8(tailRows)
		w.u64(rows)
	})
}

// expect reads the next request, checking its kind.
func (f *fake) expect(kind byte) []byte {
	f.t.Helper()

	got, body := f.request()
	if got != kind {
		f.t.Errorf("the fake server read %c, want %c", got, kind)
	}

	return body
}

// TestLOBRequests reads, makes, frees and holds large objects.
//
// Verifies: [PROTOCOL Lob 'L'] a read of a locator is answered by a Lob
// reply of data, a create by one of a locator, an append, a free and a
// hold by a Complete; a reply that is no Lob reply fails the connection;
// the server's Error is the call's failure [CLIENT Large objects].
func TestLOBRequests(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	c := openGranted(t, nil, func(f *fake) {
		f.expect(kindLobRequest)
		f.lobData([]byte("ab"))
		f.expect(kindLobRequest)
		f.lobLocator(9)
		f.expect(kindLobRequest)
		f.failure(-301, "0F001")
		f.expect(kindLobRequest)
		f.complete("00000", true)
		f.expect(kindLobRequest)
		f.complete("00000", true)
	})

	locator := Locator{Kind: CharacterLOB, Locator: 4, Octets: 9, Characters: 9}

	if got, err := c.ReadLOB(ctx, locator, 0, 2, false); err != nil || string(got) != "ab" {
		t.Errorf("ReadLOB gave %q, %v", got, err)
	}

	w, err := c.CreateLOB(ctx, BinaryLOB)
	if err != nil || w.locator != 9 {
		t.Fatalf("CreateLOB gave %v", err)
	}

	assertState(t, c.FreeLOB(ctx, locator), "0F001")

	if err := c.HoldLOB(ctx, locator); err != nil {
		t.Error(err)
	}

	if _, err := c.ReadLOB(ctx, locator, 0, 1, true); err == nil || !c.IsLost() {
		t.Errorf("a Complete for a read gave %v", err)
	}
}

// TestLOBRepliesOutOfPlace fails the connection of a Lob reply of the
// wrong kind.
//
// Verifies: [PROTOCOL Lob 'L'] a read answered by a locator, or a create
// by data, fails the connection.
func TestLOBRepliesOutOfPlace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	locator := Locator{Kind: CharacterLOB, Locator: 4, Octets: 9, Characters: 9}
	broken := openGranted(t, nil, func(f *fake) {
		f.expect(kindLobRequest)
		f.lobLocator(1)
	})
	if _, err := broken.ReadLOB(ctx, locator, 0, 1, false); err == nil || !broken.IsLost() {
		t.Errorf("a locator for a read gave %v", err)
	}

	madeData := openGranted(t, nil, func(f *fake) {
		f.expect(kindLobRequest)
		f.lobData(nil)
	})
	if _, err := madeData.CreateLOB(ctx, CharacterLOB); err == nil || !madeData.IsLost() {
		t.Errorf("data for a create gave %v", err)
	}
}

// TestLOBWriter writes a large object in appends.
//
// Verifies: [CLIENT Large objects] a writer sends what is written in
// appends of at most a chunk, once a chunk waits, on Flush, and on
// Finish, which gives the large object's locator and lengths, a CLOB's
// characters counted once each though split between writes [PROTOCOL
// Lob 'L'].
func TestLOBWriter(t *testing.T) {
	t.Parallel()

	var appended []int

	c := openGranted(t, nil, func(f *fake) {
		f.expect(kindLobRequest)
		f.lobLocator(3)

		for range 2 {
			body := f.expect(kindLobRequest)
			appended = append(appended, len(body)-1-4-4)

			f.complete("00000", true)
		}
	})

	w, err := c.CreateLOB(context.Background(), CharacterLOB)
	if err != nil {
		t.Fatal(err)
	}

	big := []byte(strings.Repeat("é", LOBChunkOctets/2))
	for _, piece := range [][]byte{big[:1], big[1:]} {
		if _, err := w.Write(piece); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := w.Write([]byte("ab")); err != nil {
		t.Fatal(err)
	}

	locator, err := w.Finish()
	want := Locator{Kind: CharacterLOB, Locator: 3, Octets: LOBChunkOctets + 2, Characters: LOBChunkOctets/2 + 2}

	if err != nil || locator != want || !slices.Equal(appended, []int{LOBChunkOctets, 2}) {
		t.Errorf("Finish gave %+v, %v, appends %v", locator, err, appended)
	}

	if err := w.Flush(); err != nil {
		t.Errorf("a Flush of nothing: %v", err)
	}
}

// TestLOBAppendRefused fails Finish of an append the server refuses.
//
// Verifies: [PROTOCOL Lob 'L'] an append the server refuses, as of a CLOB
// whose octets are not UTF-8, fails the writer's Finish with its Error.
func TestLOBAppendRefused(t *testing.T) {
	t.Parallel()

	c := openGranted(t, nil, func(f *fake) {
		f.expect(kindLobRequest)
		f.lobLocator(3)
		f.expect(kindLobRequest)
		f.failure(-301, "22021")
	})

	w, err := c.CreateLOB(context.Background(), CharacterLOB)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := w.Write([]byte{0xff}); err != nil {
		t.Fatal(err)
	}

	_, err = w.Finish()
	assertState(t, err, "22021")
}

// TestLOBReader reads a large object a chunk a request.
//
// Verifies: [CLIENT Large objects] a reader of a value by its locator
// reads it a chunk a request until its length, then ends; a server that
// sends less than its length fails the read; a read of no bytes asks
// nothing; one held whole is read from itself, by octets or characters.
func TestLOBReader(t *testing.T) {
	t.Parallel()

	c := openGranted(t, nil, func(f *fake) {
		f.expect(kindLobRequest)
		f.lobData([]byte("abc"))
		f.expect(kindLobRequest)
		f.lobData(nil)
	})

	r := c.NewLOBReader(context.Background(), Locator{Kind: BinaryLOB, Locator: 1, Octets: 3})
	if n, err := r.Read(nil); n != 0 || err != nil {
		t.Errorf("a read of nothing gave %d, %v", n, err)
	}

	if all, err := io.ReadAll(r); err != nil || string(all) != "abc" {
		t.Errorf("the reader gave %q, %v", all, err)
	}

	short := c.NewLOBReader(context.Background(), Locator{Kind: BinaryLOB, Locator: 2, Octets: 3})
	if _, err := io.ReadAll(short); !errors.Is(err, errShortLOB) {
		t.Errorf("a short value gave %v", err)
	}

	whole := LOB{Kind: CharacterLOB, Octets: []byte("héllo")}
	cases := []struct {
		from, count uint64
		characters  bool
		want        string
	}{
		{1, 2, true, "él"},
		{1, 2, false, "\xc3\xa9"},
		{4, 99, true, "o"},
		{99, 1, false, ""},
	}

	for _, k := range cases {
		got, err := c.ReadLOB(context.Background(), whole, k.from, k.count, k.characters)
		if err != nil || string(got) != k.want {
			t.Errorf("%+v gave %q, %v", k, got, err)
		}
	}

	binary := LOB{Kind: BinaryLOB, Octets: []byte{1, 2, 3}}
	if got := heldRange(binary, 1, 5, true); !bytes.Equal(got, []byte{2, 3}) {
		t.Errorf("a binary range by characters gave %v", got)
	}
}
