package turborel

// The routine debugger, from a client [CLIENT Debugging routines], with
// `debugger` *(extension)* granted: a connection attaches to the session
// of another connection, by that one's ID, sets breakpoints in the
// routines and triggers its user may debug, and drives that session's
// routines as they pause, step by step, reading their call stack,
// parameters, variables and cursors. What it hears of its target, a
// DebugEvent, comes unasked, between requests, and is kept until Wait
// reads it. The target runs its statements as ever; the debugger waits
// for its events beside it, often in another goroutine.

import (
	"context"
	"errors"
	"net"
	"time"
)

// Debugger drives the routines another connection's session runs, over
// the connection that made it; it is that connection's, and as safe to
// share between goroutines as it is.
type Debugger struct {
	conn *Conn
}

// Debugger gives the connection's debugger. Each of its calls needs
// `debugger` granted, and fails before sending anything if it was not.
func (c *Conn) Debugger() *Debugger { return &Debugger{conn: c} }

// ID gives this connection's ID, as INFORMATION_SCHEMA.CONNECTIONS shows
// it: what another connection's debugger attaches to its session by.
func (d *Debugger) ID(ctx context.Context) (uint64, error) {
	got, err := d.ask(ctx, debugRequest{what: debugIdentify}, debuggedConnection)
	if err != nil {
		return 0, err
	}

	return got.connection, nil
}

// Attach makes this connection the debugger of the session of the
// connection of id: one of its own user's, or any if its user is the
// database's owner or holds MONITOR. Any other ID is refused with
// SQLSTATE 08003; this connection attached already, or the session its
// own or debugged already, with TBD01.
func (d *Debugger) Attach(ctx context.Context, id uint64) error {
	return d.done(ctx, debugRequest{what: debugAttach, connection: id})
}

// Detach ends the debugging: its breakpoints go, its target, if paused,
// goes on, and the events kept are dropped.
func (d *Debugger) Detach(ctx context.Context) error {
	d.conn.events = nil

	return d.done(ctx, debugRequest{what: debugDetach})
}

// Break sets a breakpoint at line, from 1, of the text of the routine of
// kind named routine, its specific name or, if it is the one routine of
// that kind so named, its name; or of the trigger routine. An empty
// schema is the session's default. The target pauses at each statement
// that begins on that line, where condition, a <search condition> over
// the frame's parameters and variables, is true, if it is not empty. A
// routine the user holds no DEBUG on is refused with SQLSTATE 42000,
// SQLCODE -104.
func (d *Debugger) Break(ctx context.Context, kind RoutineKind, routine QualifiedName, line uint32,
	condition string,
) (Breakpoint, error) {
	req := debugRequest{what: debugBreak, kind: kind, routine: routine, line: line, condition: condition}

	got, err := d.ask(ctx, req, debuggedBreakpoint)
	if err != nil {
		return Breakpoint{}, err
	}

	return got.breakpoints[0], nil
}

// Clear clears the breakpoint of number.
func (d *Debugger) Clear(ctx context.Context, number uint32) error {
	return d.done(ctx, debugRequest{what: debugClear, number: number})
}

// Breakpoints gives the breakpoints set, in the order they were set.
func (d *Debugger) Breakpoints(ctx context.Context) ([]Breakpoint, error) {
	got, err := d.ask(ctx, debugRequest{what: debugBreakpoints}, debuggedBreakpoints)
	if err != nil {
		return nil, err
	}

	return got.breakpoints, nil
}

// Resume tells the paused target how to go on: StepInto, StepOver,
// StepOut or Continue. One not paused is refused with SQLSTATE TBD01.
func (d *Debugger) Resume(ctx context.Context, how ResumeAction) error {
	return d.done(ctx, debugRequest{what: debugResume, action: how})
}

// Interrupt tells the running target to pause at the next statement it
// runs of a routine the debugger may debug.
func (d *Debugger) Interrupt(ctx context.Context) error {
	return d.done(ctx, debugRequest{what: debugInterrupt})
}

// Stack gives the target's pause, with its call stack, the outermost
// frame first; nil if it runs.
func (d *Debugger) Stack(ctx context.Context) (*Pause, error) {
	got, err := d.ask(ctx, debugRequest{what: debugStack}, debuggedStack)
	if err != nil {
		return nil, err
	}

	return got.pause, nil
}

// Wait gives the next event of the target not yet read: one kept, else
// the next the server sends, waited for at most within, without limit if
// 0; nil if none came. A Notice or a Status read as it waits is the
// session's, and Wait gives nil. ctx done gives the wait up with its
// error, the connection kept, as nothing of a reply was read. It reads
// between requests, never while one is answered. A connection over a
// stream that takes no deadline waits only without limit, and only of a
// context never done.
func (d *Debugger) Wait(ctx context.Context, within time.Duration) (*DebugEvent, error) {
	c := d.conn
	if err := c.needs(CapabilityDebugger); err != nil {
		return nil, err
	}

	if len(c.events) > 0 {
		kept := c.events[0]
		c.events = c.events[1:]

		return &kept, nil
	}

	arrived, err := c.arrives(ctx, within)
	if err != nil || !arrived {
		return nil, err
	}

	return c.unasked()
}

// ask sends req and reads its Debugged, which must be of kind what.
func (d *Debugger) ask(ctx context.Context, req debugRequest, what byte) (debuggedReply, error) {
	c := d.conn
	if err := c.needs(CapabilityDebugger); err != nil {
		return debuggedReply{}, err
	}

	defer c.during(ctx)()

	if err := c.send(req); err != nil {
		return debuggedReply{}, withCause(err, ctx.Err())
	}

	rep, err := c.receive()
	if err != nil {
		return debuggedReply{}, withCause(err, ctx.Err())
	}

	switch r := rep.(type) {
	case debuggedReply:
		if r.what == what {
			return r, nil
		}
	case errorReply:
		return debuggedReply{}, r.failure
	}

	return debuggedReply{}, c.lose("the server sent no Debugged of what Debug asked")
}

// done sends req and reads its completion.
func (d *Debugger) done(ctx context.Context, req debugRequest) error {
	if err := d.conn.needs(CapabilityDebugger); err != nil {
		return err
	}

	return d.conn.completed(ctx, req)
}

// errNoDeadline is a wait bounded on a stream that takes no deadline.
var errNoDeadline = errors.New("the connection's stream takes no deadline: wait without limit")

// arrives says whether a frame begins to arrive within within, without
// limit if 0, nothing of it read; false once within passes, or, with
// ctx's error, once ctx is done.
func (c *Conn) arrives(ctx context.Context, within time.Duration) (bool, error) {
	if c.lost != nil {
		return false, c.lost
	}

	bounded := within > 0 || ctx.Done() != nil
	if err := c.stream.SetDeadline(deadlineOf(within)); err != nil && bounded {
		return false, broken(errNoDeadline.Error())
	}

	stop := context.AfterFunc(ctx, func() { _ = c.stream.SetDeadline(time.Unix(1, 0)) })
	_, err := c.r.Peek(1)

	stop()

	if err == nil {
		return true, nil
	}

	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return false, ctx.Err() //nolint:wrapcheck // The context's own error, as errors.Is reads it.
	}

	return false, c.failed(ioFailed(verbReceive, err))
}

// unasked reads one reply sent unasked: an Event; a Notice or a Status,
// taken as the session's, gives none; any other loses the connection.
func (c *Conn) unasked() (*DebugEvent, error) {
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
	case eventReply:
		return &r.event, nil
	case noticeReply, statusReply:
		c.took(rep)

		return nil, nil //nolint:nilnil // No event came, and no failure.
	case errorReply:
		c.noted(r)

		return nil, r.failure
	}

	return nil, c.lose("the server answered no request")
}
