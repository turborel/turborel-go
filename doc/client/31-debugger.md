# The Debugger

[Contents](README.md)

## NAME

`Debugger`: a connection that drives the routines another connection's
session runs. *(extension)*

## SYNOPSIS

```go
func (c *Conn) Debugger() *Debugger
func (d *Debugger) ID(ctx context.Context) (uint64, error)
func (d *Debugger) Attach(ctx context.Context, id uint64) error
func (d *Debugger) Detach(ctx context.Context) error
func (d *Debugger) Break(ctx context.Context, kind RoutineKind, routine QualifiedName, line uint32,
	condition string) (Breakpoint, error)
func (d *Debugger) Clear(ctx context.Context, number uint32) error
func (d *Debugger) Breakpoints(ctx context.Context) ([]Breakpoint, error)
func (d *Debugger) Wait(ctx context.Context, within time.Duration) (*DebugEvent, error)
func (d *Debugger) Stack(ctx context.Context) (*Pause, error)
func (d *Debugger) Resume(ctx context.Context, how ResumeAction) error
func (d *Debugger) Interrupt(ctx context.Context) error

type RoutineKind uint8   // RoutineProcedure, RoutineFunction, RoutineTrigger
type ResumeAction uint8  // StepInto, StepOver, StepOut, Continue
type EventKind uint8     // EventPaused, EventTimedOut, EventCancelled, EventEnded
type Breakpoint struct {
	Number    uint32
	Routine   QualifiedName
	Line      uint32
	Condition string
}
type DebugEvent struct {
	Kind  EventKind
	Pause *Pause
}
type Pause struct {
	Breakpoint uint32
	Frames     []StackFrame
}
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
type Origin struct {
	Kind     RoutineKind
	Specific QualifiedName
	Name     QualifiedName
}
type Variable struct {
	Name  string
	Type  string
	Value Value
}
type FrameCursor struct {
	Name  string
	Open  bool
	Query string
}
```

## DESCRIPTION

With `debugger` granted, which connecting asks for, one connection
debugs the session of another. `ID` gives a connection's ID, as
INFORMATION_SCHEMA.CONNECTIONS shows it, and `Attach` makes this
connection the debugger of the session of the connection of that ID:
one of its own user's, or any if its user is the database's owner or
holds MONITOR. `Detach` ends the debugging: its breakpoints go, its
target, if paused, goes on, and the events kept are dropped.

`Break` sets a breakpoint at a line, from 1, of the text of a
procedure, a function or a trigger, named by its specific name, or its
name if it is the one routine of that kind so named; an empty schema is
the session's default. A condition, a `<search condition>` over the
frame's parameters and variables, pauses only where it is true; an
empty one is none. The breakpoint given names its routine by its
specific name. `Clear` clears one by its number, and `Breakpoints` lists
them in the order they were set. The debugger pauses in, and sees into,
only the routines its user owns or holds DEBUG on.

`Wait` gives the next event of the target: a pause, with its call stack,
the outermost frame first; a timeout, as the target paused past
DEBUGGER.PAUSE_TIMEOUT_MS went on by itself; a cancel; or the target's
end, which detaches the debugger. An event comes unasked, between
requests; one read among another request's replies is kept for `Wait`.
`Wait` waits at most `within`, without limit if 0, and gives nil if none
came; a context done gives the wait up with its error, the connection
kept. A Notice or a Status read as it waits is the session's, and gives
nil. A connection over a stream that takes no deadline waits only
without limit, and only of a context never done.

A pause is at the breakpoint of its number, or, of 0, at the end of a
step or as the debugger asked. A frame names its routine by its specific
name and by its name; a statement run directly has no origin. A frame the
debugger may not see into is not `Visible`, and shows its origin alone.
Its place counts characters from the beginning of the routine's text,
its line and column from 1. Its parameters and variables carry their
types as SQL writes them and their values; a trigger's parameters are
its transition variables. `Stack` gives the pause again, nil if the
target runs. `Resume` tells the paused target to go on: `StepInto` to
its next statement in whatever frame, `StepOver` to the next of this
frame or one it returns to, `StepOut` to the next of the frame this one
returns to, `Continue` to a breakpoint. `Interrupt` pauses the running
target at its next statement.

The target runs its statements as ever; its call returns once the
debugger lets it go on. The debugger usually waits in a goroutine of
its own.

## ERRORS

| SQLSTATE | When |
|---|---|
| 08003 | `Attach` of an ID of no connection served whose session this one may debug |
| 42000 | SQLCODE -104: a breakpoint in a routine the user holds no DEBUG on; -103, no such routine or line |
| TBD01 | SQLCODE -910: an action the state does not allow, as `Resume` of a target not paused, a second `Attach`, or any call but `ID` and `Attach` attached to none |
| 08006 | SQLCODE -919, before anything is sent, the connection kept: `debugger` not granted, or a bounded `Wait` on a stream that takes no deadline |
| 08006 | SQLCODE -919: a Debugged of another kind than asked, a malformed one or a malformed Event, a reply of no request where `Wait` waits, or the connection lost |

## EXAMPLES

The examples of [Debugging Routines](../user/11-debugging.md).

## STANDARD

*(extension)* [PROTOCOL Debug 'V'], [PROTOCOL Debugged 'V'] and [PROTOCOL Event 'W'] of [The Native Protocol](https://github.com/turborel/turborel/blob/main/doc/client/05-protocol.md); [CLIENT Debugging routines] of [The Client Library](https://github.com/turborel/turborel/blob/main/doc/client/02-library.md).

## SEE ALSO

[Debugging Routines](../user/11-debugging.md), [Connections](21-connections.md), [Statements and Cursors](22-statements.md), [Errors](30-errors.md).
