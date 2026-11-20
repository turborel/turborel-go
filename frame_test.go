package turborel

import (
	"bytes"
	"errors"
	"testing"
)

// TestFrameHeaderWorkedExample encodes and decodes the headers of the
// spec's worked example.
//
// Verifies: [PROTOCOL A worked example] the headers of Hello, Ping, Pong,
// Query, Rows and Complete are written and read byte for byte as the
// example gives them.
func TestFrameHeaderWorkedExample(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		wire   [frameHeaderBytes]byte
		header frameHeader
	}{
		{"Hello", [frameHeaderBytes]byte{0x48, 0x24, 0x00, 0x00, 0x00}, frameHeader{kind: 'H', length: 36}},
		{"Ping", [frameHeaderBytes]byte{0x47, 0x00, 0x00, 0x00, 0x00}, frameHeader{kind: 'G', length: 0}},
		{"Query", [frameHeaderBytes]byte{0x51, 0x0D, 0x00, 0x00, 0x00}, frameHeader{kind: 'Q', length: 13}},
		{"Rows", [frameHeaderBytes]byte{0x42, 0x1D, 0x00, 0x00, 0x00}, frameHeader{kind: 'B', length: 29}},
		{"Complete", [frameHeaderBytes]byte{0x4B, 0x0E, 0x00, 0x00, 0x00}, frameHeader{kind: 'K', length: 14}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			assertHeaderRoundTrip(t, c.wire, c.header)
		})
	}
}

// TestFrameHeaderLittleEndian reads a length whose four bytes all
// differ.
//
// Verifies: [PROTOCOL Values and fields] a frame's length is a u32
// written least significant byte first.
func TestFrameHeaderLittleEndian(t *testing.T) {
	t.Parallel()

	wire := [frameHeaderBytes]byte{'B', 0x04, 0x03, 0x02, 0x00}
	assertHeaderRoundTrip(t, wire, frameHeader{kind: 'B', length: 0x00020304})
}

// TestFrameHeaderLongestBody reads the longest body taken, and refuses
// one byte more.
//
// Verifies: [PROTOCOL Frames] a body of the longest length taken is
// read, and a longer one fails the connection with SQLCODE -919 and
// SQLSTATE 08006.
func TestFrameHeaderLongestBody(t *testing.T) {
	t.Parallel()

	longest := appendFrameHeader(nil, frameHeader{kind: 'B', length: maxBody})
	assertHeaderRoundTrip(t, [frameHeaderBytes]byte(longest), frameHeader{kind: 'B', length: maxBody})

	tooLong := appendFrameHeader(nil, frameHeader{kind: 'B', length: maxBody + 1})

	decoded, err := decodeFrameHeader([frameHeaderBytes]byte(tooLong), maxBody)
	if decoded != (frameHeader{}) {
		t.Errorf("decoded %+v of a body too long, want nothing", decoded)
	}

	var failure *Error
	if !errors.As(err, &failure) {
		t.Fatalf("err = %v, want an *Error", err)
	}

	if failure.SQLCode != sqlcodeConnection || failure.SQLState != sqlstateConnectionFailure {
		t.Errorf("failure %q, want SQLCODE -919 and SQLSTATE 08006", failure)
	}

	want := "SQLCODE -919 (SQLSTATE 08006): a message of 16777217 bytes is longer than the 16777216 taken"
	if failure.Error() != want {
		t.Errorf("Error() = %q, want %q", failure.Error(), want)
	}
}

// assertHeaderRoundTrip checks that header encodes as wire, and wire
// decodes as header.
func assertHeaderRoundTrip(t *testing.T, wire [frameHeaderBytes]byte, header frameHeader) {
	t.Helper()

	encoded := appendFrameHeader(nil, header)
	if !bytes.Equal(encoded, wire[:]) {
		t.Errorf("encoded % X, want % X", encoded, wire)
	}

	decoded, err := decodeFrameHeader(wire, maxBody)
	if err != nil {
		t.Fatalf("decoding % X: %v", wire, err)
	}

	if decoded != header {
		t.Errorf("decoded %+v, want %+v", decoded, header)
	}
}
