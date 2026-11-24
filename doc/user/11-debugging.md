# Debugging Routines

[Contents](README.md)

*(extension)* A procedure, a function or a trigger written in SQL can be
run under a debugger: another connection to the same server, which
attaches to the session that runs the routine, sets breakpoints at lines
of the routine's text, and, each time that session pauses, reads its
call stack, each frame's parameters, variables and cursors, and tells it
how to go on. Turborel's [Debugging
Routines](https://github.com/turborel/turborel/blob/main/doc/user/11-debugging.md)
tells the debugger itself.

A connection's `Debugger` does it, with `debugger` granted, which
connecting asks for. A connection attaches to the session of another
connection of its own user; the database's owner, and a user granted the
role MONITOR, to any connection's. The debugger pauses in, and sees
into, only the routines its user owns or holds DEBUG on; a frame of any
other is shown by its origin alone.

## Two connections

Here ANN's first connection, `conn`, has made the procedure, and a
second, `other`, debugs it.

```sql
CREATE TABLE STAFF (ID INTEGER PRIMARY KEY, SALARY INTEGER);
INSERT INTO STAFF VALUES (1, 50000), (2, 60000);
CREATE PROCEDURE RAISE_PAY(IN PCT INTEGER) MODIFIES SQL DATA
BEGIN
  DECLARE N INTEGER DEFAULT 0;
  UPDATE STAFF SET SALARY = SALARY * (100 + PCT) / 100;
  SET N = (SELECT COUNT(*) FROM STAFF);
END;
```

The debugger attaches by the first connection's ID and sets a breakpoint
at line 4 of the procedure's text, its UPDATE. The breakpoint names the
procedure by its specific name, as a routine's name may be overloaded.
The first connection's CALL pauses as it comes to line 4, and completes
only once the debugger lets it go on, so it runs in a goroutine of its
own.

```go
debugger := other.Debugger()
id, _ := conn.Debugger().ID(ctx)
fmt.Println(debugger.Attach(ctx, id))
set, _ := debugger.Break(ctx, turborel.RoutineProcedure, turborel.QualifiedName{Name: "RAISE_PAY"}, 4, "")
fmt.Println(set.Number, set.Routine, set.Line)

called := make(chan error, 1)
go func() {
	_, err := conn.Query(ctx, "CALL RAISE_PAY(10)")
	called <- err
}()
// <nil>
// 1 {ANN RAISE_PAY_1} 4
```

## Pausing and stepping

`Wait` gives the next event of the target, waiting at most the time it
is given. A pause carries the call stack, the outermost frame first, the
paused statement's last, with its line and column, and its parameters
and variables, each with its type as SQL writes it and its value.

```go
event, _ := debugger.Wait(ctx, 10*time.Second)
top := event.Pause.Frames[len(event.Pause.Frames)-1]
fmt.Println(event.Kind == turborel.EventPaused, event.Pause.Breakpoint, top.Origin.Specific, top.Line, top.Column)
for _, v := range append(top.Parameters, top.Variables...) {
	fmt.Println(v.Name, v.Type, v.Value)
}
// true 1 {ANN RAISE_PAY_1} 4 3
// PCT INTEGER 10
// N INTEGER 0
```

`Resume` lets the target go on: `StepInto` to its next statement in
whatever frame, `StepOver` to the next of this frame, `StepOut` to the
next of the frame this one returns to, or `Continue` to a breakpoint. A
pause at the end of a step is at breakpoint 0. `Stack` gives the pause
again, and `Interrupt` pauses a running target at its next statement.

```go
fmt.Println(debugger.Resume(ctx, turborel.StepOver))
event, _ = debugger.Wait(ctx, 10*time.Second)
top = event.Pause.Frames[len(event.Pause.Frames)-1]
fmt.Println(event.Pause.Breakpoint, top.Line)

fmt.Println(debugger.Resume(ctx, turborel.Continue))
fmt.Println(<-called)
fmt.Println(debugger.Detach(ctx))
// <nil>
// 0 5
// <nil>
// <nil>
// <nil>
```

`Breakpoints` lists the breakpoints and `Clear` clears one. `Detach`
ends the debugging: the breakpoints go, and the target, if paused, goes
on. A target paused longer than DEBUGGER.PAUSE_TIMEOUT_MS goes on by
itself, and `Wait` gives `EventTimedOut`; one whose statement is
cancelled, `EventCancelled`; one whose session ends, `EventEnded`.

An ID of no connection the user may see is refused with SQLSTATE 08003;
a breakpoint in a routine the user holds no DEBUG on, with 42000,
SQLCODE -104; an action the state does not allow, as `Resume` of a
target not paused, with TBD01. The session goes on after each. [The
Debugger](../client/31-debugger.md) gives every call.
