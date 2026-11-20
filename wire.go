package turborel

// The fields every message is made of [PROTOCOL Values and fields]:
// integers little-endian, flags, strings, bytes, lists and keys, written
// by a writer and read by a reader.
//
// The reader keeps its first error and gives zero values after it, so a
// message is read field by field and its error looked at once, at the
// end. It holds only what it has read: a list's count is never used to
// reserve memory, so a count larger than what follows fails as the body
// runs out (R-ERR-4).

import (
	"encoding/binary"
	"fmt"
	"math"
	"unicode/utf8"
)

// The sizes of the protocol's fixed fields, in bytes.
const (
	byteBytes  = 1
	shortBytes = 2
	u32Bytes   = 4
	wordBytes  = 8
	// keyBytes is a cancel key's size: 16 bytes with no length before
	// them.
	keyBytes = 16
)

// The two bytes a flag may be [PROTOCOL Values and fields].
const (
	flagFalse byte = 0
	flagTrue  byte = 1
)

// maxNesting is how deep arrays, rows, multisets and structured values
// and types may nest, as turborel's MAX_TYPE_NESTING, 16: a reply nested
// deeper fails the connection rather than the driver's stack.
const maxNesting = 16

// signed64 is the int64 whose two's complement bits u holds, as the
// protocol writes `i64 as u64`.
func signed64(u uint64) int64 {
	return int64(u) //nolint:gosec // G115: the bits are reinterpreted on purpose, as the protocol says.
}

// unsigned64 is the bits of v, as the protocol writes `i64 as u64`.
func unsigned64(v int64) uint64 {
	return uint64(v) //nolint:gosec // G115: the bits are reinterpreted on purpose, as the protocol says.
}

// signed32 is the int32 whose two's complement bits u holds, as the
// protocol writes `i32 as u32`.
func signed32(u uint32) int32 {
	return int32(u) //nolint:gosec // G115: the bits are reinterpreted on purpose, as the protocol says.
}

// signed16 is the int16 whose two's complement bits u holds.
func signed16(u uint16) int16 {
	return int16(u) //nolint:gosec // G115: the bits are reinterpreted on purpose, as the protocol says.
}

// unsigned16 is the bits of v.
func unsigned16(v int16) uint16 {
	return uint16(v) //nolint:gosec // G115: the bits are reinterpreted on purpose, as the protocol says.
}

// unsigned32 is the bits of v, as the protocol writes `i32 as u32`.
func unsigned32(v int32) uint32 {
	return uint32(v) //nolint:gosec // G115: the bits are reinterpreted on purpose, as the protocol says.
}

// count32 is a length or count as the u32 the protocol writes, at most
// the greatest u32; a message that long is refused by its frame's limit
// before it is sent.
func count32(n int) uint32 {
	if n < 0 {
		return 0
	}

	if uint64(n) > math.MaxUint32 {
		return math.MaxUint32
	}

	return uint32(n)
}

// writer appends a message's fields.
type writer struct {
	buf []byte
}

func (w *writer) u8(v byte) { w.buf = append(w.buf, v) }

func (w *writer) u32(v uint32) { w.buf = binary.LittleEndian.AppendUint32(w.buf, v) }

func (w *writer) u64(v uint64) { w.buf = binary.LittleEndian.AppendUint64(w.buf, v) }

func (w *writer) i128(v int128) { w.buf = v.appendLE(w.buf) }

// flag writes a flag: 1 if set, else 0.
func (w *writer) flag(set bool) {
	if set {
		w.u8(flagTrue)

		return
	}

	w.u8(flagFalse)
}

// bytes writes b after its length.
func (w *writer) bytes(b []byte) {
	w.u32(count32(len(b)))
	w.buf = append(w.buf, b...)
}

// str writes s after its length in bytes.
func (w *writer) str(s string) {
	w.u32(count32(len(s)))
	w.buf = append(w.buf, s...)
}

// strs writes a list of strings: their count, then each.
func (w *writer) strs(list []string) {
	w.u32(count32(len(list)))

	for _, s := range list {
		w.str(s)
	}
}

// reader reads a message's fields, keeping the first error.
type reader struct {
	buf []byte
	at  int
	// depth is how deep the constructed values and types being read
	// nest.
	depth int
	// what names the message, as "reply", in an error.
	what string
	err  error
}

// newReader reads body, a message named what.
func newReader(body []byte, what string) *reader {
	return &reader{buf: body, what: what}
}

// fail records that the message is malformed: a connection failure
// [PROTOCOL Frames], naming where it was found.
func (r *reader) fail(why string) {
	if r.err == nil {
		r.err = broken(fmt.Sprintf("a %s received is malformed at byte %d: %s", r.what, r.at, why))
	}
}

// take gives the next n bytes, or fails if the body has fewer.
func (r *reader) take(n int) []byte {
	if r.err != nil {
		return nil
	}

	if n < 0 || n > len(r.buf)-r.at {
		r.fail("it is cut short")

		return nil
	}

	b := r.buf[r.at : r.at+n]
	r.at += n

	return b
}

func (r *reader) u8() byte {
	b := r.take(byteBytes)
	if b == nil {
		return 0
	}

	return b[0]
}

func (r *reader) u32() uint32 {
	b := r.take(u32Bytes)
	if b == nil {
		return 0
	}

	return binary.LittleEndian.Uint32(b)
}

func (r *reader) u64() uint64 {
	b := r.take(wordBytes)
	if b == nil {
		return 0
	}

	return binary.LittleEndian.Uint64(b)
}

func (r *reader) i128() int128 {
	b := r.take(int128Bytes)
	if b == nil {
		return int128{}
	}

	return int128At(b)
}

// flag reads a flag; a byte other than 0 or 1 fails.
func (r *reader) flag() bool {
	switch r.u8() {
	case flagFalse:
		return false
	case flagTrue:
		return true
	default:
		r.fail("a flag is neither 0 nor 1")

		return false
	}
}

// bytes reads bytes after their length, copied so the body may be
// reused.
func (r *reader) bytes() []byte {
	n := r.u32()
	if r.err != nil {
		return nil
	}

	if int64(n) > int64(len(r.buf)-r.at) {
		r.fail("it is cut short")

		return nil
	}

	b := r.take(int(n))

	return append([]byte{}, b...)
}

// str reads a string after its length; one not UTF-8 fails.
func (r *reader) str() string {
	b := r.bytes()
	if r.err != nil {
		return ""
	}

	if !utf8.Valid(b) {
		r.fail("a string is not UTF-8")

		return ""
	}

	return string(b)
}

// count reads a list's count. Nothing is reserved for it: each item read
// takes at least a byte, so a count larger than what follows fails as
// the body runs out.
func (r *reader) count() uint32 {
	return r.u32()
}

// more says whether the next of n items should be read: none after an
// error.
func (r *reader) more(i, n uint32) bool {
	return i < n && r.err == nil
}

// strs reads a list of strings.
func (r *reader) strs() []string {
	n := r.count()

	var list []string
	for i := uint32(0); r.more(i, n); i++ {
		list = append(list, r.str())
	}

	return list
}

// key reads a cancel key, 16 bytes with no length.
func (r *reader) key() [keyBytes]byte {
	var key [keyBytes]byte

	copy(key[:], r.take(keyBytes))

	return key
}

// atEnd says whether every byte of the body was read.
func (r *reader) atEnd() bool {
	return r.at >= len(r.buf)
}

// finish fails if bytes are left over after the last field [PROTOCOL
// Frames], and gives the reader's error.
func (r *reader) finish() error {
	if r.err == nil && !r.atEnd() {
		r.fail("bytes are left over after its last field")
	}

	return r.err
}

// nest runs read one level deeper, failing past maxNesting.
func (r *reader) nest(read func()) {
	if r.depth >= maxNesting {
		r.fail("its arrays and rows are nested too deep")

		return
	}

	r.depth++
	read()
	r.depth--
}
