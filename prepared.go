package turborel

// Prepared statements [CLIENT Prepared statements] [PROTOCOL Prepare
// 'P']: prepared once under the client's number, their parameters' types
// and columns described, then executed with values as often as wanted;
// a query's cursor opened and its rows fetched a batch at a time. With
// cursors granted, a statement has several cursors open at once, each
// numbered by the program; with describe, a statement or an open cursor
// is described again [CLIENT Controlling the session].

import "context"

// Statement is a statement prepared on the connection that prepared it,
// valid across transactions until deallocated.
type Statement struct {
	number     uint32
	parameters []Parameter
	columns    []Column
}

// Parameters gives the type each `?` takes, in the order of the marks.
func (s *Statement) Parameters() []Parameter { return s.parameters }

// Columns gives a query's columns; none of another statement.
func (s *Statement) Columns() []Column { return s.columns }

// Batch is rows of a cursor, and whether it has more.
type Batch struct {
	Rows []Row
	More bool
}

// Prepare prepares sql, its dynamic parameters written `?`, under the
// connection's next number.
func (c *Conn) Prepare(ctx context.Context, sql string) (*Statement, error) {
	number := c.nextStatement
	c.nextStatement++

	defer c.during(ctx)()

	if err := c.send(prepareRequest{statement: number, sql: sql}); err != nil {
		return nil, withCause(err, ctx.Err())
	}

	st, err := c.prepared()

	return st, withCause(err, ctx.Err())
}

// prepared reads Prepared.
func (c *Conn) prepared() (*Statement, error) {
	rep, err := c.receive()
	if err != nil {
		return nil, err
	}

	switch r := rep.(type) {
	case preparedReply:
		return &Statement{number: r.statement, parameters: r.parameters, columns: r.columns}, nil
	case errorReply:
		return nil, r.failure
	default:
		return nil, c.lose("the server sent no Prepared")
	}
}

// Execute runs st with values for its parameters: a query's columns and
// rows, or another statement's completion; under autocommit its own
// transaction.
func (c *Conn) Execute(ctx context.Context, st *Statement, values []Value) (*Outcome, error) {
	defer c.during(ctx)()

	req := executeRequest{statement: st.number, values: values, ends: c.autocommit, attributes: c.requestAttributes()}
	if err := c.send(req); err != nil {
		return nil, withCause(err, ctx.Err())
	}

	if len(st.columns) == 0 {
		done, err := c.completion()

		return &Outcome{Completion: done}, withCause(err, ctx.Err())
	}

	rows, done, _, err := c.rows()
	if err != nil {
		return nil, withCause(err, ctx.Err())
	}

	return &Outcome{Columns: st.columns, Rows: rows, Completion: done}, nil
}

// OpenCursor runs the query st, opening its cursor 0, and gives at most
// rows of its rows, all if 0. Under autocommit, with fetch-ends granted,
// the transaction ends as the cursor does.
func (c *Conn) OpenCursor(ctx context.Context, st *Statement, values []Value, rows uint32) (*Batch, error) {
	return c.OpenCursorNumbered(ctx, st, 0, values, rows)
}

// OpenCursorNumbered opens st's cursor of the program's number, which
// needs cursors granted unless it is 0.
func (c *Conn) OpenCursorNumbered(
	ctx context.Context, st *Statement, cursor uint32, values []Value, rows uint32,
) (*Batch, error) {
	if err := c.needsCursor(cursor); err != nil {
		return nil, err
	}

	defer c.during(ctx)()

	ends := c.autocommit && c.granted[CapabilityFetchEnds]

	req := executeRequest{
		statement: st.number, values: values, rows: rows, ends: ends, attributes: c.requestAttributes(), cursor: cursor,
	}
	if err := c.send(req); err != nil {
		return nil, withCause(err, ctx.Err())
	}

	fetched, _, more, err := c.rows()
	if err != nil {
		return nil, withCause(err, ctx.Err())
	}

	return &Batch{Rows: fetched, More: more}, nil
}

// Fetch gives at most rows more rows of st's cursor 0, all if 0.
func (c *Conn) Fetch(ctx context.Context, st *Statement, rows uint32) (*Batch, error) {
	return c.FetchNumbered(ctx, st, 0, rows)
}

// FetchNumbered gives more rows of st's cursor of the program's number.
func (c *Conn) FetchNumbered(ctx context.Context, st *Statement, cursor, rows uint32) (*Batch, error) {
	if err := c.needsCursor(cursor); err != nil {
		return nil, err
	}

	defer c.during(ctx)()

	if err := c.send(fetchRequest{statement: st.number, rows: rows, cursor: cursor}); err != nil {
		return nil, withCause(err, ctx.Err())
	}

	fetched, _, more, err := c.rows()
	if err != nil {
		return nil, withCause(err, ctx.Err())
	}

	return &Batch{Rows: fetched, More: more}, nil
}

// CloseCursor closes st's cursor 0.
func (c *Conn) CloseCursor(ctx context.Context, st *Statement) error {
	return c.CloseCursorNumbered(ctx, st, 0)
}

// CloseCursorNumbered closes st's cursor of the program's number.
func (c *Conn) CloseCursorNumbered(ctx context.Context, st *Statement, cursor uint32) error {
	if err := c.needsCursor(cursor); err != nil {
		return err
	}

	return c.completed(ctx, closeRequest{statement: st.number, cursor: cursor})
}

// Describe describes st again, as Prepare did; it needs describe.
func (c *Conn) Describe(ctx context.Context, st *Statement) (*Statement, error) {
	if err := c.needs(CapabilityDescribe); err != nil {
		return nil, err
	}

	defer c.during(ctx)()

	if err := c.send(describeRequest{statement: st.number}); err != nil {
		return nil, withCause(err, ctx.Err())
	}

	described, err := c.prepared()

	return described, withCause(err, ctx.Err())
}

// DescribeCursor gives the columns of st's open cursor of the program's
// number; it needs describe.
func (c *Conn) DescribeCursor(ctx context.Context, st *Statement, cursor uint32) ([]Column, error) {
	if err := c.needs(CapabilityDescribe); err != nil {
		return nil, err
	}

	defer c.during(ctx)()

	if err := c.send(describeRequest{ofCursor: true, statement: st.number, cursor: cursor}); err != nil {
		return nil, withCause(err, ctx.Err())
	}

	// A cursor's Description comes alone, with no rows after it.
	c.answeredByOne = true
	rep, err := c.receive()
	c.answeredByOne = false

	if err != nil {
		return nil, withCause(err, ctx.Err())
	}

	switch r := rep.(type) {
	case descriptionReply:
		return r.columns, nil
	case errorReply:
		return nil, r.failure
	default:
		return nil, c.lose("the server sent no Description")
	}
}

// Deallocate destroys st; it is of no use after.
func (c *Conn) Deallocate(ctx context.Context, st *Statement) error {
	return c.completed(ctx, deallocateRequest{statement: st.number})
}

// needsCursor fails a cursor other than 0 without cursors granted.
func (c *Conn) needsCursor(cursor uint32) error {
	if cursor == 0 {
		return nil
	}

	return c.needs(CapabilityCursors)
}
