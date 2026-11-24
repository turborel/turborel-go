package turborel

// Import [CLIENT Importing]: rows read from a Source and inserted into a
// table over a connection, through one prepared INSERT whose dynamic
// parameters take the table's columns' types, its columns the source's
// headings or else the table's; a batch of rows to each transaction. Each
// value is cast by the server to its column's type, but a string for a
// character string column, which is assigned to it as INSERT assigns, so
// one too long is refused, not cut. The rows go by bulk copy, with no
// round trip a row, where the server grants it, no bad row is to be
// skipped and no column is a large object's. A source yields a record at
// a time, so memory is bounded by a record whatever the source's size.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// DefaultBatch is the rows an import commits at a time unless told
// otherwise.
const DefaultBatch = 1000

// Position is where in a source a record began: its number among the
// source's records of data, the first 1, a header record 0; and the line
// it began on, of a source made of lines, if HasLine.
type Position struct {
	Record  uint64
	Line    uint64
	HasLine bool
}

// String gives the position as "record 7, line 9".
func (p Position) String() string {
	text := "record " + strconv.FormatUint(p.Record, decimalRadix)
	if p.HasLine {
		text += ", line " + strconv.FormatUint(p.Line, decimalRadix)
	}

	return text
}

// Heading is what a source says of one of its fields: a name, read as an
// identifier if it is one, a regular one in upper case, else taken as it
// is; and the type of its values, nil if the source does not know it.
type Heading struct {
	Name string
	Type DataType
}

// Record is a record of a source: a value for each of its fields, each
// cast to its column's type as the INSERT takes it, and where it began.
type Record struct {
	Values   []Value
	Position Position
}

// SourceError is why a source cannot give its next record: where, and
// what is wrong, in plain words.
type SourceError struct {
	Position Position
	Message  string
}

func (e *SourceError) Error() string { return e.Position.String() + ": " + e.Message }

// Unwrap gives the failure as the driver's: SQLCODE -301, SQLSTATE 22000.
func (e *SourceError) Unwrap() error {
	return &Error{SQLCode: sqlcodeData, SQLState: sqlstateDataException, Message: e.Message}
}

// Source is where rows to import come from. Headings is asked once,
// before the first record; then Record until it gives none. A source
// holds no more than a record at a time, however long it is. A failure
// of either is a [*SourceError], after which the source cannot go on.
type Source interface {
	// Headings gives the source's fields' names and types, as of a header
	// record, in order; nil if it does not know them.
	Headings() ([]Heading, error)
	// Record gives the next record; nil at the end.
	Record() (*Record, error)
}

// Import says how to import: the table, as SQL names it; the rows
// committed at a time, 0 for all at once; and whether a row that fails is
// skipped, given to the program, and the import goes on.
type Import struct {
	Table string
	Batch uint64
	Skip  bool
}

// ImportInto is an import into table, DefaultBatch rows at a time, a row
// that fails ending it.
func ImportInto(table string) Import {
	return Import{Table: table, Batch: DefaultBatch}
}

// Imported is what an import did: the rows inserted and committed, and
// the rows skipped.
type Imported struct {
	Rows     uint64
	Rejected uint64
}

// Cause is why a row failed: the source's input, Input, or SQL's, SQL,
// its position in the INSERT dropped. One of the two is set.
type Cause struct {
	Input string
	SQL   *Error
}

// String gives the cause in plain words.
func (c Cause) String() string {
	if c.SQL != nil {
		return c.SQL.Error()
	}

	return c.Input
}

// sqlCause is the cause of a failure SQL gave, without its position.
func sqlCause(err error) Cause {
	var failure *Error
	if !errors.As(err, &failure) {
		return Cause{Input: err.Error()}
	}

	given := *failure
	given.Position, given.Placed = 0, false

	return Cause{SQL: &given}
}

// Rejected is a row skipped: where its record began, and why.
type Rejected struct {
	Position Position
	Cause    Cause
}

// String gives the row as "record 7, line 9: why; skipped".
func (r Rejected) String() string {
	return r.Position.String() + ": " + r.Cause.String() + "; skipped"
}

// ImportError is an import that stopped: where the record that stopped it
// began, if Placed; why; the rows committed before it, and those of its
// batch rolled back.
type ImportError struct {
	Position   Position
	Placed     bool
	Cause      Cause
	Committed  uint64
	RolledBack uint64
}

func (e *ImportError) Error() string {
	text := ""
	if e.Placed {
		text = e.Position.String() + ": "
	}

	return fmt.Sprintf("%s%s; %s committed, %d rolled back", text, e.Cause, rowsText(e.Committed), e.RolledBack)
}

// Unwrap gives the driver's failure the import stopped for: SQL's, or of
// the source's input, SQLCODE -301 and SQLSTATE 22000.
func (e *ImportError) Unwrap() error {
	if e.Cause.SQL != nil {
		return e.Cause.SQL
	}

	return &Error{SQLCode: sqlcodeData, SQLState: sqlstateDataException, Message: e.Cause.Input}
}

// before is an import that stopped before its first row, for cause.
func before(cause Cause) *ImportError { return &ImportError{Cause: cause} }

// place places the failure at p.
func (e *ImportError) place(p Position) {
	e.Position, e.Placed = p, true
}

// rowsText gives n rows in words: "1 row", "2 rows".
func rowsText(n uint64) string {
	if n == 1 {
		return "1 row"
	}

	return strconv.FormatUint(n, decimalRadix) + " rows"
}

// The statements an import ends a batch with.
const (
	commitStatement   = "COMMIT"
	rollbackStatement = "ROLLBACK"
)

// The SQLCODE of a statement cancelled.
const sqlcodeCancelled int32 = -920

// Import inserts the rows of source into how.Table: the rows inserted and
// those skipped. Each skipped row is given to rejected, if how.Skip. The
// session must have no transaction active, as the import commits its
// batches. A row that fails ends the import with an [*ImportError],
// unless skipped.
func (c *Conn) Import(ctx context.Context, source Source, how Import, rejected func(Rejected)) (Imported, error) {
	insert, columns, err := c.importPrepared(ctx, source, how)
	if err != nil {
		return Imported{}, err
	}

	if c.copies(insert, how) {
		table, _ := tableName(how.Table)
		// Its parameters' types known, the INSERT is of no more use.
		if err := c.Deallocate(ctx, insert); err != nil {
			return Imported{}, before(sqlCause(err))
		}

		run := copyRun{how: how, parameters: insert.inputs}

		return run.run(ctx, c, source, table, columns)
	}

	run := insertRun{conn: c, ctx: ctx, insert: insert, how: how, rejected: rejected}
	done, failure := run.rows(source)

	if c.IsLost() {
		if failure != nil {
			return done, failure
		}

		return done, nil
	}

	// Let go of even if the import stopped, so the server holds nothing.
	freed := c.Deallocate(ctx, insert)
	if failure != nil {
		return done, failure
	}

	if freed != nil {
		return done, &ImportError{Cause: sqlCause(freed), Committed: done.Rows}
	}

	return done, nil
}

// importPrepared prepares the INSERT of the import: of the source's
// headings' columns, or else the table's; and gives the columns'
// delimited names.
func (c *Conn) importPrepared(ctx context.Context, source Source, how Import) (*Statement, []string, error) {
	if c.InTransaction() {
		message := "[DESIGN] an import begins with no transaction active, as it commits its batches"

		return nil, nil, before(Cause{Input: message})
	}

	table, ok := tableName(how.Table)
	if !ok {
		return nil, nil, before(Cause{Input: "[ISO/IEC 9075:1992 §5.4] " + how.Table + " is not a <table name>"})
	}

	headings, err := source.Headings()
	if err != nil {
		return nil, nil, sourceFailed(err, &ImportError{})
	}

	var columns []string

	if headings != nil {
		for _, h := range headings {
			columns = append(columns, columnName(h.Name))
		}
	} else if columns, err = c.described(ctx, table); err != nil {
		return nil, nil, err
	}

	names := make([]string, len(columns))
	for i, column := range columns {
		names[i] = delimited(column)
	}

	marks := strings.TrimSuffix(strings.Repeat("?, ", len(names)), ", ")
	sql := "INSERT INTO " + table + " (" + strings.Join(names, ", ") + ") VALUES (" + marks + ")"

	insert, err := c.Prepare(ctx, sql)
	if err != nil {
		return nil, nil, before(sqlCause(err))
	}

	return insert, names, nil
}

// sourceFailed is the import's failure for a source's err, where the
// record began.
func sourceFailed(err error, stopped *ImportError) *ImportError {
	if failure, ok := errors.AsType[*SourceError](err); ok {
		stopped.Cause = Cause{Input: failure.Message}
		stopped.place(failure.Position)

		return stopped
	}

	stopped.Cause = Cause{Input: err.Error()}

	return stopped
}

// described gives the names of table's columns, as a prepared query's
// description gives them.
func (c *Conn) described(ctx context.Context, table string) ([]string, error) {
	query, err := c.Prepare(ctx, "SELECT * FROM "+table) //nolint:unqueryvet // Every column is what is described.
	if err != nil {
		return nil, before(sqlCause(err))
	}

	names := make([]string, len(query.columns))
	for i, column := range query.columns {
		names[i] = column.Name
	}

	if err := c.Deallocate(ctx, query); err != nil {
		return nil, before(sqlCause(err))
	}

	return names, nil
}

// insertRun is an import by INSERT under way.
type insertRun struct {
	conn      *Conn
	ctx       context.Context //nolint:containedctx // The run's calls are each cancelled as the import's context is done.
	insert    *Statement
	how       Import
	rejected  func(Rejected)
	committed uint64
	inBatch   uint64
	skipped   uint64
}

// rows inserts each record of source, committing a batch at a time.
func (r *insertRun) rows(source Source) (Imported, *ImportError) {
	for {
		record, err := source.Record()
		if err != nil {
			return Imported{}, r.stop(nil, Cause{}, err)
		}

		if record == nil {
			break
		}

		if failure := r.row(record); failure != nil {
			return Imported{}, failure
		}
	}

	if failure := r.commit(); failure != nil {
		return Imported{}, failure
	}

	return Imported{Rows: r.committed, Rejected: r.skipped}, nil
}

// row inserts record's values, committing the batch once it is whole; a
// row that fails is skipped if it alone was undone and skipping is
// asked.
func (r *insertRun) row(record *Record) *ImportError {
	wanted := len(r.insert.inputs)
	if len(record.Values) != wanted {
		message := fmt.Sprintf("[DESIGN] %d fields, for %d columns", len(record.Values), wanted)

		return r.reject(Rejected{Position: record.Position, Cause: Cause{Input: message}})
	}

	err := r.execute(record.Values)
	if err == nil {
		r.inBatch++
		if r.inBatch == r.how.Batch {
			return r.commit()
		}

		return nil
	}

	// A row is skipped only if it alone was undone: the batch's
	// transaction goes on, and the connection.
	alone := r.inBatch == 0 || r.conn.InTransaction()

	var failure *Error
	if !alone || !errors.As(err, &failure) || endsConnection(failure) || failure.SQLCode == sqlcodeCancelled {
		return r.stop(&record.Position, sqlCause(err), nil)
	}

	return r.reject(Rejected{Position: record.Position, Cause: sqlCause(err)})
}

// execute runs the INSERT with values, each string for a character string
// column assigned to it, each long one for a large object's sent by a
// locator.
func (r *insertRun) execute(values []Value) error {
	given, failure := assigned(values, r.insert.inputs)
	if failure != nil {
		return failure
	}

	located, err := r.located(given)
	if err != nil {
		return err
	}

	_, err = r.conn.Execute(r.ctx, r.insert, located)

	return err
}

// located gives values, each longer than LOBInlineOctets for a large
// object's parameter sent ahead by a locator, a piece at a time.
func (r *insertRun) located(values []Value) ([]Value, error) {
	for i, v := range values {
		kind, isLOB := lobParameter(r.insert.inputs[i].Type)
		if !isLOB {
			continue
		}

		var octets []byte

		switch v := v.(type) {
		case Char:
			octets = []byte(v)
		case Binary:
			octets = v
		case LOB:
			octets = v.Octets
		}

		if len(octets) <= LOBInlineOctets {
			continue
		}

		writer, err := r.conn.CreateLOB(r.ctx, kind)
		if err != nil {
			return nil, err
		}

		if _, err := writer.Write(octets); err != nil {
			return nil, err
		}

		locator, err := writer.Finish()
		if err != nil {
			return nil, err
		}

		values[i] = locator
	}

	return values, nil
}

// lobParameter gives the kind of a large object's type t; false if t is
// none.
func lobParameter(t DataType) (LOBKind, bool) {
	switch t.(type) {
	case CLOBType:
		return CharacterLOB, true
	case BLOBType:
		return BinaryLOB, true
	default:
		return 0, false
	}
}

// reject skips row, if skipping is asked; else the import stops at it.
func (r *insertRun) reject(row Rejected) *ImportError {
	if !r.how.Skip {
		return r.stop(&row.Position, row.Cause, nil)
	}

	r.skipped++

	if r.rejected != nil {
		r.rejected(row)
	}

	return nil
}

// stop rolls the batch back and gives the import's failure: at position,
// for cause, or the source's err.
func (r *insertRun) stop(position *Position, cause Cause, err error) *ImportError {
	if !r.conn.IsLost() && r.conn.InTransaction() {
		// The import has failed already: a rollback that fails too loses
		// the connection, which ends the session's transaction.
		_, _ = r.conn.Query(r.ctx, rollbackStatement)
	}

	stopped := &ImportError{Cause: cause, Committed: r.committed, RolledBack: r.inBatch}
	if err != nil {
		return sourceFailed(err, stopped)
	}

	if position != nil {
		stopped.place(*position)
	}

	return stopped
}

// commit commits the batch.
func (r *insertRun) commit() *ImportError {
	if r.conn.InTransaction() {
		if _, err := r.conn.Query(r.ctx, commitStatement); err != nil {
			return &ImportError{Cause: sqlCause(err), Committed: r.committed, RolledBack: r.inBatch}
		}
	}

	r.committed += r.inBatch
	r.inBatch = 0

	return nil
}
