package turborel

// How long a connection waits on its server [CLIENT Timeouts]: to
// connect, begin TLS and for each step of the login at most the connect
// timeout, 30 seconds by default; then each read or write of a request
// or reply at most the read timeout, none by default, as a statement may
// run as long as it must. A wait that runs out loses the connection,
// saying the server did not answer in time.

import (
	"errors"
	"net"
	"os"
	"time"
)

// The waits turborel-client takes by default.
const (
	// connectWithin is how long connecting and each step of the login
	// wait, CONNECT_WITHIN.
	connectWithin = 30 * time.Second
	// terminateWithin is how long Terminate waits for the server to end
	// the session and close, unless the read timeout says,
	// TERMINATE_WITHIN.
	terminateWithin = 60 * time.Second
	// cancelWithin is how long a Cancel waits for the server, CANCEL_WITHIN.
	cancelWithin = 10 * time.Second
)

// What the driver was doing when a wait or a read failed, as its
// failure says.
const (
	verbConnect = "connect"
	verbSend    = "send"
	verbReceive = "receive"
)

// Timeouts says how long a connection waits on its server; 0 is no
// limit.
type Timeouts struct {
	// Connect bounds connecting, beginning TLS, and each step of the
	// login.
	Connect time.Duration
	// Read bounds each read or write of the server once logged in.
	Read time.Duration
}

// DefaultTimeouts are turborel-client's: 30 seconds to connect and for
// each step of the login, then no limit.
func DefaultTimeouts() Timeouts {
	return Timeouts{Connect: connectWithin}
}

// ioFailed is the connection failure of what the driver could not do,
// saying so where the server took longer than the connection waits.
func ioFailed(what string, err error) *Error {
	var timeout net.Error
	if errors.Is(err, os.ErrDeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout() {
		return broken("cannot " + what + ": the server did not answer in time (" + err.Error() + ")")
	}

	return broken("cannot " + what + ": " + err.Error())
}

// deadlineOf is the moment a wait of within from now ends; the zero
// time, no limit.
func deadlineOf(within time.Duration) time.Time {
	if within <= 0 {
		return time.Time{}
	}

	return time.Now().Add(within)
}
