package turborel

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

// writeLOB writes octets to a new large object of kind, in pieces of
// piece octets: the large object.
func writeLOB(t *testing.T, c *Conn, kind LOBKind, octets []byte, piece int) Locator {
	t.Helper()

	w, err := c.CreateLOB(context.Background(), kind)
	if err != nil {
		t.Fatal(err)
	}

	for len(octets) > 0 {
		n := min(piece, len(octets))
		if _, err := w.Write(octets[:n]); err != nil {
			t.Fatal(err)
		}

		octets = octets[n:]
	}

	locator, err := w.Finish()
	if err != nil {
		t.Fatal(err)
	}

	return locator
}

// largeObject gives the one value sql selects, a large object.
func largeObject(t *testing.T, c *Conn, sql string) LargeObject {
	t.Helper()

	v, ok := mustRun(t, c, sql).Rows[0][0].(LargeObject)
	if !ok {
		t.Fatalf("%s gave no large object", sql)
	}

	return v
}

// TestLiveLOBs writes large objects by locators and reads them back.
//
// Verifies: [CLIENT Large objects] a large object of at most
// SERVER.LOB_INLINE_BYTES comes whole in its row, a longer one as its
// locator and lengths [PROTOCOL Values]; a range is read by octets or by
// a CLOB's characters, at most a chunk a read [PROTOCOL Lob 'L']; a
// reader reads it whole, a chunk at a time; a value is made a piece at a
// time through a writer, a CLOB's character split between writes, and
// passed as a parameter by its locator; one sent whole is read from
// itself.
func TestLiveLOBs(t *testing.T) {
	t.Parallel()

	c := liveSchema(t, "LOB_TEST")
	mustRun(t, c, "CREATE TABLE LOB_TEST.DOCS (ID INT, BODY CLOB(1M), SCAN BLOB(1M))")

	text := strings.Repeat("é", 200000) // 400000 octets, split mid-character by pieces of 999.
	scan := bytes.Repeat([]byte{7, 0}, 150000)
	body := writeLOB(t, c, CharacterLOB, []byte(text), 999)
	image := writeLOB(t, c, BinaryLOB, scan, LOBChunkOctets+1)

	if body.Octets != uint64(len(text)) || body.Characters != 200000 || image.Octets != uint64(len(scan)) {
		t.Errorf("written %+v, %+v", body, image)
	}

	insert, err := c.Prepare(context.Background(), "INSERT INTO LOB_TEST.DOCS VALUES (?, ?, ?)")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := c.Execute(context.Background(), insert, []Value{DecimalOf(1, 0), body, image}); err != nil {
		t.Fatal(err)
	}

	mustRun(t, c, "INSERT INTO LOB_TEST.DOCS VALUES (2, 'short', X'0102')")
	mustRun(t, c, "COMMIT")

	assertLongLOBs(t, c, text, scan)

	short := largeObject(t, c, "SELECT BODY FROM LOB_TEST.DOCS WHERE ID = 2")
	if whole, ok := short.(LOB); !ok || string(whole.Octets) != "short" {
		t.Fatalf("a short CLOB came as %#v", short)
	}

	assertRange(t, c, short, lobRange{from: 1, count: 3, characters: true}, "hor")
	assertRange(t, c, short, lobRange{from: 9, count: 3, characters: true}, "")

	if all, err := io.ReadAll(c.NewLOBReader(context.Background(), short)); err != nil || string(all) != "short" {
		t.Errorf("the reader of a value sent whole gave %q, %v", all, err)
	}

	mustRun(t, c, "COMMIT")
}

// assertLongLOBs reads back the long large objects of row 1, text and
// scan, by their locators.
func assertLongLOBs(t *testing.T, c *Conn, text string, scan []byte) {
	t.Helper()

	long := largeObject(t, c, "SELECT BODY FROM LOB_TEST.DOCS WHERE ID = 1")
	if locator, ok := long.(Locator); !ok || locator.Characters != 200000 || locator.Octets != uint64(len(text)) {
		t.Fatalf("a long CLOB came as %#v", long)
	}

	assertRange(t, c, long, lobRange{from: 3, count: 2, characters: true}, "éé")
	assertRange(t, c, long, lobRange{from: 2, count: 2}, "é")

	chunk, err := c.ReadLOB(context.Background(), long, 0, 1<<30, false)
	if err != nil || len(chunk) != LOBChunkOctets {
		t.Errorf("a read gave %d octets, %v", len(chunk), err)
	}

	all, err := io.ReadAll(c.NewLOBReader(context.Background(), long))
	if err != nil || string(all) != text {
		t.Errorf("the reader gave %d octets, %v", len(all), err)
	}

	scanned := largeObject(t, c, "SELECT SCAN FROM LOB_TEST.DOCS WHERE ID = 1")

	octets, err := io.ReadAll(c.NewLOBReader(context.Background(), scanned))
	if err != nil || !bytes.Equal(octets, scan) {
		t.Errorf("the BLOB reader gave %d octets, %v", len(octets), err)
	}
}

// lobRange is a range of a large object to read.
type lobRange struct {
	from, count uint64
	characters  bool
}

// assertRange checks the range r of lob a read gives.
func assertRange(t *testing.T, c *Conn, lob LargeObject, r lobRange, want string) {
	t.Helper()

	got, err := c.ReadLOB(context.Background(), lob, r.from, r.count, r.characters)
	if err != nil || string(got) != want {
		t.Errorf("%+v gave %q, %v", r, got, err)
	}
}

// TestLiveLocatorLifetime keeps, frees and holds locators.
//
// Verifies: [CLIENT Large objects] a locator is valid until its
// transaction ends, a statement whose reply sent one keeping its
// transaction open under autocommit too; a locator freed is no longer
// valid (SQLSTATE 0F001); one held stays valid past the commit [PROTOCOL
// Lob 'L'].
func TestLiveLocatorLifetime(t *testing.T) {
	t.Parallel()

	c := liveSchema(t, "LIFETIME_TEST")
	mustRun(t, c, "CREATE TABLE LIFETIME_TEST.DOCS (ID INT, BODY CLOB(1M))")

	insert, err := c.Prepare(context.Background(), "INSERT INTO LIFETIME_TEST.DOCS VALUES (?, ?)")
	if err != nil {
		t.Fatal(err)
	}

	long := []byte(strings.Repeat("x", LOBInlineOctets+1))
	for id := range 2 {
		locator := writeLOB(t, c, CharacterLOB, long, len(long))
		if _, err := c.Execute(context.Background(), insert, []Value{DecimalOf(int64(id), 0), locator}); err != nil {
			t.Fatal(err)
		}
	}

	mustRun(t, c, "COMMIT")
	c.SetAutocommit(true)

	freed, _ := largeObject(t, c, "SELECT BODY FROM LIFETIME_TEST.DOCS WHERE ID = 0").(Locator)
	if !c.InTransaction() {
		t.Error("a locator sent left no transaction active under autocommit")
	}

	if err := c.FreeLOB(context.Background(), freed); err != nil {
		t.Fatal(err)
	}

	_, err = c.ReadLOB(context.Background(), freed, 0, 1, false)
	assertState(t, err, "0F001")
	assertState(t, c.FreeLOB(context.Background(), freed), "0F001")

	held, _ := largeObject(t, c, "SELECT BODY FROM LIFETIME_TEST.DOCS WHERE ID = 1").(Locator)
	if err := c.HoldLOB(context.Background(), held); err != nil {
		t.Fatal(err)
	}

	mustRun(t, c, "COMMIT")

	if got, err := c.ReadLOB(context.Background(), held, 0, 2, false); err != nil || string(got) != "xx" {
		t.Errorf("a held locator past the commit gave %q, %v", got, err)
	}
}
