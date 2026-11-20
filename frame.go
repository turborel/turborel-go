package turborel

// Frame headers [PROTOCOL Frames]. Every message, either way, is a frame:
// its kind, one byte, then its body's length, a little-endian u32 that
// does not count the header, then the body.

import (
	"encoding/binary"
	"fmt"
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
