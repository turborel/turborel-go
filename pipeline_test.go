package turborel

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"
)

// TestPipelineWindows writes requests a window at a time.
//
// Verifies: [CLIENT Pipelines] requests are grouped into windows of at
// most 64 KiB, in order, a request longer than a window alone in one,
// every request once; none queued, no window.
func TestPipelineWindows(t *testing.T) {
	t.Parallel()

	requests := []queued{
		{kind: queuedQuery, sql: "a"}, {kind: queuedQuery, sql: "VALUES 1"},
		{kind: queuedQuery, sql: strings.Repeat("x", PipelineWindow/2)},
		{kind: queuedQuery, sql: strings.Repeat("y", PipelineWindow)}, {kind: queuedQuery, sql: "z"},
	}

	all := windows(requests, false, nil)

	sizes := make([]int, len(all))
	total := 0

	for i, w := range all {
		sizes[i] = len(w)
		for _, f := range w {
			total += len(f.frame)
		}
	}

	whole := 0
	for _, q := range requests {
		whole += len(encodeRequest(q.request(false, nil)))
	}

	if !slices.Equal(sizes, []int{3, 1, 1}) || total != whole || len(windows(nil, false, nil)) != 0 {
		t.Errorf("windows %v, %d of %d bytes", sizes, total, whole)
	}
}

// replyQuery serves a Query, replying a Complete, or an Error if refused.
func replyQuery(refused bool) func(f *fake) {
	return func(f *fake) {
		f.expect(kindQuery)

		if refused {
			f.refuse("23000", true, "")
		} else {
			f.done(true)
		}
	}
}

// TestPipelineContinues runs every request, a failure its own.
//
// Verifies: [CLIENT Pipelines] queued queries and executions run as one
// batch, one outcome each, in order; a failure is its request's outcome
// alone, the requests after it run [PROTOCOL Requests].
func TestPipelineContinues(t *testing.T) {
	t.Parallel()

	c := openGranted(t, nil, func(f *fake) {
		f.sqlOf()
		f.preparedOf(nil, []Column{{Named: true, Name: "N", Type: ExactType{Precision: 9}}})

		for _, step := range []func(*fake){replyQuery(false), replyQuery(true)} {
			step(f)
		}

		f.expect(kindExecute)
		f.reply(kindRows, func(w *writer) { w.u32(1); encodeValues(w, []Value{DecimalOf(7, 0)}) })
		f.done(true)
	})

	st, err := c.Prepare(context.Background(), "VALUES ?")
	if err != nil {
		t.Fatal(err)
	}

	p := c.Pipeline().Query("INSERT 1").Query("INSERT 2").Execute(st, []Value{DecimalOf(7, 0)})
	if p.Len() != 3 {
		t.Fatalf("%d queued", p.Len())
	}

	out, err := p.Run(context.Background())
	if err != nil || len(out) != 3 || out[0].Outcome == nil || out[1].Failure == nil || len(out[2].Outcome.Rows) != 1 {
		t.Errorf("the batch gave %+v, %v", out, err)
	}
}

// TestPipelineSkips stops a batch at its first failure.
//
// Verifies: [CLIENT Pipelines] under skip on error a savepoint is set
// before each request but the first; after a failure the requests run
// are rolled back to the savepoint after it and skipped, the first
// savepoint released, and, under autocommit of a batch that began no
// transaction, the batch committed; a window after the failure's is
// never sent.
func TestPipelineSkips(t *testing.T) {
	t.Parallel()

	var statements []string

	c := openGranted(t, nil, func(f *fake) {
		for _, refused := range []bool{false, false, true, false, false, false} {
			statements = append(statements, newReader(f.expect(kindQuery), "request").str())

			if refused {
				f.refuse("23000", true, "")
			} else {
				f.done(true)
			}
		}

		for range 3 {
			statements = append(statements, newReader(f.expect(kindQuery), "request").str())
			f.done(true)
		}
	})

	c.SetAutocommit(true)

	p := c.Pipeline().OnError(OnErrorSkip).Query("A").Query("B").Query("C").Query(strings.Repeat("D", PipelineWindow))

	out, err := p.Run(context.Background())
	if err != nil || len(out) != 4 || out[0].Outcome == nil || out[1].Failure == nil || !out[2].Skipped ||
		!out[3].Skipped {
		t.Errorf("the batch gave %+v, %v", out, err)
	}

	want := []string{
		"A", "SAVEPOINT TURBOREL_PIPELINE_1", "B", "SAVEPOINT TURBOREL_PIPELINE_2", "C", "SAVEPOINT TURBOREL_PIPELINE_3",
		"ROLLBACK TO SAVEPOINT TURBOREL_PIPELINE_2", "RELEASE SAVEPOINT TURBOREL_PIPELINE_1", "COMMIT",
	}
	if !slices.Equal(statements, want) {
		t.Errorf("sent %v", statements)
	}
}

// TestPipelineLost fails a batch whose connection is lost.
//
// Verifies: [CLIENT Pipelines] run fails once the connection is lost
// (SQLSTATE 08006), what the requests answered before it not given; a
// batch run during a copy abandons the copy first.
func TestPipelineLost(t *testing.T) {
	t.Parallel()

	c := openGranted(t, []string{CapabilityCopy}, func(f *fake) {
		f.copyIn()

		if !f.copyEnd() {
			t.Error("the copy was not abandoned")
		}

		f.counted(0)
		f.expect(kindQuery)
		f.done(true)
		f.expect(kindQuery)
		f.reply(kindRows, nil)
	})

	if _, err := c.CopyIn(context.Background(), "T", nil); err != nil {
		t.Fatal(err)
	}

	_, err := c.Pipeline().Query("A").Query("B").Run(context.Background())
	assertState(t, err, sqlstateConnectionFailure)
}

// TestPipelineReleases closes and destroys statements, and executes a
// statement of no rows.
//
// Verifies: [CLIENT Pipelines] an execution of a statement of no columns
// is answered by its completion; a pool's release of a statement closes
// its cursor and destroys it, each answered by a Complete.
func TestPipelineReleases(t *testing.T) {
	t.Parallel()

	c := openGranted(t, nil, func(f *fake) {
		f.sqlOf()
		f.preparedOf(nil, nil)

		for _, kind := range []byte{kindExecute, kindClose, kindDeallocate} {
			f.expect(kind)
			f.done(false)
		}
	})

	st, err := c.Prepare(context.Background(), "INSERT")
	if err != nil {
		t.Fatal(err)
	}

	out, err := c.Pipeline().Execute(st, nil).release(st.number).Run(context.Background())
	if err != nil || len(out) != 3 || out[0].Outcome.IsQuery() || out[2].Outcome == nil {
		t.Errorf("the batch gave %+v, %v", out, err)
	}

	whole := c.NewLOBReader(context.Background(), LOB{Kind: BinaryLOB, Octets: []byte{1, 2}})
	if b, err := io.ReadAll(whole); err != nil || len(b) != 2 {
		t.Errorf("a value held whole read %v, %v", b, err)
	}
}
