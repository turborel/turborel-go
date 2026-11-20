package turborel

// Cancelling a statement from another goroutine [PROTOCOL Cancel 'Z']
// [CLIENT Running statements]: a connection of its own, whose first and
// only message is Cancel with the key the session's Ready gave. The
// server closes it with no reply, and the statement fails with SQLCODE
// -920, SQLSTATE TB920.

import (
	"context"
	"io"
	"time"
)

// Canceller cancels the statement a connection runs, from another
// goroutine. It holds the session's cancel key, a secret, which its
// String never shows.
type Canceller struct {
	target Target
	key    [keyBytes]byte
}

// Canceller gives what cancels the statement this connection runs; false
// for one opened over a program's stream, whose server it cannot reach
// again.
func (c *Conn) Canceller() (*Canceller, bool) {
	if c.target == nil {
		return nil, false
	}

	return &Canceller{target: *c.target, key: c.key}, true
}

// String never shows the key.
func (*Canceller) String() string { return "Canceller(..)" }

// GoString never shows the key.
func (*Canceller) GoString() string { return "Canceller(..)" }

// Cancel asks the server to cancel the statement, giving up after 10
// seconds if it does not answer.
func (k *Canceller) Cancel(ctx context.Context) error {
	return k.CancelWithin(ctx, cancelWithin)
}

// CancelWithin asks the server to cancel the statement, giving up after
// within.
func (k *Canceller) CancelWithin(ctx context.Context, within time.Duration) error {
	s, _, err := k.target.open(ctx, within)
	if err != nil {
		return err
	}

	defer func() { _ = s.Close() }() // The server closes it; its close is nothing to report.

	_ = s.SetDeadline(deadlineOf(within)) // A stream that takes no deadline is bounded by its maker.

	if _, err := s.Write(encodeRequest(cancelRequest{key: k.key})); err != nil {
		return ioFailed(verbSend, err)
	}

	if _, err := io.Copy(io.Discard, s); err != nil {
		return ioFailed(verbReceive, err)
	}

	return nil
}
