package turborel

// Frame headers [PROTOCOL Frames]. Every message, either way, is a frame:
// its kind, one byte, then its body's length, a little-endian u32 that
// does not count the header, then the body.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	// frameHeaderBytes is a frame header's size: the kind, then the
	// body's length.
	frameHeaderBytes = 5
	// frameLengthAt is where the body's length begins in the header,
	// after the kind's one byte.
	frameLengthAt = 1
	// maxBody is the longest body the driver reads unless told
	// otherwise, 16 MiB, the protocol's longest and turborel-client's
	// MAX_BODY. A longer one fails the connection, so a server cannot
	// make the driver hold memory without bound.
	maxBody uint32 = 16 << 20
)

// frameHeader is a frame's kind and its body's length.
type frameHeader struct {
	// kind says which message the frame is, read in the direction it
	// travels.
	kind byte
	// length is the body's length in bytes, the header not counted.
	length uint32
}

// appendFrameHeader appends the five bytes of h to dst.
func appendFrameHeader(dst []byte, h frameHeader) []byte {
	dst = append(dst, h.kind)

	return binary.LittleEndian.AppendUint32(dst, h.length)
}

// decodeFrameHeader reads a frame header. A body longer than limit fails
// the connection [PROTOCOL Frames], before any of it is read.
func decodeFrameHeader(header [frameHeaderBytes]byte, limit uint32) (frameHeader, error) {
	decoded := frameHeader{kind: header[0], length: binary.LittleEndian.Uint32(header[frameLengthAt:])}
	if decoded.length > limit {
		return frameHeader{}, broken(fmt.Sprintf("a message of %d bytes is longer than the %d taken", decoded.length, limit))
	}

	return decoded, nil
}

// reservedBody is the most of a body reserved before its bytes arrive,
// as turborel-client's RESERVED, 64 KiB: past it the buffer grows as
// they come, so a header alone makes the driver hold little.
const reservedBody = 64 << 10

// readFrame reads one frame from src: its header, then a body of at most
// limit bytes, nothing held for a longer one and no more than 64 KiB
// reserved before its bytes arrive [PROTOCOL Frames]. A frame of kind
// `z` is inflated where compressed says `compression` was granted, and
// fails the connection where it was not [PROTOCOL Compressed frames]. A
// header or body cut short, or a read that fails or runs out of time,
// fails the connection.
func readFrame(src io.Reader, limit uint32, compressed bool) (byte, []byte, error) {
	var header [frameHeaderBytes]byte

	if _, err := io.ReadFull(src, header[:]); err != nil {
		return 0, nil, ioFailed(verbReceive, err)
	}

	h, err := decodeFrameHeader(header, limit)
	if err != nil {
		return 0, nil, err
	}

	var body bytes.Buffer

	body.Grow(int(min(h.length, reservedBody)))

	if _, err := io.CopyN(&body, src, int64(h.length)); err != nil {
		if errors.Is(err, io.EOF) {
			return 0, nil, broken("cannot receive: the connection closed within a reply")
		}

		return 0, nil, ioFailed(verbReceive, err)
	}

	if h.kind != kindCompressed {
		return h.kind, body.Bytes(), nil
	}

	if !compressed {
		return 0, nil, broken("a compressed frame came, compression not granted")
	}

	return decompress(body.Bytes(), limit)
}
