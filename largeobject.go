package turborel

// Large objects over a connection [CLIENT Large objects]: one a result
// sends by its locator read by ranges, or as a stream (LOBReader), a
// chunk a request; a new one written a piece at a time (LOBWriter) and
// sent as a parameter by its locator; and a locator freed or held
// [PROTOCOL Lob 'L']. A large object a result sends whole is read from
// the value itself. A locator is valid until the transaction it was given
// in ends, a held one until a rollback.

import (
	"context"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

// LOBInlineOctets is the longest value a server sends whole for a large
// object unless its operator sets otherwise, SERVER.LOB_INLINE_BYTES; a
// longer one comes by its locator, and an import sends a longer one by a
// locator.
const LOBInlineOctets = 64 * 1024

// LOBChunkOctets is the most octets a read of a large object gives, and
// the octets a LOBWriter sends in one append.
const LOBChunkOctets = 256 * 1024

// errShortLOB is a large object the server sent less of than its length.
var errShortLOB = errors.New("the server sent less of the large object than its length")

// LargeObject is a large object: a [LOB] held whole, or a [Locator].
type LargeObject interface {
	Value
	lobOctets() uint64
}

func (v LOB) lobOctets() uint64     { return uint64(len(v.Octets)) }
func (v Locator) lobOctets() uint64 { return v.Octets }

// ReadLOB gives at most a chunk of lob from octet from, counted from 0, at
// most count octets; or, of a character large object if characters, from
// character from, at most count characters, whole ones. It gives fewer
// than asked only at the large object's end, or at a chunk's. One held
// whole is read from itself, with no request.
func (c *Conn) ReadLOB(ctx context.Context, lob LargeObject, from, count uint64, characters bool) ([]byte, error) {
	whole, ok := lob.(LOB)
	if ok {
		return heldRange(whole, from, count, characters), nil
	}

	locator, _ := lob.(Locator) // A LargeObject is a LOB or a Locator.

	defer c.during(ctx)()

	rep, err := c.lobRequest(lobReadRequest{locator: locator.Locator, from: from, count: count, characters: characters})
	if err != nil {
		return nil, withCause(err, ctx.Err())
	}

	data, ok := rep.(lobDataReply)
	if !ok {
		return nil, c.lose("the server sent a locator for a Read")
	}

	return data.data, nil
}

// heldRange gives the range of a large object held whole: octets, or of a
// character large object if characters, characters.
func heldRange(v LOB, from, count uint64, characters bool) []byte {
	if !characters || v.Kind == BinaryLOB {
		start := min(from, uint64(len(v.Octets)))
		end := min(start+min(count, uint64(len(v.Octets))), uint64(len(v.Octets)))

		return v.Octets[start:end]
	}

	runes := []rune(strings.ToValidUTF8(string(v.Octets), string(utf8.RuneError)))
	start := min(from, uint64(len(runes)))
	end := min(start+min(count, uint64(len(runes))), uint64(len(runes)))

	return []byte(string(runes[start:end]))
}

// FreeLOB ends locator, and the value it named [PROTOCOL Lob 'L']. It
// fails with SQLSTATE 0F001 if the locator is not valid.
func (c *Conn) FreeLOB(ctx context.Context, locator Locator) error {
	return c.completed(ctx, lobFreeRequest{locator: locator.Locator})
}

// HoldLOB keeps locator valid past its transaction's commit, until it is
// freed or a rollback ends it [PROTOCOL Lob 'L'].
func (c *Conn) HoldLOB(ctx context.Context, locator Locator) error {
	return c.completed(ctx, lobHoldRequest{locator: locator.Locator})
}

// lobRequest sends req and gives the server's Lob reply.
func (c *Conn) lobRequest(req request) (reply, error) {
	if err := c.send(req); err != nil {
		return nil, err
	}

	rep, err := c.receive()
	if err != nil {
		return nil, err
	}

	switch r := rep.(type) {
	case lobDataReply, lobLocatorReply:
		return rep, nil
	case errorReply:
		return nil, r.failure
	default:
		return nil, c.lose("the server sent no Lob reply")
	}
}

// LOBReader reads a large object whole, a chunk a request, as an
// [io.Reader].
type LOBReader struct {
	conn  *Conn
	ctx   context.Context //nolint:containedctx // A reader's Read takes no context, so it keeps its maker's.
	lob   LargeObject
	at    uint64
	chunk []byte
}

// NewLOBReader gives a reader of lob's octets, read a chunk at a time,
// each read cancelled on the server as ctx is done.
func (c *Conn) NewLOBReader(ctx context.Context, lob LargeObject) *LOBReader {
	return &LOBReader{conn: c, ctx: ctx, lob: lob}
}

// Read reads the large object's next octets into out.
func (r *LOBReader) Read(out []byte) (int, error) {
	if len(r.chunk) == 0 {
		if len(out) == 0 {
			return 0, nil
		}

		if r.at >= r.lob.lobOctets() {
			return 0, io.EOF
		}

		chunk, err := r.conn.ReadLOB(r.ctx, r.lob, r.at, LOBChunkOctets, false)
		if err != nil {
			return 0, err
		}

		if len(chunk) == 0 {
			return 0, errShortLOB
		}

		r.at += uint64(len(chunk))
		r.chunk = chunk
	}

	n := copy(out, r.chunk)
	r.chunk = r.chunk[n:]

	return n, nil
}

// LOBWriter writes a new large object a piece at a time, sent in appends
// of at most a chunk [PROTOCOL Lob 'L'], as an [io.Writer]. A character
// large object's octets are UTF-8, a character free to be split between
// writes.
type LOBWriter struct {
	conn    *Conn
	ctx     context.Context //nolint:containedctx // A writer's Write takes no context, so it keeps its maker's.
	kind    LOBKind
	locator uint32
	pending []byte
	octets  uint64
	// chars counts each character at its lead octet, so one split
	// between writes is counted once.
	chars uint64
}

// CreateLOB makes a new large object of kind, to write a piece at a time.
func (c *Conn) CreateLOB(ctx context.Context, kind LOBKind) (*LOBWriter, error) {
	defer c.during(ctx)()

	rep, err := c.lobRequest(lobCreateRequest{kind: kind})
	if err != nil {
		return nil, withCause(err, ctx.Err())
	}

	made, ok := rep.(lobLocatorReply)
	if !ok {
		return nil, c.lose("the server sent data for a Create")
	}

	return &LOBWriter{conn: c, ctx: ctx, kind: kind, locator: made.locator}, nil
}

// Write adds octets to the large object, sending them once a chunk is
// waiting.
func (w *LOBWriter) Write(octets []byte) (int, error) {
	w.pending = append(w.pending, octets...)
	w.octets += uint64(len(octets))

	if w.kind == CharacterLOB {
		for _, b := range octets {
			if !utf8.RuneStart(b) {
				continue
			}

			w.chars++
		}
	}

	if len(w.pending) >= LOBChunkOctets {
		if err := w.Flush(); err != nil {
			return 0, err
		}
	}

	return len(octets), nil
}

// Flush sends what is written and not yet sent.
func (w *LOBWriter) Flush() error {
	if len(w.pending) == 0 {
		return nil
	}

	octets := w.pending
	w.pending = nil

	return w.conn.completed(w.ctx, lobAppendRequest{locator: w.locator, octets: octets})
}

// Finish sends what is not yet sent and gives the large object, to send
// as a parameter by its locator. The server refuses an append of a
// character large object whose octets are not UTF-8 (SQLSTATE 22021), or
// that makes it longer than the longest (22001).
func (w *LOBWriter) Finish() (Locator, error) {
	if err := w.Flush(); err != nil {
		return Locator{}, err
	}

	return Locator{Kind: w.kind, Locator: w.locator, Octets: w.octets, Characters: w.chars}, nil
}
