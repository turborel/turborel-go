package turborel

import (
	"context"
	"testing"
	"time"
)

// raisePay is DEBUG_TEST.RAISE_PAY, its UPDATE on line 4 of its text and
// its SET on line 5.
const raisePay = "CREATE PROCEDURE DEBUG_TEST.RAISE_PAY(IN PCT INTEGER) MODIFIES SQL DATA\n" +
	"BEGIN\n" +
	"  DECLARE N INTEGER DEFAULT 0;\n" +
	"  UPDATE DEBUG_TEST.STAFF SET SALARY = SALARY * (100 + PCT) / 100;\n" +
	"  SET N = (SELECT COUNT(*) FROM DEBUG_TEST.STAFF);\n" +
	"END"

// debugWait is how long a live test waits for its target to pause.
const debugWait = 10 * time.Second

// TestLiveDebugger debugs a procedure another connection calls.
//
// Verifies: [CLIENT Debugging routines] a debugger attaches to another
// connection's session by its ID, sets a breakpoint at a line of a
// procedure's text, waits for the pause and reads its frames, the
// procedure's parameters and variables; steps over to the next line,
// continues, lists and clears its breakpoints, and detaches; the target's
// CALL completes once let go [PROTOCOL Debug 'V'] [PROTOCOL Debugged 'V']
// [PROTOCOL Event 'W'].
func TestLiveDebugger(t *testing.T) {
	t.Parallel()

	target := liveSchema(t, "DEBUG_TEST")
	mustRun(t, target, "CREATE TABLE DEBUG_TEST.STAFF (ID INTEGER PRIMARY KEY, SALARY INTEGER)")
	mustRun(t, target, "INSERT INTO DEBUG_TEST.STAFF VALUES (1, 50000), (2, 60000)")
	mustRun(t, target, raisePay)
	mustRun(t, target, "COMMIT")

	ctx := context.Background()
	d := liveConnect(t, liveTLS(t, 0, false), Trust{}, nil).Debugger()

	id, err := target.Debugger().ID(ctx)
	if err != nil || d.Attach(ctx, id) != nil {
		t.Fatalf("ID %d, %v", id, err)
	}

	set, err := d.Break(ctx, RoutineProcedure, QualifiedName{"DEBUG_TEST", "RAISE_PAY"}, 4, "")
	if err != nil {
		t.Fatal(err)
	}

	called := make(chan error, 1)

	go func() {
		_, err := target.Query(ctx, "CALL DEBUG_TEST.RAISE_PAY(10)")
		called <- err
	}()

	assertPausedAt(t, d, set.Number, 4)

	if err := d.Resume(ctx, StepOver); err != nil {
		t.Fatal(err)
	}

	assertPausedAt(t, d, 0, 5)
	assertBreakpoints(t, d, set)

	if err := d.Resume(ctx, Continue); err != nil {
		t.Fatal(err)
	}

	if err := <-called; err != nil {
		t.Errorf("the CALL gave %v", err)
	}

	if err := d.Detach(ctx); err != nil {
		t.Error(err)
	}
}

// assertPausedAt waits for the target to pause at line, at the
// breakpoint of number or, of 0, a step, and checks its frames and the
// stack read again.
func assertPausedAt(t *testing.T, d *Debugger, number, line uint32) {
	t.Helper()

	event, err := d.Wait(context.Background(), debugWait)
	if err != nil || event == nil || event.Kind != EventPaused {
		t.Fatalf("Wait gave %+v, %v", event, err)
	}

	pause := event.Pause
	top := pause.Frames[len(pause.Frames)-1]

	if pause.Breakpoint != number || top.Line != line || !raisePayFrame(top) {
		t.Errorf("paused at %d, line %d: %+v, origin %+v", pause.Breakpoint, top.Line, top, top.Origin)
	}

	again, err := d.Stack(context.Background())
	if err != nil || again == nil || again.Frames[len(again.Frames)-1].Line != line {
		t.Errorf("Stack gave %+v, %v", again, err)
	}
}

// raisePayFrame says whether f is a visible frame of RAISE_PAY, of PCT 10
// and a variable N.
func raisePayFrame(f StackFrame) bool {
	return f.Visible && f.Origin != nil && f.Origin.Name.Name == "RAISE_PAY" && len(f.Parameters) == 1 &&
		f.Parameters[0].Name == "PCT" && valueText(f.Parameters[0].Value) == "10" && len(f.Variables) == 1 &&
		f.Variables[0].Name == "N"
}

// assertBreakpoints lists the breakpoints, set alone, clears it, and
// lists none.
func assertBreakpoints(t *testing.T, d *Debugger, set Breakpoint) {
	t.Helper()

	ctx := context.Background()

	all, err := d.Breakpoints(ctx)
	if err != nil || len(all) != 1 || all[0] != set || set.Line != 4 {
		t.Errorf("Breakpoints gave %+v, %v", all, err)
	}

	if err := d.Clear(ctx, set.Number); err != nil {
		t.Error(err)
	}

	if all, err := d.Breakpoints(ctx); err != nil || len(all) != 0 {
		t.Errorf("Breakpoints after Clear gave %+v, %v", all, err)
	}
}

// TestLiveDebuggerRefused refuses what a debugger may not do.
//
// Verifies: [PROTOCOL Debug 'V'] an ID of no connection the user may see
// is refused (SQLSTATE 08003); an action the state does not allow, as a
// Resume attached to none, TBD01 (SQLCODE -910); a breakpoint in a
// routine the user holds no DEBUG on, 42000 (SQLCODE -104), and in none,
// 42000 (SQLCODE -103); each keeps the
// session [CLIENT Debugging routines].
func TestLiveDebuggerRefused(t *testing.T) {
	t.Parallel()

	owner := liveSchema(t, "UNDEBUGGED")
	mustRun(t, owner, "CREATE PROCEDURE UNDEBUGGED.P() READS SQL DATA BEGIN END")
	mustRun(t, owner, "COMMIT")
	mustRun(t, owner, "CREATE USER DEBUG_BOB PASSWORD 'bob''s'")

	ctx := context.Background()
	d := owner.Debugger()

	assertState(t, d.Attach(ctx, 1<<62), "08003")
	assertFailure(t, d.Resume(ctx, Continue), -910, "TBD01", "")

	bob, other := liveAs(t, "DEBUG_BOB", "bob's"), liveAs(t, "DEBUG_BOB", "bob's")

	id, err := other.Debugger().ID(ctx)
	if err != nil || bob.Debugger().Attach(ctx, id) != nil {
		t.Fatalf("ID %d, %v", id, err)
	}

	_, err = bob.Debugger().Break(ctx, RoutineProcedure, QualifiedName{"UNDEBUGGED", "P"}, 1, "")
	assertFailure(t, err, -104, "42000", "")

	_, err = bob.Debugger().Break(ctx, RoutineProcedure, QualifiedName{"UNDEBUGGED", "NONE"}, 1, "")
	assertFailure(t, err, -103, "42000", "")

	if owner.IsLost() || bob.IsLost() {
		t.Error("a refusal lost the connection")
	}
}

// liveAs connects as the owner, then as user by password.
func liveAs(t *testing.T, user, password string) *Conn {
	t.Helper()

	c := liveConnect(t, liveTLS(t, 0, false), Trust{}, nil)
	if err := c.ChangeUser(context.Background(), user, NewScram(password)); err != nil {
		t.Fatal(err)
	}

	return c
}
