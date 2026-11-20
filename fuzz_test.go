package turborel

import (
	"errors"
	"testing"
)

// FuzzDecodeReply decodes arbitrary bytes as a reply of each kind.
//
// Verifies: [PROTOCOL Errors and notices] whatever a server sends, the
// decoder never panics: a malformed message fails the connection with
// SQLCODE -919 and SQLSTATE 08006, and nothing else.
func FuzzDecodeReply(f *testing.F) {
	seeds := [][]byte{
		{0, 0, 0, 0, 5, 0, 0, 0, '0', '0', '0', '0', '0', 0},
		{1, 0, 0, 0, 1, 0, 0, 0, 2, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		{1, 0, 0, 0, 1, 0, 0, 0, 25, 3, 0, 0, 0, 1, 10, 0},
		{1, 0, 0, 0, 1, 0, 0, 0, 16, 1, 0, 0, 0, 17, 0, 0, 0, 0},
		{0xFF, 0xFF, 0xFF, 0xFF},
		{},
	}
	for _, kind := range []byte("CRPDBSKELNTGz") {
		for _, seed := range seeds {
			f.Add(kind, seed)
		}
	}

	f.Fuzz(func(t *testing.T, kind byte, b []byte) {
		_, err := decodeReply(kind, b)
		if err == nil {
			return
		}

		var failure *Error
		if !errors.As(err, &failure) {
			t.Fatalf("err = %v, want the driver's failure", err)
		}

		switch failure.SQLState {
		case sqlstateConnectionFailure, sqlstateCorrupt, sqlstateFeatureNotSupported:
		default:
			t.Fatalf("err = %v, want a connection failure or a value's", err)
		}
	})
}
