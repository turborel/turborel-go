package turborel

// A connection's frames [CLIENT What the server tells]: each request
// encoded and sent, compressed once compression is granted, and each
// reply read and decoded, a Notice or a Status before a statement's
// reply taken on the way. A connection failure loses the connection.

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"
)

// keptNotices is how many notices are kept with no handler set, the
// last ones, as turborel-client's KEPT_NOTICES.
const keptNotices = 64

// The SQLSTATE classes and the one state that say whether a failure ends
// its connection [PROTOCOL Error 'E'].
const (
	classConnection            = "08"
	classAuthorization         = "28"
	sqlstateConnectionNotExist = "08003"
)

// The status names the server tells [PROTOCOL Status 'T'], gap G2's
// among them.
const (
	StatusAuthorization  = "AUTHORIZATION"
	StatusSchema         = "SCHEMA"
	StatusReadOnly       = "READ_ONLY"
	StatusServerVersion  = "SERVER_VERSION"
	StatusServerReadOnly = "SERVER_READ_ONLY"
	StatusCatalog        = "CATALOG"
	StatusOwner          = "OWNER"
)

// send writes one request, compressed past the threshold once
// compression is granted, within the connection's wait.
func (c *Conn) send(req request) error {
	if c.lost != nil {
		return c.lost
	}

	c.leaveCopy(req)

	if c.lost != nil {
		return c.lost
	}

	frames := encodeRequest(req)
	if c.compressing {
		frames = compressFrames(frames, c.compressFrom)
	}

	_ = c.stream.SetDeadline(deadlineOf(c.step)) // A stream that takes no deadline is bounded by its maker.

	if _, err := c.stream.Write(frames); err != nil {
		return c.loseBy(ioFailed(verbSend, err))
	}

	return nil
}

// receive gives the server's next reply, each Notice and Status before
// it taken, a compressed one inflated.
func (c *Conn) receive() (reply, error) {
	if c.lost != nil {
		return nil, c.lost
	}

	for {
		_ = c.stream.SetDeadline(deadlineOf(c.step)) // A stream that takes no deadline is bounded by its maker.

		kind, body, err := readFrame(c.r, c.maxBody, c.compressing)
		if err != nil {
			return nil, c.failed(err)
		}

		rep, err := decodeReply(kind, body)
		if err != nil {
			return nil, c.failed(err)
		}

		switch r := rep.(type) {
		case noticeReply:
			c.notice(r.notice)
		case statusReply:
			c.settings[r.name] = r.value
		default:
			return c.noted(rep), nil
		}
	}
}

// noted notes what a reply says of the session's transaction.
func (c *Conn) noted(rep reply) reply {
	switch r := rep.(type) {
	case completeReply:
		c.active = r.completion.Active
		done := r.completion
		c.last = &done

		if done.Committed {
			position := done.Position
			c.position = &position
		}
	case errorReply:
		c.active = r.failure.Active
		_ = c.failed(r.failure) // The failure is given with its reply; failed notes whether it lost the connection.
	case descriptionReply, rowsReply, suspendedReply, lobDataReply, lobLocatorReply:
		c.active = true
	}

	return rep
}

// notice hands a notice to the handler, or keeps it, the last 64.
func (c *Conn) notice(n Notice) {
	if c.handler != nil {
		c.handler(n)

		return
	}

	if len(c.notices) == keptNotices {
		c.notices = c.notices[1:]
	}

	c.notices = append(c.notices, n)
}

// SetNoticeHandler hands each notice from now on to handler, as it
// comes; nil, notices are kept, the last 64.
func (c *Conn) SetNoticeHandler(handler func(Notice)) { c.handler = handler }

// TakeNotices takes the notices kept.
func (c *Conn) TakeNotices() []Notice {
	taken := c.notices
	c.notices = nil

	return taken
}

// failed gives err, the connection lost if it ends it: a connection
// failure, an Error of class 08 but 08003, or of class 28 as the
// connection is made [PROTOCOL Error 'E'].
func (c *Conn) failed(err error) error {
	var failure *Error
	if errors.As(err, &failure) && endsConnection(failure) {
		c.lost = failure
	}

	return err
}

// endsConnection says whether failure ends the connection it came on.
func endsConnection(failure *Error) bool {
	connecting := failure.SQLCode == sqlcodeConnection && strings.HasPrefix(failure.SQLState, classAuthorization)
	another := failure.SQLState == sqlstateConnectionNotExist

	return strings.HasPrefix(failure.SQLState, classConnection) && !another || connecting
}

// lose loses the connection, for a connection failure of message.
func (c *Conn) lose(message string) *Error {
	return c.loseBy(broken(message))
}

// loseBy loses the connection, for failure.
func (c *Conn) loseBy(failure *Error) *Error {
	c.lost = failure

	return failure
}

// Close closes the connection at once, with no Terminate: the server
// ends the session as it would a client gone.
func (c *Conn) Close() error {
	if c.lost == nil {
		c.lost = broken("the connection was closed")
	}

	if err := c.stream.Close(); err != nil {
		return ioFailed("close", err)
	}

	return nil
}

// Terminate ends the session, its transaction as SESSION.COMMIT_AT_END
// says, once the server has closed the connection, waiting at most the
// read timeout, else 60 seconds [CLIENT Connecting]. A commit that fails
// as the session ends gives its failure, the transaction rolled back.
func (c *Conn) Terminate() error {
	defer func() { _ = c.stream.Close() }() // The connection ends whatever Terminate meets.

	if err := c.send(terminateRequest{}); err != nil {
		return err
	}

	within := c.timeouts.Read
	if within <= 0 {
		within = terminateWithin
	}

	_ = c.stream.SetDeadline(deadlineOf(within)) // A stream that takes no deadline is bounded by its maker.
	c.lost = broken("the session was terminated")

	rest, err := io.ReadAll(io.LimitReader(c.r, int64(c.maxBody)+frameHeaderBytes))
	if err != nil {
		return ioFailed(verbReceive, err)
	}

	return terminated(rest, c.maxBody)
}

// terminated reads what the server sent before closing, after
// Terminate: nothing, or a notice or status, ends well; an Error is the
// session's end failing.
func terminated(rest []byte, limit uint32) error {
	if len(rest) < frameHeaderBytes {
		return nil
	}

	h, err := decodeFrameHeader([frameHeaderBytes]byte(rest), limit)
	if err != nil {
		return err
	}

	// As turborel-client reads it, the body is all that follows the
	// header, so a second frame after the first fails as bytes left over.
	rep, err := decodeReply(h.kind, rest[frameHeaderBytes:])
	if err != nil {
		return err
	}

	switch r := rep.(type) {
	case errorReply:
		return r.failure
	case noticeReply, statusReply, pongReply:
		return nil
	default:
		return broken("the server answered Terminate")
	}
}

// Ping asks whether the server and the connection are alive, with no
// statement run: a Ping, answered by Pong from the connection's own
// task, so it answers while every worker is busy [PROTOCOL Ping 'G']. It
// needs `ping` granted, and fails before sending anything if it was not.
func (c *Conn) Ping(ctx context.Context) error {
	if err := c.needs(CapabilityPing); err != nil {
		return err
	}

	defer c.watch(ctx)()

	if err := c.send(pingRequest{}); err != nil {
		return withCause(err, ctx.Err())
	}

	rep, err := c.receive()
	if err != nil {
		return withCause(err, ctx.Err())
	}

	switch r := rep.(type) {
	case pongReply:
		return nil
	case errorReply:
		return r.failure
	default:
		return c.lose("the server answered Ping with another reply")
	}
}

// watch gives up the connection's waits once ctx is done, the
// connection then lost; the function it gives stops the watch.
func (c *Conn) watch(ctx context.Context) func() {
	stop := context.AfterFunc(ctx, func() {
		_ = c.stream.SetDeadline(time.Now()) // A stream that takes no deadline is closed instead.
		_ = c.stream.Close()
	})

	return func() { stop() }
}
