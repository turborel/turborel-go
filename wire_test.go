package turborel

import (
	"bytes"
	"errors"
	"math"
	"strings"
	"testing"
)

// assertBroken checks that err is the connection failure a malformed
// message gives, SQLCODE -919 and SQLSTATE 08006, saying why.
func assertBroken(t *testing.T, err error, why string) {
	t.Helper()

	var failure *Error
	if !errors.As(err, &failure) {
		t.Fatalf("err = %v, want an *Error", err)
	}

	if failure.SQLCode != sqlcodeConnection || failure.SQLState != sqlstateConnectionFailure {
		t.Errorf("failure %q, want SQLCODE -919 and SQLSTATE 08006", failure)
	}

	if !strings.Contains(failure.Message, why) {
		t.Errorf("message %q, want it to say %q", failure.Message, why)
	}
}

// TestFieldsLittleEndian writes and reads each integer field.
//
// Verifies: [PROTOCOL Values and fields] u8, u32 and u64 are written least
// significant byte first, `i32 as u32` and `i64 as u64` carry a signed
// number's bits, and an i128 is 16 bytes of two's complement.
func TestFieldsLittleEndian(t *testing.T) {
	t.Parallel()

	var w writer

	w.u8(0xAB)
	w.u32(0x01020304)
	w.u64(0x0102030405060708)
	w.u32(unsigned32(-2))
	w.u64(unsigned64(-3))
	w.i128(int128Of(-1))

	want := make([]byte, 0, 2*int128Bytes)
	want = append(want,
		0xAB,
		0x04, 0x03, 0x02, 0x01,
		0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01,
		0xFE, 0xFF, 0xFF, 0xFF,
		0xFD, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
	)
	want = append(want, bytes.Repeat([]byte{0xFF}, int128Bytes)...)

	if !bytes.Equal(w.buf, want) {
		t.Fatalf("written % X, want % X", w.buf, want)
	}

	r := newReader(w.buf, "test")
	got := []int64{int64(r.u8()), int64(r.u32()), signed64(r.u64()), int64(signed32(r.u32())), signed64(r.u64())}

	if !r.i128().toBig().IsInt64() || r.finish() != nil {
		t.Fatalf("reading back failed: %v", r.err)
	}

	if got[0] != 0xAB || got[1] != 0x01020304 || got[2] != 0x0102030405060708 || got[3] != -2 || got[4] != -3 {
		t.Errorf("read back %v", got)
	}
}

// TestFieldsStringsBytesListsKeys writes and reads the counted fields.
//
// Verifies: [PROTOCOL Values and fields] a string is its length in bytes
// then its UTF-8, bytes their length then the bytes, a list its count
// then each item, and a key 16 bytes with no length.
func TestFieldsStringsBytesListsKeys(t *testing.T) {
	t.Parallel()

	var w writer

	w.str("né")
	w.bytes([]byte{9})
	w.strs([]string{"a", "bc"})
	w.buf = append(w.buf, bytes.Repeat([]byte{7}, keyBytes)...)

	want := []byte{3, 0, 0, 0, 'n', 0xC3, 0xA9, 1, 0, 0, 0, 9, 2, 0, 0, 0, 1, 0, 0, 0, 'a', 2, 0, 0, 0, 'b', 'c'}
	if !bytes.Equal(w.buf[:len(want)], want) {
		t.Fatalf("written % X, want % X", w.buf, want)
	}

	r := newReader(w.buf, "test")
	s, b, list, key := r.str(), r.bytes(), r.strs(), r.key()

	if err := r.finish(); err != nil {
		t.Fatal(err)
	}

	if s != "né" || !bytes.Equal(b, []byte{9}) || len(list) != 2 || list[1] != "bc" || key[15] != 7 {
		t.Errorf("read back %q % X %q % X", s, b, list, key)
	}
}

// TestFieldsFlag reads 0 and 1, and refuses any other byte.
//
// Verifies: [PROTOCOL Values and fields] a flag is a u8, 0 or 1; any
// other byte fails the connection.
func TestFieldsFlag(t *testing.T) {
	t.Parallel()

	r := newReader([]byte{0, 1}, "test")
	if r.flag() || !r.flag() || r.finish() != nil {
		t.Fatal("flags 0 and 1 misread")
	}

	r = newReader([]byte{2}, "test")
	r.flag()
	assertBroken(t, r.err, "a flag is neither 0 nor 1")
}

// TestFieldsRefused refuses a body cut short, a string not UTF-8, and
// bytes left over.
//
// Verifies: [PROTOCOL Frames] a body cut short, or bytes left over after
// the last field, fail the connection with SQLCODE -919 and SQLSTATE
// 08006.
func TestFieldsRefused(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body []byte
		read func(r *reader)
		why  string
	}{
		{"short u32", []byte{1, 2, 3}, func(r *reader) { r.u32() }, "cut short"},
		{"short u64", []byte{1}, func(r *reader) { r.u64() }, "cut short"},
		{"short i128", []byte{1}, func(r *reader) { r.i128() }, "cut short"},
		{"short u8", nil, func(r *reader) { r.u8() }, "cut short"},
		{"short bytes", []byte{5, 0, 0, 0, 1}, func(r *reader) { r.bytes() }, "cut short"},
		{"not UTF-8", []byte{1, 0, 0, 0, 0xFF}, func(r *reader) { r.str() }, "not UTF-8"},
		{"left over", []byte{1, 2}, func(r *reader) { r.u8() }, "left over"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			r := newReader(c.body, "test")
			c.read(r)
			assertBroken(t, r.finish(), c.why)
		})
	}
}

// TestFieldsCountPastBody refuses a list whose count is larger than what
// follows, as the body runs out.
//
// Verifies: [PROTOCOL Values and fields] a count larger than what
// follows fails as the body runs out, and nothing is reserved for it: a
// count of 4 294 967 295 strings in a body of one string reads one, then
// fails.
func TestFieldsCountPastBody(t *testing.T) {
	t.Parallel()

	var w writer

	w.u32(math.MaxUint32)
	w.str("x")

	r := newReader(w.buf, "test")

	list := r.strs()
	if len(list) > 2 || cap(list) > 2 {
		t.Errorf("read %d strings, capacity %d, want the one sent and the one cut short", len(list), cap(list))
	}

	assertBroken(t, r.err, "cut short")
}

// TestFieldsNesting refuses values nested past 16.
//
// Verifies: [PROTOCOL Values] arrays nested 16 deep are read, and 17
// deep fail the connection rather than the driver's stack.
func TestFieldsNesting(t *testing.T) {
	t.Parallel()

	nested := func(depth int) []byte {
		var v Value = Null{}
		for range depth {
			v = Array{v}
		}

		var w writer

		v.encodeValue(&w)

		return w.buf
	}

	r := newReader(nested(maxNesting), "test")
	r.value()

	if err := r.finish(); err != nil {
		t.Fatalf("16 deep: %v", err)
	}

	r = newReader(nested(maxNesting+1), "test")
	r.value()
	assertBroken(t, r.finish(), "nested too deep")
}

// TestCount32Bounds writes a length within a u32.
//
// Verifies: [PROTOCOL Values and fields] a length or count is a u32: a
// negative one is 0 and one past the greatest u32 is that u32, which the
// frame's limit refuses before it is sent.
func TestCount32Bounds(t *testing.T) {
	t.Parallel()

	if count32(-1) != 0 || count32(7) != 7 || count32(math.MaxInt) != math.MaxUint32 {
		t.Errorf("count32 = %d, %d, %d", count32(-1), count32(7), count32(math.MaxInt))
	}
}
