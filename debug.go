package turborel

// The routine debugger's messages [PROTOCOL Debug 'V'], with `debugger`
// *(extension)*: Debug, what a debugger's connection asks of the session
// it debugs, another connection's; Debugged, what it gives back but a
// completion; and Event, what the debugger hears of that session, sent
// unasked between requests.

import "fmt"

// The kinds of the debugger's messages: Debug and Debugged are both 'V',
// read in the direction they travel.
const (
	kindDebug    byte = 'V'
	kindDebugged byte = 'V'
	kindEvent    byte = 'W'
)

// What a Debug asks [PROTOCOL Debug 'V'].
const (
	debugIdentify    byte = 0
	debugAttach      byte = 1
	debugDetach      byte = 2
	debugBreak       byte = 3
	debugClear       byte = 4
	debugBreakpoints byte = 5
	debugResume      byte = 6
	debugInterrupt   byte = 7
	debugStack       byte = 8
)

// What a Debugged gives [PROTOCOL Debugged 'V'].
const (
	debuggedConnection  byte = 0
	debuggedBreakpoint  byte = 1
	debuggedBreakpoints byte = 2
	debuggedStack       byte = 3
)

// Why a session paused [PROTOCOL Event 'W'].
const (
	pauseAtBreakpoint byte = 0
	pauseStepped      byte = 1
)

// RoutineKind is what a breakpoint's or a frame's routine is.
type RoutineKind uint8

// The kinds of routine, coded as the protocol codes them.
const (
	// RoutineProcedure is an SQL-invoked procedure.
	RoutineProcedure RoutineKind = 0
	// RoutineFunction is an SQL-invoked function.
	RoutineFunction RoutineKind = 1
	// RoutineTrigger is a trigger's triggered action.
	RoutineTrigger RoutineKind = 2
)

// ResumeAction is how a paused session goes on.
type ResumeAction uint8

// The actions, coded as the protocol codes them.
const (
	// StepInto pauses at the next statement, in whatever frame it is.
	StepInto ResumeAction = 0
	// StepOver pauses at the next statement of this frame or one it
	// returns to.
	StepOver ResumeAction = 1
	// StepOut pauses at the next statement of the frame this one returns
	// to.
	StepOut ResumeAction = 2
	// Continue runs until a breakpoint.
	Continue ResumeAction = 3
)

// Breakpoint is a breakpoint a debugger set: its number, which clears it,
// its routine by its specific name, or its trigger by its name, its line
// from 1, and the condition it pauses only where true, empty if none.
type Breakpoint struct {
	Number    uint32
	Routine   QualifiedName
	Line      uint32
	Condition string
}

// EventKind is what a debugger hears of its target.
type EventKind uint8

// The events, coded as the protocol codes them.
const (
	// EventPaused says the target paused, at a breakpoint or a step.
	EventPaused EventKind = 0
	// EventTimedOut says the target, paused past
	// DEBUGGER.PAUSE_TIMEOUT_MS, went on by itself.
	EventTimedOut EventKind = 1
	// EventCancelled says the target's statement was cancelled while it
	// was paused.
	EventCancelled EventKind = 2
	// EventEnded says the target's session ended: the debugger is
	// detached.
	EventEnded EventKind = 3
)

// DebugEvent is what a debugger hears of its target: its kind, and of a
// pause the pause.
type DebugEvent struct {
	Kind  EventKind
	Pause *Pause
}

// Pause is a paused session: at the breakpoint of number Breakpoint, or,
// of 0, at the end of a step or as the debugger asked; and its call
// stack, the outermost frame first, the paused statement's last.
type Pause struct {
	Breakpoint uint32
	Frames     []StackFrame
}

// StackFrame is a run of a routine's or a trigger's body. Origin is nil of
// a statement run directly. A frame the debugger may not see into is not
// Visible, and shows its origin alone. Place is where its statement
// begins, in characters from the beginning of the routine's text, if
// Placed; Line and Column count from 1, 0 if not told. A trigger's
// Parameters are its transition variables.
type StackFrame struct {
	Origin     *Origin
	Visible    bool
	Place      uint64
	Placed     bool
	Line       uint32
	Column     uint32
	Parameters []Variable
	Variables  []Variable
	Cursors    []FrameCursor
}

// Origin is the routine or trigger a frame runs the body of: its kind, its
// specific name, which a breakpoint names it by, and its name.
type Origin struct {
	Kind     RoutineKind
	Specific QualifiedName
	Name     QualifiedName
}

// Variable is a frame's parameter or variable: its name, its declared
// type as SQL writes it, and its value.
type Variable struct {
	Name  string
	Type  string
	Value Value
}

// FrameCursor is a frame's local cursor: its name, whether it is open, and
// its query as written.
type FrameCursor struct {
	Name  string
	Open  bool
	Query string
}

// debugRequest is a Debug [PROTOCOL Debug 'V']: what it asks and that
// ask's fields.
type debugRequest struct {
	what       byte
	connection uint64
	kind       RoutineKind
	routine    QualifiedName
	line       uint32
	condition  string
	number     uint32
	action     ResumeAction
}

func (q debugRequest) encode(w *writer) {
	w.frame(kindDebug, func() {
		w.u8(q.what)

		switch q.what {
		case debugAttach:
			w.u64(q.connection)
		case debugBreak:
			w.u8(byte(q.kind))
			w.str(q.routine.Schema)
			w.str(q.routine.Name)
			w.u32(q.line)
			w.given(q.condition)
		case debugClear:
			w.u32(q.number)
		case debugResume:
			w.u8(byte(q.action))
		}
	})
}

// given writes a flag, then s if it is not empty.
func (w *writer) given(s string) {
	w.flag(s != "")

	if s != "" {
		w.str(s)
	}
}

// debuggedReply is a Debugged [PROTOCOL Debugged 'V'].
type debuggedReply struct {
	what        byte
	connection  uint64
	breakpoints []Breakpoint
	pause       *Pause
}

// eventReply is an Event [PROTOCOL Event 'W'].
type eventReply struct {
	event DebugEvent
}

func (debuggedReply) isReply() {}
func (eventReply) isReply()    {}

// debugged reads a Debugged's body.
func (r *reader) debugged() debuggedReply {
	d := debuggedReply{what: r.u8()}

	switch d.what {
	case debuggedConnection:
		d.connection = r.u64()
	case debuggedBreakpoint:
		d.breakpoints = []Breakpoint{r.breakpoint()}
	case debuggedBreakpoints:
		n := r.count()
		for i := uint32(0); r.more(i, n); i++ {
			d.breakpoints = append(d.breakpoints, r.breakpoint())
		}
	case debuggedStack:
		if r.flag() {
			d.pause = r.pause()
		}
	default:
		r.fail(fmt.Sprintf("no Debugged is of kind %d", d.what))
	}

	return d
}

// breakpoint reads a breakpoint: number, routine, line, condition if
// given.
func (r *reader) breakpoint() Breakpoint {
	b := Breakpoint{Number: r.u32(), Routine: r.qualifiedName(), Line: r.u32()}
	if r.flag() {
		b.Condition = r.str()
	}

	return b
}

// qualifiedName reads a schema's name, then the object's.
func (r *reader) qualifiedName() QualifiedName {
	return QualifiedName{Schema: r.str(), Name: r.str()}
}

// event reads an Event's body.
func (r *reader) event() eventReply {
	e := DebugEvent{Kind: EventKind(r.u8())}

	switch e.Kind {
	case EventPaused:
		e.Pause = r.pause()
	case EventTimedOut, EventCancelled, EventEnded:
	default:
		r.fail(fmt.Sprintf("no Event is of kind %d", e.Kind))
	}

	return eventReply{event: e}
}

// pause reads a pause: its reason, then its frames.
func (r *reader) pause() *Pause {
	p := &Pause{}

	switch reason := r.u8(); reason {
	case pauseAtBreakpoint:
		p.Breakpoint = r.u32()
	case pauseStepped:
	default:
		r.fail(fmt.Sprintf("no pause is for reason %d", reason))
	}

	n := r.count()
	for i := uint32(0); r.more(i, n); i++ {
		p.Frames = append(p.Frames, r.stackFrame())
	}

	return p
}

// stackFrame reads a frame of the call stack.
func (r *reader) stackFrame() StackFrame {
	var f StackFrame

	if r.flag() {
		f.Origin = &Origin{Kind: r.routineKind(), Specific: r.qualifiedName(), Name: r.qualifiedName()}
	}

	f.Visible = r.flag()

	if f.Placed = r.flag(); f.Placed {
		f.Place = r.u64()
	}

	if r.flag() {
		f.Line, f.Column = r.u32(), r.u32()
	}

	f.Parameters = r.variables()
	f.Variables = r.variables()

	n := r.count()
	for i := uint32(0); r.more(i, n); i++ {
		f.Cursors = append(f.Cursors, FrameCursor{Name: r.str(), Open: r.flag(), Query: r.str()})
	}

	return f
}

// routineKind reads a routine's kind, failing of another code.
func (r *reader) routineKind() RoutineKind {
	kind := RoutineKind(r.u8())
	if r.err == nil && kind > RoutineTrigger {
		r.fail(fmt.Sprintf("%d is no kind of routine", kind))
	}

	return kind
}

// variables reads a list of parameters or variables: each its name, its
// type as SQL writes it, and its value.
func (r *reader) variables() []Variable {
	n := r.count()

	var list []Variable
	for i := uint32(0); r.more(i, n); i++ {
		list = append(list, Variable{Name: r.str(), Type: r.str(), Value: r.value()})
	}

	return list
}
