package turborel

// Running statements [CLIENT Running statements] [PROTOCOL Query 'Q']: a
// direct statement's rows or completion, read whole or a batch at a time
// into a sink; the session's transaction, begun by the first statement
// and lasting until COMMIT or ROLLBACK, or each statement its own under
// autocommit, by the request's `ends`. A context done while a statement
// runs cancels it on the server, by the protocol's Cancel (RULES.md D2).

import (
	"context"
	"slices"
	"strings"
)

// The SQLSTATE class of a warning a completion may carry.
const classWarning = "01"

// Outcome is what a statement gave: a query's columns and rows, or for
// another statement no columns; and its completion either way.
type Outcome struct {
	Columns    []Column
	Rows       []Row
	Completion Completion
}

// IsQuery says whether the statement was a query, whose rows Rows holds.
func (o *Outcome) IsQuery() bool { return o.Columns != nil }

// Sink takes a query's rows as they come, a batch at a time.
type Sink interface {
	// Columns is given the columns of the rows that follow.
	Columns(columns []Column)
	// Rows is given a batch of rows.
	Rows(rows []Row)
}

// Streamed is what a statement run into a sink gave: how many rows went
// to the sink, of a query, and its completion.
type Streamed struct {
	Rows       uint64
	Completion Completion
}

// SetAutocommit makes each statement Query, QueryInto and Execute run its
// own transaction from now on, if on: the server commits it as the
// statement succeeds, or rolls it back as it fails, within the request,
// so no COMMIT is sent. Off as a connection opens.
func (c *Conn) SetAutocommit(on bool) { c.autocommit = on }

// Autocommit says whether each statement is its own transaction.
func (c *Conn) Autocommit() bool { return c.autocommit }

// Warning gives the SQLSTATE of the warning, class 01, the last
// statement completed with; false if it completed with none.
func (c *Conn) Warning() (string, bool) {
	if c.last == nil || !strings.HasPrefix(c.last.SQLState, classWarning) {
		return "", false
	}

	return c.last.SQLState, true
}

// Query runs sql, a direct SQL statement: a query's columns and rows, or
// another statement's completion; under autocommit, its own transaction.
func (c *Conn) Query(ctx context.Context, sql string) (*Outcome, error) {
	defer c.during(ctx)()

	if err := c.send(queryRequest{sql: sql, ends: c.autocommit, attributes: c.requestAttributes()}); err != nil {
		return nil, withCause(err, ctx.Err())
	}

	outcome, _, err := c.outcome()

	return outcome, withCause(err, ctx.Err())
}

// QueryInto runs sql as Query does, a query's columns and rows given to
// sink as they come, so no more than a batch is held.
func (c *Conn) QueryInto(ctx context.Context, sql string, sink Sink) (*Streamed, error) {
	defer c.during(ctx)()

	if err := c.send(queryRequest{sql: sql, ends: c.autocommit, attributes: c.requestAttributes()}); err != nil {
		return nil, withCause(err, ctx.Err())
	}

	streamed, err := c.streamInto(sink)

	return streamed, withCause(err, ctx.Err())
}

// streamInto reads a statement's reply into sink.
func (c *Conn) streamInto(sink Sink) (*Streamed, error) {
	rep, err := c.receive()
	if err != nil {
		return nil, err
	}

	switch r := rep.(type) {
	case completeReply:
		return &Streamed{Completion: r.completion}, nil
	case descriptionReply:
		sink.Columns(r.columns)
	case errorReply:
		return nil, r.failure
	default:
		return nil, c.lose("the server sent no result")
	}

	streamed := &Streamed{}

	for {
		rep, err := c.receive()
		if err != nil {
			return nil, err
		}

		switch r := rep.(type) {
		case rowsReply:
			streamed.Rows += uint64(len(r.rows))
			sink.Rows(r.rows)
		case completeReply:
			streamed.Completion = r.completion

			return streamed, nil
		case errorReply:
			return nil, r.failure
		default:
			return nil, c.lose("the server broke off its rows")
		}
	}
}

// outcome reads a statement's reply: its outcome, and whether another
// statement's replies follow, of a Query of several.
func (c *Conn) outcome() (*Outcome, bool, error) {
	rep, err := c.receive()
	if err != nil {
		return nil, false, err
	}

	switch r := rep.(type) {
	case completeReply:
		return &Outcome{Completion: r.completion}, r.completion.More, nil
	case descriptionReply:
		rows, done, _, err := c.rows()
		if err != nil {
			return nil, false, err
		}

		return &Outcome{Columns: r.columns, Rows: rows, Completion: done}, done.More, nil
	case errorReply:
		return nil, false, r.failure
	default:
		return nil, false, c.lose("the server sent no result")
	}
}

// rows reads Rows until the server says whether the cursor has more:
// the rows, the completion that ended them, and whether it has more.
func (c *Conn) rows() ([]Row, Completion, bool, error) {
	rows := []Row{}

	for {
		rep, err := c.receive()
		if err != nil {
			return nil, Completion{}, false, err
		}

		switch r := rep.(type) {
		case rowsReply:
			rows = append(rows, r.rows...)
		case completeReply:
			return rows, r.completion, false, nil
		case suspendedReply:
			return rows, Completion{}, true, nil
		case errorReply:
			return nil, Completion{}, false, r.failure
		default:
			return nil, Completion{}, false, c.lose("the server broke off its rows")
		}
	}
}

// completion reads a request's Complete.
func (c *Conn) completion() (Completion, error) {
	rep, err := c.receive()
	if err != nil {
		return Completion{}, err
	}

	switch r := rep.(type) {
	case completeReply:
		return r.completion, nil
	case errorReply:
		return Completion{}, r.failure
	default:
		return Completion{}, c.lose("the server sent no completion")
	}
}

// completed sends req and reads its Complete, cancelled on the server as
// ctx is done.
func (c *Conn) completed(ctx context.Context, req request) error {
	defer c.during(ctx)()

	if err := c.send(req); err != nil {
		return withCause(err, ctx.Err())
	}

	_, err := c.completion()

	return withCause(err, ctx.Err())
}

// during cancels the statement under way on the server once ctx is done,
// by Cancel over a connection of its own; of a connection opened over a
// program's stream, which cannot reach its server again, it gives the
// connection's waits up instead. The function it gives stops the watch.
func (c *Conn) during(ctx context.Context) func() {
	if ctx.Done() == nil {
		return func() {}
	}

	canceller, ok := c.Canceller()
	if !ok {
		return c.watch(ctx)
	}

	stop := context.AfterFunc(ctx, func() {
		// Sent once ctx is done, so on a context of ctx's values that is
		// not; the statement's failure tells whether it was cancelled.
		_ = canceller.CancelWithin(context.WithoutCancel(ctx), cancelWithin)
	})

	return func() { stop() }
}

// requestAttributes gives the attributes every statement carries, with
// query-attributes granted; none otherwise.
func (c *Conn) requestAttributes() []Attribute {
	if !c.granted[CapabilityQueryAttributes] {
		return nil
	}

	return c.attributes
}

// SetQueryAttributes sets attributes every later statement carries, read
// in SQL by QUERY_ATTRIBUTE, until set again; it needs query-attributes
// granted unless attributes is empty, and attributes within the bounds a
// server takes.
func (c *Conn) SetQueryAttributes(attributes []Attribute) error {
	if len(attributes) > 0 {
		if err := c.needs(CapabilityQueryAttributes); err != nil {
			return err
		}
	}

	if err := checkAttributes(attributes); err != nil {
		return err
	}

	c.attributes = slices.Clone(attributes)

	return nil
}
