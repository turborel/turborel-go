package turborel

// Compressed frames [PROTOCOL Compressed frames]. Once `compression` is
// granted, either side may send any frame compressed, as a frame of kind
// `z`: the held frame's kind, its body's length, then a zlib stream of
// the body (RFC 1950). The reader inflates at most that length, itself at
// most the longest body it takes, so a stream cannot make it hold more.

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	// kindCompressed is a compressed frame's kind, the same both ways.
	kindCompressed byte = 'z'
	// compressedPrefix is the bytes of a compressed frame's body before
	// its stream: the held frame's kind and its body's length.
	compressedPrefix = frameHeaderBytes
	// defaultCompressionThreshold is the threshold turborel-protocol
	// chooses: a frame shorter than this, its header counted, is never
	// compressed, as the stream's header and trailer outweigh what it
	// saves.
	defaultCompressionThreshold = 1024
)

// The ways a compressed frame's stream is refused.
var (
	errStreamPastLength = errors.New("it inflates past the length it declares")
	errStreamTrailing   = errors.New("bytes follow the stream's Adler-32")
)

// compressFrames gives the frames of frames, each whose whole length is
// at least threshold compressed, where that makes it shorter.
func compressFrames(frames []byte, threshold int) []byte {
	var dst []byte

	for len(frames) >= frameHeaderBytes {
		length := int(binary.LittleEndian.Uint32(frames[frameLengthAt:frameHeaderBytes]))
		length = min(length, len(frames)-frameHeaderBytes)
		whole := frameHeaderBytes + length

		dst = compressFrame(dst, frames[:whole], threshold)
		frames = frames[whole:]
	}

	return dst
}

// compressFrame appends one frame, compressed if it is at least
// threshold bytes long and its stream comes out shorter than its body.
func compressFrame(dst, frame []byte, threshold int) []byte {
	body := frame[frameHeaderBytes:]
	if len(frame) < threshold {
		return append(dst, frame...)
	}

	var stream bytes.Buffer

	z := zlib.NewWriter(&stream)
	_, _ = z.Write(body) // A bytes.Buffer's writes never fail.
	_ = z.Close()        // Nor does the flush of a stream into one.

	if stream.Len()+compressedPrefix >= len(body) {
		return append(dst, frame...)
	}

	w := writer{buf: dst}
	w.frame(kindCompressed, func() {
		w.u8(frame[0])
		w.u32(count32(len(body)))
		w.buf = append(w.buf, stream.Bytes()...)
	})

	return w.buf
}

// decompress gives the kind and body of the frame the compressed
// frame's body holds, its length at most limit, inflated no further. A
// `z` within a `z`, a length past limit, a stream not of RFC 1950, or one
// inflating to another length fails the connection.
func decompress(body []byte, limit uint32) (byte, []byte, error) {
	r := newReader(body, "compressed frame")
	kind, length := r.u8(), r.u32()

	if r.err != nil {
		return 0, nil, r.err
	}

	if kind == kindCompressed {
		return 0, nil, broken("a compressed frame holds another")
	}

	if length > limit {
		return 0, nil, broken(fmt.Sprintf("a compressed message of %d bytes is longer than the %d taken", length, limit))
	}

	inflated, err := inflate(body[compressedPrefix:], int64(length))
	if err != nil {
		return 0, nil, broken("a compressed frame: " + err.Error())
	}

	if int64(len(inflated)) != int64(length) {
		return 0, nil, broken("a compressed frame inflates to another length than it declares")
	}

	return kind, inflated, nil
}

// inflate reads the zlib stream, which must be all of stream, holding at
// most limit bytes of its data.
func inflate(stream []byte, limit int64) ([]byte, error) {
	source := bytes.NewReader(stream)

	z, err := zlib.NewReader(source)
	if err != nil {
		return nil, fmt.Errorf("the stream's header: %w", err)
	}

	data, err := io.ReadAll(io.LimitReader(z, limit+1))
	if err != nil {
		return nil, fmt.Errorf("the stream: %w", err)
	}

	if int64(len(data)) > limit {
		return nil, errStreamPastLength
	}

	if source.Len() != 0 {
		return nil, errStreamTrailing
	}

	return data, nil
}
