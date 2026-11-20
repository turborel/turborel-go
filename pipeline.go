package turborel

// Pipelining [CLIENT Pipelines] [PROTOCOL Requests]: a Pipeline queues
// requests, writes them at once, and reads one outcome for each, in
// order, as the server answers each request alone and in turn. A batch
// costs one round trip, not one a request.
//
// The requests are written a window at a time, each window at most
// PipelineWindow bytes, and its replies read before the next is written.
// A window fits the buffers of the two sockets between client and
// server, so it is written whole even while the server writes the
// replies of its first requests and reads no more.
//
// Each request is independent, so by default a failure is that request's
// outcome alone and the rest run. OnErrorSkip makes the batch stop at its
// first failure, the requests before it kept: a savepoint is set before
// each request but the first, within the transaction, and the requests
// after a failed one are undone to the savepoint set after it, or never
// sent if their window was not yet written. Its requests are of one
// transaction, which under autocommit, of a batch that began none, is
// committed as the batch ends.

import (
	"context"
	"slices"
	"strconv"
)

// PipelineWindow is the most bytes of requests written before their
// replies are read: well within the smallest socket buffer a platform
// gives.
const PipelineWindow = 64 * 1024

// pipelineSavepoint names the savepoints OnErrorSkip sets, then each its
// place.
const pipelineSavepoint = "TURBOREL_PIPELINE_"

// OnError is what a pipeline does after a request fails.
type OnError uint8

// What a pipeline does after a failure.
const (
	// OnErrorContinue runs every request; a failure is its request's
	// outcome alone.
	OnErrorContinue OnError = iota
	// OnErrorSkip stops the batch at its first failure: the requests
	// after it are undone or never sent, each skipped.
	OnErrorSkip
)

// Piped is the outcome of one request of a pipeline: what it gave; how
// it failed, as the server reported it; or, if Skipped, not run, or run
// and undone, after an earlier request failed under OnErrorSkip.
type Piped struct {
	Outcome *Outcome
	Failure *Error
	Skipped bool
}

// queuedKind is what a queued request is.
type queuedKind uint8

const (
	queuedQuery queuedKind = iota
	queuedExecute
	queuedClose
	queuedDeallocate
	queuedSavepoint
)

// queued is a request queued, and how its reply is read.
type queued struct {
	kind      queuedKind
	sql       string
	statement *Statement
	values    []Value
	number    uint32
	place     int
}

// Pipeline is requests queued on a connection, written at once by Run.
type Pipeline struct {
	conn    *Conn
	queued  []queued
	onError OnError
}

// Pipeline gives an empty pipeline of requests on the connection, of
// OnErrorContinue.
func (c *Conn) Pipeline() *Pipeline { return &Pipeline{conn: c} }

// Query queues sql, run as Conn.Query runs it.
func (p *Pipeline) Query(sql string) *Pipeline {
	p.queued = append(p.queued, queued{kind: queuedQuery, sql: sql})

	return p
}

// Execute queues an execution of st with values, as Conn.Execute runs
// it: all of a query's rows.
func (p *Pipeline) Execute(st *Statement, values []Value) *Pipeline {
	p.queued = append(p.queued, queued{kind: queuedExecute, statement: st, values: values})

	return p
}

// OnError sets what the batch does after a request fails.
func (p *Pipeline) OnError(onError OnError) *Pipeline {
	p.onError = onError

	return p
}

// Len gives how many requests are queued.
func (p *Pipeline) Len() int { return len(p.queued) }

// Run writes the requests, a window at a time, and reads one outcome for
// each, in the order they were queued. It fails once the connection is
// lost, what the requests answered before it not given; and under
// OnErrorSkip with the failure of undoing the requests after a failed
// one.
func (p *Pipeline) Run(ctx context.Context) ([]Piped, error) {
	c := p.conn
	skip := p.onError == OnErrorSkip
	began := c.InTransaction()

	sent := p.queued
	if skip {
		sent = guarded(sent)
	}

	defer c.during(ctx)()

	c.leaveCopy(queryRequest{})

	outcomes, err := c.runWindows(windows(sent, c.autocommit && !skip, c.requestAttributes()), skip)
	if err != nil {
		return nil, withCause(err, ctx.Err())
	}

	if skip {
		if err := c.settle(sent, outcomes, began); err != nil {
			return nil, withCause(err, ctx.Err())
		}
	}

	shown := make([]Piped, 0, len(p.queued))

	for i, q := range sent {
		if q.kind != queuedSavepoint {
			shown = append(shown, outcomes[i])
		}
	}

	return shown, nil
}

// release queues the closing of statement number's cursor, then its
// destruction: a pool's reset of what a borrower left.
func (p *Pipeline) release(number uint32) *Pipeline {
	p.queued = append(p.queued, queued{kind: queuedClose, number: number}, queued{kind: queuedDeallocate, number: number})

	return p
}

// runWindows writes each window and reads its outcomes; skipping, a
// window after a failure is not sent, each of its requests skipped.
func (c *Conn) runWindows(all [][]framedRequest, skip bool) ([]Piped, error) {
	var outcomes []Piped

	for _, window := range all {
		if skip && anyFailed(outcomes) {
			for range window {
				outcomes = append(outcomes, Piped{Skipped: true})
			}

			continue
		}

		read, err := c.readWindow(window)
		if err != nil {
			return nil, err
		}

		outcomes = append(outcomes, read...)
	}

	return outcomes, nil
}

// anyFailed says whether one of outcomes failed.
func anyFailed(outcomes []Piped) bool {
	for _, o := range outcomes {
		if o.Failure != nil {
			return true
		}
	}

	return false
}

// savepointName is the savepoint OnErrorSkip sets before the request of
// place n.
func savepointName(n int) string { return pipelineSavepoint + strconv.Itoa(n) }

// guarded gives requests, a savepoint of its own before each but the
// first.
func guarded(requests []queued) []queued {
	sent := make([]queued, 0, len(requests)+len(requests))

	for n, q := range requests {
		if n > 0 {
			sent = append(sent, queued{kind: queuedSavepoint, place: n})
		}

		sent = append(sent, q)
	}

	return sent
}

// framedRequest is a request and its frame.
type framedRequest struct {
	queued queued
	frame  []byte
}

// windows groups requests into windows of at most PipelineWindow bytes, a
// request longer than that alone in its own; each statement of the
// program's ending its transaction if ends, carrying attributes.
func windows(requests []queued, ends bool, attributes []Attribute) [][]framedRequest {
	var all [][]framedRequest

	filled := PipelineWindow

	for _, q := range requests {
		frame := encodeRequest(q.request(ends, attributes))

		filled += len(frame)
		if filled > PipelineWindow {
			all = append(all, nil)
			filled = len(frame)
		}

		all[len(all)-1] = append(all[len(all)-1], framedRequest{queued: q, frame: frame})
	}

	return all
}

// request gives the request q sends.
func (q queued) request(ends bool, attributes []Attribute) request {
	switch q.kind {
	case queuedExecute:
		return executeRequest{statement: q.statement.number, values: q.values, ends: ends, attributes: attributes}
	case queuedClose:
		return closeRequest{statement: q.number}
	case queuedDeallocate:
		return deallocateRequest{statement: q.number}
	case queuedSavepoint:
		return queryRequest{sql: "SAVEPOINT " + savepointName(q.place)}
	case queuedQuery:
	}

	return queryRequest{sql: q.sql, ends: ends, attributes: attributes}
}

// readWindow writes window at once and reads its outcomes.
func (c *Conn) readWindow(window []framedRequest) ([]Piped, error) {
	var frames []byte
	for _, f := range window {
		frames = append(frames, f.frame...)
	}

	if err := c.sendEncoded(frames); err != nil {
		return nil, err
	}

	outcomes := make([]Piped, 0, len(window))

	for _, f := range window {
		outcome, err := c.replyOf(f.queued)
		if err != nil {
			if c.lost != nil {
				return nil, err
			}

			outcomes = append(outcomes, Piped{Failure: asError(err)})

			continue
		}

		outcomes = append(outcomes, Piped{Outcome: outcome})
	}

	return outcomes, nil
}

// replyOf reads the reply of the request q.
func (c *Conn) replyOf(q queued) (*Outcome, error) {
	switch q.kind {
	case queuedExecute:
		if len(q.statement.columns) == 0 {
			done, err := c.completion()
			if err != nil {
				return nil, err
			}

			return &Outcome{Completion: done}, nil
		}

		rows, done, _, err := c.rows()
		if err != nil {
			return nil, err
		}

		return &Outcome{Columns: q.statement.columns, Rows: rows, Completion: done}, nil
	case queuedClose, queuedDeallocate:
		done, err := c.completion()
		if err != nil {
			return nil, err
		}

		return &Outcome{Completion: done}, nil
	case queuedQuery, queuedSavepoint:
	}

	outcome, _, err := c.outcome()

	return outcome, err
}

// settle ends a batch of sent run under OnErrorSkip: the requests after
// the first that failed undone, to the savepoint set after it, and each
// marked skipped; the savepoints released; and, under autocommit of a
// batch that began no transaction, its work committed.
func (c *Conn) settle(sent []queued, outcomes []Piped, began bool) error {
	var statements []string

	failed := slices.IndexFunc(outcomes, func(o Piped) bool { return o.Failure != nil })
	if failed >= 0 {
		if after := failed + 1; after < len(sent) && sent[after].kind == queuedSavepoint && outcomes[after].Outcome != nil {
			statements = append(statements, "ROLLBACK TO SAVEPOINT "+savepointName(sent[after].place))
		}

		for i := failed + 1; i < len(outcomes); i++ {
			outcomes[i] = Piped{Skipped: true}
		}
	}

	if len(sent) > 1 && sent[1].kind == queuedSavepoint && outcomes[1].Outcome != nil {
		statements = append(statements, "RELEASE SAVEPOINT "+savepointName(sent[1].place))
	}

	if !began && c.autocommit {
		statements = append(statements, commitStatement)
	}

	for _, sql := range statements {
		if err := c.within(sql); err != nil {
			return err
		}
	}

	return nil
}

// within runs sql within the transaction, whatever autocommit says: a
// statement of the batch's own, which must not end it.
func (c *Conn) within(sql string) error {
	if err := c.send(queryRequest{sql: sql}); err != nil {
		return err
	}

	_, _, err := c.outcome()

	return err
}
