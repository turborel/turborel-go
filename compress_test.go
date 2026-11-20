package turborel

import (
	"bytes"
	"compress/zlib"
	"errors"
	"testing"
)

// zlibOf is data as a zlib stream.
func zlibOf(t *testing.T, data []byte) []byte {
	t.Helper()

	var stream bytes.Buffer

	z := zlib.NewWriter(&stream)
	if _, err := z.Write(data); err != nil {
		t.Fatal(err)
	}

	if err := z.Close(); err != nil {
		t.Fatal(err)
	}

	return stream.Bytes()
}

// flipLast is b with its last byte's lowest bit flipped.
func flipLast(b []byte) []byte {
	flipped := append([]byte{}, b...)
	flipped[len(flipped)-1] ^= 1

	return flipped
}

// TestCompressRoundTrip compresses a long frame and reads it back.
//
// Verifies: [PROTOCOL Compressed frames] a frame sent compressed is a
// frame of kind `z`: the held frame's kind, its body's length, then a
// zlib stream of the body, inflated back to the same frame.
func TestCompressRoundTrip(t *testing.T) {
	t.Parallel()

	sql := bytes.Repeat([]byte("SELECT 1; "), 400)
	frame := encodeRequest(queryRequest{sql: string(sql)})
	compressed := compressFrames(frame, defaultCompressionThreshold)

	if compressed[0] != kindCompressed || len(compressed) >= len(frame) {
		t.Fatalf("compressed % X, from %d bytes", compressed[:10], len(frame))
	}

	if compressed[5] != kindQuery || int(compressed[6])|int(compressed[7])<<8 != len(frame)-frameHeaderBytes {
		t.Errorf("the held kind and length are % X", compressed[5:10])
	}

	kind, inflated, err := readFrame(bytes.NewReader(compressed), maxBody, true)
	if err != nil || kind != kindQuery || !bytes.Equal(inflated, frame[frameHeaderBytes:]) {
		t.Errorf("read back %c, %d bytes, %v", kind, len(inflated), err)
	}
}

// TestCompressThreshold leaves a short frame, and one compressing would
// not shorten, as they are.
//
// Verifies: [PROTOCOL Compressed frames] a client chooses its own
// threshold, and a frame shorter than it, or whose compressed form would
// not be shorter, is sent as it is; frames after one another are each
// considered alone.
func TestCompressThreshold(t *testing.T) {
	t.Parallel()

	short := encodeRequest(queryRequest{sql: "VALUES 1"})
	if got := compressFrames(short, defaultCompressionThreshold); !bytes.Equal(got, short) {
		t.Errorf("a short frame was changed: % X", got)
	}

	random := make([]byte, 64)
	for i := range random {
		random[i] = byte(i)*167 + 13
	}

	incompressible := encodeRequest(answerRequest{answer: random})
	if got := compressFrames(incompressible, 0); !bytes.Equal(got, incompressible) {
		t.Errorf("a frame that would not shrink was changed")
	}

	long := encodeRequest(queryRequest{sql: string(bytes.Repeat([]byte("x"), 2000))})
	both := compressFrames(append(append([]byte{}, short...), long...), defaultCompressionThreshold)

	if !bytes.Equal(both[:len(short)], short) || both[len(short)] != kindCompressed {
		t.Errorf("two frames compressed as % X", both[:len(short)+1])
	}
}

// TestDecompressRefused refuses the compressed frames that must fail the
// connection.
//
// Verifies: [PROTOCOL Compressed frames] a `z` frame holding another `z`,
// one declaring more than the longest body taken, one whose stream
// inflates past its length or to another length, a stream not of RFC
// 1950 or followed by bytes, and a `z` frame received when compression
// was not granted, each fail the connection.
func TestDecompressRefused(t *testing.T) {
	t.Parallel()

	data := bytes.Repeat([]byte("a"), 100)
	stream := zlibOf(t, data)
	prefix := func(kind byte, length uint32) []byte {
		return body(func(w *writer) {
			w.u8(kind)
			w.u32(length)
		})
	}

	cases := map[string]struct {
		body []byte
		why  string
	}{
		"z in z":      {append(prefix(kindCompressed, 100), stream...), "holds another"},
		"past limit":  {append(prefix(kindRows, maxBody+1), stream...), "longer than"},
		"past length": {append(prefix(kindRows, 99), stream...), "inflates past"},
		"shorter":     {append(prefix(kindRows, 101), stream...), "another length"},
		"not zlib":    {append(prefix(kindRows, 100), 1, 2, 3), "header"},
		"trailing":    {append(append(prefix(kindRows, 100), stream...), 0), "follow"},
		"cut short":   {[]byte{kindRows, 1}, "cut short"},
		"bad adler":   {append(prefix(kindRows, 100), flipLast(stream)...), "stream"},
	}
	for name, c := range cases {
		_, _, err := decompress(c.body, maxBody)
		if err == nil {
			t.Errorf("%s: inflated", name)

			continue
		}

		assertBroken(t, err, c.why)
	}

	header := frameHeader{kind: kindCompressed, length: count32(len(stream) + compressedPrefix)}
	frame := append(appendFrameHeader(nil, header), prefix(kindRows, 100)...)
	frame = append(frame, stream...)
	_, _, err := readFrame(bytes.NewReader(frame), maxBody, false)
	assertBroken(t, err, "not granted")
}

// TestReadFrame reads frames from a stream, and refuses those cut short
// or too long.
//
// Verifies: [PROTOCOL Frames] a frame is read as its header says, a body
// longer than the longest taken, 16 MiB or the program's own limit,
// failing before any of it is held, and a header or body cut short
// failing the connection.
func TestReadFrame(t *testing.T) {
	t.Parallel()

	kind, b, err := readFrame(bytes.NewReader([]byte{'G', 0, 0, 0, 0}), maxBody, false)
	if err != nil || kind != kindPong || len(b) != 0 {
		t.Errorf("read %c % X %v", kind, b, err)
	}

	_, _, err = readFrame(bytes.NewReader([]byte{'G', 0}), maxBody, false)
	assertBroken(t, err, "cannot receive")

	_, _, err = readFrame(bytes.NewReader([]byte{'B', 5, 0, 0, 0, 1}), maxBody, false)
	assertBroken(t, err, "the connection closed within a reply")

	_, _, err = readFrame(bytes.NewReader([]byte{'B', 0, 0, 0, 0x7F}), maxBody, false)
	assertBroken(t, err, "longer than")

	_, _, err = readFrame(bytes.NewReader([]byte{'B', 5, 0, 0, 0, 1, 2, 3, 4, 5}), 4, false)
	assertBroken(t, err, "a message of 5 bytes is longer than the 4 taken")
}

// FuzzDecompress inflates arbitrary compressed frames.
//
// Verifies: [PROTOCOL Compressed frames] a compressed frame never makes
// the reader hold more than its declared length, itself within the
// limit, and a malformed one fails the connection.
func FuzzDecompress(f *testing.F) {
	f.Add([]byte{'B', 4, 0, 0, 0, 0x78, 0x9C, 0x63, 0x60, 0x60, 0x60, 0, 0, 0, 4, 0, 1})

	f.Fuzz(func(t *testing.T, b []byte) {
		const limit = 1 << 16

		_, inflated, err := decompress(b, limit)
		if err == nil && len(inflated) > limit {
			t.Fatalf("inflated %d bytes past the limit", len(inflated))
		}

		var failure *Error
		if err != nil && (!errors.As(err, &failure) || failure.SQLCode != sqlcodeConnection) {
			t.Fatalf("err = %v, want a connection failure", err)
		}
	})
}
