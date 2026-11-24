package turborel

// An import by bulk copy [CLIENT Importing]: its rows checked as the
// INSERT's parameters take them, then sent a batch to a copy, each copy
// committing its batch as it ends, with no round trip but the copy's end
// [PROTOCOL CopyEnd 'N']. A row the server refuses fails its copy, every
// row of the batch undone, the failure's detail naming the row, so the
// import says its record as it would of a row inserted alone.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// What the server's detail of a row a copy refused begins with, and ends.
const (
	copyRowPrefix = "row "
	copyRowSuffix = " of the copy"
)

// copies says whether an import of insert goes by copy: copy granted, no
// bad row skipped, no large object's column.
func (c *Conn) copies(insert *Statement, how Import) bool {
	if !c.granted[CapabilityCopy] || how.Skip {
		return false
	}

	for _, p := range insert.inputs {
		if _, isLOB := lobParameter(p.Type); isLOB {
			return false
		}
	}

	return true
}

// copyRowOf gives the row, from 1, a copy's failure names in its detail;
// 0 if it names none.
func copyRowOf(failure *Error) int {
	if failure == nil || failure.Diagnostics == nil {
		return 0
	}

	rest, ok := strings.CutPrefix(failure.Diagnostics.Detail, copyRowPrefix)
	if !ok {
		return 0
	}

	digits, ok := strings.CutSuffix(rest, copyRowSuffix)
	if !ok {
		return 0
	}

	n, err := strconv.ParseUint(digits, decimalRadix, 63)
	if err != nil {
		return 0
	}

	return int(n)
}

// copyRun is an import by copy under way.
type copyRun struct {
	how        Import
	parameters []Parameter
	committed  uint64
	batch      []Position
}

// stop gives the import's failure: at position, if placed, for cause,
// rolledBack rows of the batch undone.
func (r *copyRun) stop(position *Position, cause Cause, rolledBack int) *ImportError {
	stopped := &ImportError{Cause: cause, Committed: r.committed, RolledBack: len64(rolledBack)}
	if position != nil {
		stopped.place(*position)
	}

	return stopped
}

// run copies source's records into table's columns, a batch to a copy.
func (r *copyRun) run(ctx context.Context, c *Conn, source Source, table string, columns []string) (Imported, error) {
	for {
		more, failure := r.copied(ctx, c, source, table, columns)
		if failure != nil {
			return Imported{}, failure
		}

		if !more {
			return Imported{Rows: r.committed}, nil
		}
	}
}

// checked reads the next record and checks it as the INSERT's parameters
// take it: where it began, and its values; nil at the end.
func (r *copyRun) checked(source Source) (*Position, Row, *ImportError) {
	record, err := source.Record()
	if err != nil {
		return nil, nil, sourceFailed(err, r.stop(nil, Cause{}, len(r.batch)))
	}

	if record == nil {
		return nil, nil, nil
	}

	if wanted := len(r.parameters); len(record.Values) != wanted {
		message := fmt.Sprintf("[DESIGN] %d fields, for %d columns", len(record.Values), wanted)

		return nil, nil, r.stop(&record.Position, Cause{Input: message}, len(r.batch))
	}

	values, failure := assigned(record.Values, r.parameters)
	if failure != nil {
		return nil, nil, r.stop(&record.Position, sqlCause(failure), len(r.batch))
	}

	return &record.Position, values, nil
}

// copied copies a batch, its copy begun at its first record and ended at
// its last: whether records may follow.
func (r *copyRun) copied(
	ctx context.Context, c *Conn, source Source, table string, columns []string,
) (bool, *ImportError) {
	at, values, failure := r.checked(source)
	if failure != nil || at == nil {
		return false, failure
	}

	k, err := c.copyBegin(ctx, table, columns, true)
	if err != nil {
		return false, r.stop(nil, sqlCause(err), 0)
	}

	defer func() { _ = k.Abandon() }() // A copy the import stopped within is abandoned.

	for at != nil {
		if err := k.Send(values); err != nil {
			return false, r.stop(at, sqlCause(err), len(r.batch))
		}

		r.batch = append(r.batch, *at)
		if uint64(len(r.batch)) == r.how.Batch {
			return true, r.ended(k)
		}

		if at, values, failure = r.checked(source); failure != nil {
			return false, failure
		}
	}

	return false, r.ended(k)
}

// ended finishes the batch's copy: its rows committed, or its failure at
// the row it names.
func (r *copyRun) ended(k *Copy) *ImportError {
	rows, err := k.Finish()
	if err == nil {
		r.committed += rows
		r.batch = r.batch[:0]

		return nil
	}

	cause := sqlCause(err)

	row := copyRowOf(cause.SQL)
	if row < 1 || row > len(r.batch) {
		return r.stop(nil, cause, len(r.batch))
	}

	return r.stop(&r.batch[row-1], cause, row-1)
}
