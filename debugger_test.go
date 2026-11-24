package turborel

import (
	"context"
	"errors"
	"testing"
	"time"
)

// writeBreakpoint writes b as a Debugged carries it.
func writeBreakpoint(w *writer, b Breakpoint) {
	w.u32(b.Number)
	w.str(b.Routine.Schema)
	w.str(b.Routine.Name)
	w.u32(b.Line)
	w.given(b.Condition)
}

// writePause writes p as a Debugged or an Event carries it, each frame
// of one parameter.
func writePause(w *writer, p *Pause) {
	if p.Breakpoint == 0 {
		w.u8(pauseStepped)
	} else {
		w.u8(pauseAtBreakpoint)
		w.u32(p.Breakpoint)
	}

	w.u32(count32(len(p.Frames)))

	for _, f := range p.Frames {
		w.flag(false)
		w.flag(f.Visible)
		w.flag(false)
		w.flag(false)
		w.u32(count32(len(f.Parameters)))

		for _, v := range f.Parameters {
			w.str(v.Name)
			w.str(v.Type)
			v.Value.encodeValue(w)
		}

		w.u32(0)
		w.u32(0)
	}
}

// debugged replies a Debugged of what, its body written by body.
func (f *fake) debugged(what byte, body func(w *writer)) {
	f.reply(kindDebugged, func(w *writer) {
		w.u8(what)
		body(w)
	})
}

// event replies an Event of kind, of pause if it is a pause.
func (f *fake) event(kind EventKind, pause *Pause) {
	f.reply(kindEvent, func(w *writer) {
		w.u8(byte(kind))

		if pause != nil {
			writePause(w, pause)
		}
	})
}

// debugAsked reads a Debug, giving what it asks.
func (f *fake) debugAsked() byte {
	return f.expect(kindDebug)[0]
}

// stepped is a pause at the end of a step, of one frame of X = 1.
func stepped() *Pause {
	return &Pause{Frames: []StackFrame{{
		Visible: true, Parameters: []Variable{{Name: "X", Type: "INTEGER", Value: DecimalOf(1, 0)}},
	}}}
}

// debugScript answers each of the debugger's calls in TestDebuggerCalls.
func debugScript(f *fake) {
	set := Breakpoint{Number: 1, Routine: QualifiedName{"S", "P"}, Line: 4, Condition: "X > 1"}
	replies := []func(){
		func() { f.debugged(debuggedConnection, func(w *writer) { w.u64(7) }) },
		func() { f.done(false) },
		func() { f.debugged(debuggedBreakpoint, func(w *writer) { writeBreakpoint(w, set) }) },
		func() { f.done(false) },
		func() {
			f.debugged(debuggedBreakpoints, func(w *writer) {
				w.u32(1)
				writeBreakpoint(w, set)
			})
		},
		func() { f.done(false) },
		func() { f.done(false) },
		func() {
			f.debugged(debuggedStack, func(w *writer) {
				w.flag(true)
				writePause(w, stepped())
			})
		},
		func() { f.done(false) },
	}

	asks := []byte{
		debugIdentify, debugAttach, debugBreak, debugClear, debugBreakpoints, debugResume, debugInterrupt,
		debugStack, debugDetach,
	}

	for i, reply := range replies {
		if what := f.debugAsked(); what != asks[i] {
			f.t.Errorf("Debug %d asked %d", i, what)
		}

		reply()
	}
}

// TestDebuggerCalls drives a debugger's every call.
//
// Verifies: [PROTOCOL Debug 'V'] each ask is a Debug of its own code,
// identify to stack: identify, break, breakpoints and stack answered by
// a Debugged of what they ask; attach, detach, clear, resume and
// interrupt by a Complete [PROTOCOL Debugged 'V'] [CLIENT Debugging
// routines].
func TestDebuggerCalls(t *testing.T) {
	t.Parallel()

	c := openGranted(t, []string{CapabilityDebugger}, debugScript)
	d, ctx := c.Debugger(), context.Background()

	id, err := d.ID(ctx)
	if err != nil || id != 7 || d.Attach(ctx, 2) != nil {
		t.Fatalf("ID %d, %v", id, err)
	}

	set, err := d.Break(ctx, RoutineProcedure, QualifiedName{"S", "P"}, 4, "X > 1")
	if err != nil || set.Number != 1 || set.Condition != "X > 1" || d.Clear(ctx, 1) != nil {
		t.Fatalf("Break gave %+v, %v", set, err)
	}

	assertDebuggerGoesOn(t, d)
}

// assertDebuggerGoesOn lists the breakpoints, resumes, interrupts, reads
// the stack and detaches.
func assertDebuggerGoesOn(t *testing.T, d *Debugger) {
	t.Helper()

	ctx := context.Background()

	all, err := d.Breakpoints(ctx)
	if err != nil || len(all) != 1 || d.Resume(ctx, StepOver) != nil || d.Interrupt(ctx) != nil {
		t.Fatalf("Breakpoints gave %v, %v", all, err)
	}

	paused, err := d.Stack(ctx)
	if err != nil || paused == nil || paused.Frames[0].Parameters[0].Name != "X" || d.Detach(ctx) != nil {
		t.Fatalf("Stack gave %+v, %v", paused, err)
	}
}

// TestDebuggerEventsKept keeps an Event read among a request's replies.
//
// Verifies: [PROTOCOL Event 'W'] an Event is read wherever a reply is
// read and never ends a request's replies: one before a Query's reply
// and one among its rows are kept, and Wait gives them, in order, with
// nothing more read; Detach drops what is kept [CLIENT Debugging
// routines].
func TestDebuggerEventsKept(t *testing.T) {
	t.Parallel()

	c := openGranted(t, []string{CapabilityDebugger}, func(f *fake) {
		f.expect(kindQuery)
		f.event(EventTimedOut, nil)
		f.reply(kindDescription, func(w *writer) { encodeColumns(w, nil) })
		f.event(EventPaused, stepped())
		f.reply(kindRows, func(w *writer) { w.u32(0) })
		f.done(false)
		f.event(EventCancelled, nil)
		f.debugAsked()
		f.done(false)
	})

	if _, err := c.Query(context.Background(), "VALUES 1"); err != nil {
		t.Fatal(err)
	}

	d := c.Debugger()

	first, err := d.Wait(context.Background(), 0)
	if err != nil || first.Kind != EventTimedOut {
		t.Fatalf("Wait gave %+v, %v", first, err)
	}

	second, err := d.Wait(context.Background(), 0)
	if err != nil || second.Kind != EventPaused || second.Pause.Frames[0].Parameters[0].Value != DecimalOf(1, 0) {
		t.Fatalf("Wait gave %+v, %v", second, err)
	}

	third, err := d.Wait(context.Background(), 0)
	if err != nil || third.Kind != EventCancelled {
		t.Fatalf("Wait gave %+v, %v", third, err)
	}

	c.events = []DebugEvent{{Kind: EventEnded}}

	if err := d.Detach(context.Background()); err != nil || len(c.events) != 0 {
		t.Errorf("Detach gave %v, %d kept", err, len(c.events))
	}
}

// TestDebuggerWait waits for events, a while or until a context is done.
//
// Verifies: [CLIENT Debugging routines] Wait gives the next event the
// server sends; none once its time passes, the connection kept; a
// context done gives the wait up with its error, the connection kept; a
// Notice is the session's, and gives none; a reply of no request loses
// the connection (SQLSTATE 08006) [PROTOCOL Event 'W'].
func TestDebuggerWait(t *testing.T) {
	t.Parallel()

	c := openGranted(t, []string{CapabilityDebugger, CapabilityNotices}, func(f *fake) {
		f.debugAsked()
		f.done(false)
		f.event(EventEnded, nil)
		f.reply(kindNotice, func(w *writer) { w.str("01000"); w.str("told") })
		f.debugAsked()
		f.done(false)
		f.done(false)
	})

	d, ctx := c.Debugger(), context.Background()

	if none, err := d.Wait(ctx, 20*time.Millisecond); none != nil || err != nil || c.IsLost() {
		t.Fatalf("a wait that ran out gave %+v, %v", none, err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	time.AfterFunc(20*time.Millisecond, cancel)

	if none, err := d.Wait(cancelled, 0); none != nil || !errors.Is(err, context.Canceled) || c.IsLost() {
		t.Fatalf("a wait cancelled gave %+v, %v", none, err)
	}

	if err := d.Interrupt(ctx); err != nil {
		t.Fatal(err)
	}

	ended, err := d.Wait(ctx, time.Second)
	if err != nil || ended.Kind != EventEnded {
		t.Fatalf("Wait gave %+v, %v", ended, err)
	}

	if none, err := d.Wait(ctx, time.Second); none != nil || err != nil || len(c.notices) != 1 {
		t.Fatalf("a Notice gave %+v, %v", none, err)
	}

	if err := d.Interrupt(ctx); err != nil {
		t.Fatal(err)
	}

	_, err = d.Wait(ctx, time.Second)
	assertState(t, err, sqlstateConnectionFailure)
}

// TestDebuggerRefused refuses and fails the debugger's calls.
//
// Verifies: [PROTOCOL Debug 'V'] a call without `debugger` granted fails
// before sending (SQLCODE -919, SQLSTATE 08006), the connection kept; the
// server's refusal, as TBD01, keeps the session; a Debugged of another
// ask's kind, or of no kind, an Event of no kind, a pause of no reason
// and a frame of no kind of routine lose the connection [PROTOCOL
// Debugged 'V'] [PROTOCOL Event 'W'] [CLIENT Debugging routines].
func TestDebuggerRefused(t *testing.T) {
	t.Parallel()

	plain := openGranted(t, nil, func(*fake) {})
	_, err := plain.Debugger().ID(context.Background())
	assertState(t, err, sqlstateConnectionFailure)

	if _, err := plain.Debugger().Wait(context.Background(), 0); err == nil || plain.IsLost() {
		t.Error("Wait without debugger granted")
	}

	refused := openGranted(t, []string{CapabilityDebugger}, func(f *fake) {
		f.debugAsked()
		f.failure(-910, "TBD01")
	})

	err = refused.Debugger().Resume(context.Background(), Continue)
	if assertState(t, err, "TBD01"); refused.IsLost() {
		t.Error("a refusal lost the connection")
	}

	for name, body := range malformedDebugged() {
		c := openGranted(t, []string{CapabilityDebugger}, func(f *fake) {
			f.debugAsked()
			f.reply(body.kind, body.write)
		})

		_, err := c.Debugger().ID(context.Background())
		if assertState(t, err, sqlstateConnectionFailure); !c.IsLost() {
			t.Errorf("%s kept the connection", name)
		}
	}
}

// malformedReply is a reply a Debug may not be answered with.
type malformedReply struct {
	kind  byte
	write func(w *writer)
}

// malformedDebugged gives replies of the debugger's that lose the
// connection.
func malformedDebugged() map[string]malformedReply {
	stackOf := func(reason, kind byte) func(w *writer) {
		return func(w *writer) {
			w.u8(byte(EventPaused))
			w.u8(reason)
			w.u32(1)
			w.flag(true)
			w.u8(kind)
		}
	}

	return map[string]malformedReply{
		"another kind": {kindDebugged, func(w *writer) {
			w.u8(debuggedStack)
			w.flag(false)
		}},
		"no kind":            {kindDebugged, func(w *writer) { w.u8(9) }},
		"an event":           {kindEvent, func(w *writer) { w.u8(9) }},
		"no reason":          {kindEvent, stackOf(5, 0)},
		"no kind of routine": {kindEvent, stackOf(pauseStepped, 3)},
	}
}
