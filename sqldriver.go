package turborel

// The database/sql driver (RULES.md D2), registered as "turborel": a
// connection by sql.Open of a turborel:// URI, or by sql.OpenDB of a
// DBConnector of Settings made in Go. Outside a transaction each statement
// is its own, as database/sql has it, by the protocol's autocommit; a
// transaction begun by BeginTx is the session's, its isolation level and
// access mode set by SET TRANSACTION. A query's rows are read through its
// cursor, a batch at a time. A context done while a statement runs
// cancels it on the server, its failure wrapping the context's error.
// Every failure is an *Error; one of a connection lost before the call
// wraps driver.ErrBadConn too, so database/sql takes another.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"reflect"
	"strconv"
	"strings"
)

// DriverName is the name the driver is registered by.
const DriverName = "turborel"

// fetchRows is how many rows a query's cursor reads at a time.
const fetchRows = 1000

// The statements a transaction is begun and ended by.
const (
	setTransaction = "SET TRANSACTION "
	readOnlyMode   = "READ ONLY"
)

//nolint:gochecknoinits // database/sql takes a driver by its registration, as every Go driver registers.
func init() {
	sql.Register(DriverName, Driver{})
}

// Driver is the database/sql driver.
type Driver struct{}

// Open connects as the connection URI name says.
func (Driver) Open(name string) (driver.Conn, error) {
	settings, err := SettingsFromURI(name)
	if err != nil {
		return nil, err
	}

	return NewDBConnector(settings).Connect(context.Background())
}

// OpenConnector gives a connector of the settings of the connection URI
// name.
func (Driver) OpenConnector(name string) (driver.Connector, error) {
	settings, err := SettingsFromURI(name)
	if err != nil {
		return nil, err
	}

	return NewDBConnector(settings), nil
}

// DBConnector makes database/sql's connections as settings say.
type DBConnector struct {
	settings *Settings
}

// NewDBConnector gives a connector of settings, for sql.OpenDB.
func NewDBConnector(settings *Settings) *DBConnector { return &DBConnector{settings: settings} }

// Connect makes a connection as the settings say.
func (c *DBConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.settings.Connect(ctx)
	if err != nil {
		return nil, err
	}

	conn.SetAutocommit(true)

	return &sqlConn{conn: conn}, nil
}

// Driver gives the driver.
func (*DBConnector) Driver() driver.Driver { return Driver{} }

// sqlConn is a connection as database/sql uses it.
type sqlConn struct {
	conn *Conn
}

// NativeConn gives the native connection of a database/sql driver
// connection, as sql.Conn's Raw hands it over; false if it is none of the
// driver's.
func NativeConn(driverConn any) (*Conn, bool) {
	c, ok := driverConn.(*sqlConn)
	if !ok {
		return nil, false
	}

	return c.conn, true
}

// Prepare prepares query.
func (c *sqlConn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

// PrepareContext prepares query.
func (c *sqlConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if err := c.usable(); err != nil {
		return nil, err
	}

	st, err := c.conn.Prepare(ctx, query)
	if err != nil {
		return nil, err
	}

	return &sqlStmt{conn: c, st: st}, nil
}

// Close ends the session.
func (c *sqlConn) Close() error {
	if c.conn.IsLost() {
		return nil
	}

	return c.conn.Terminate()
}

// Begin begins a transaction.
func (c *sqlConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

// BeginTx begins a transaction of the isolation level and access mode
// opts gives, by SET TRANSACTION: the four levels of SQL; another is
// refused (SQLSTATE TB910).
func (c *sqlConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if err := c.usable(); err != nil {
		return nil, err
	}

	var modes []string

	if level := sql.IsolationLevel(opts.Isolation); level != sql.LevelDefault {
		name, ok := isolationNames()[level]
		if !ok {
			return nil, misused("the isolation level " + level.String() + " is none of SQL's")
		}

		modes = append(modes, "ISOLATION LEVEL "+name)
	}

	if opts.ReadOnly {
		modes = append(modes, readOnlyMode)
	}

	c.conn.SetAutocommit(false)

	if len(modes) > 0 {
		if _, err := c.conn.Query(ctx, setTransaction+joinModes(modes)); err != nil {
			c.conn.SetAutocommit(true)

			return nil, err
		}
	}

	return &sqlTx{conn: c}, nil
}

// isolationNames gives SQL's isolation levels, by database/sql's.
func isolationNames() map[sql.IsolationLevel]string {
	return map[sql.IsolationLevel]string{
		sql.LevelReadUncommitted: "READ UNCOMMITTED", sql.LevelReadCommitted: "READ COMMITTED",
		sql.LevelRepeatableRead: "REPEATABLE READ", sql.LevelSerializable: "SERIALIZABLE",
	}
}

// joinModes gives a SET TRANSACTION's modes, separated by commas.
func joinModes(modes []string) string {
	return strings.Join(modes, ", ")
}

// ExecContext runs query with args: by Query of no args, else prepared,
// executed and deallocated.
func (c *sqlConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := c.usable(); err != nil {
		return nil, err
	}

	if len(args) == 0 {
		out, err := c.conn.Query(ctx, query)
		if err != nil {
			return nil, err
		}

		return resultOf(c.conn, out), nil
	}

	st, err := c.conn.Prepare(ctx, query)
	if err != nil {
		return nil, err
	}

	defer func() { _ = c.conn.Deallocate(ctx, st) }() // Its failure is the connection's, which says so at the next call.

	return (&sqlStmt{conn: c, st: st}).ExecContext(ctx, args)
}

// QueryContext runs query with args, its rows read through its cursor.
func (c *sqlConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := c.usable(); err != nil {
		return nil, err
	}

	st, err := c.conn.Prepare(ctx, query)
	if err != nil {
		return nil, err
	}

	rows, err := (&sqlStmt{conn: c, st: st}).rows(ctx, args)
	if err != nil {
		_ = c.conn.Deallocate(ctx, st) // The statement's failure is the one given.

		return nil, err
	}

	rows.deallocate = true

	return rows, nil
}

// Ping checks the server answers: by Ping where granted, else ROLLBACK.
func (c *sqlConn) Ping(ctx context.Context) error {
	if err := c.usable(); err != nil {
		return err
	}

	if c.conn.granted[CapabilityPing] {
		return c.conn.Ping(ctx)
	}

	_, err := c.conn.Query(ctx, poolCheck)

	return err
}

// ResetSession refuses a connection lost, so database/sql takes another.
func (c *sqlConn) ResetSession(context.Context) error {
	if c.conn.lost != nil {
		return driver.ErrBadConn
	}

	return nil
}

// IsValid says whether the connection is not lost.
func (c *sqlConn) IsValid() bool { return c.conn.lost == nil }

// CheckNamedValue makes a Go value the Value it is sent as, an sql.Out
// kept for its CALL to bind; a named argument is refused, as the
// driver's parameters are each a ?.
func (c *sqlConn) CheckNamedValue(nv *driver.NamedValue) error {
	if nv.Name != "" {
		return misused("the parameter " + nv.Name + " is named; the driver's parameters are each a ?")
	}

	if _, ok := nv.Value.(sql.Out); ok {
		return nil
	}

	v, err := valueOf(nv.Value)
	if err != nil {
		return err
	}

	nv.Value = v

	return nil
}

// usable fails with the connection's failure, wrapping driver.ErrBadConn,
// if it was lost before the call.
func (c *sqlConn) usable() error {
	if c.conn.lost == nil {
		return nil
	}

	return withCause(c.conn.lost, driver.ErrBadConn)
}

// sqlTx is a transaction as database/sql uses it.
type sqlTx struct {
	conn *sqlConn
}

// Commit commits the transaction, each statement after its own again.
func (t *sqlTx) Commit() error { return t.end(commitStatement) }

// Rollback rolls the transaction back, each statement after its own
// again.
func (t *sqlTx) Rollback() error { return t.end(rollbackStatement) }

// end ends the transaction by sql.
func (t *sqlTx) end(sql string) error {
	c := t.conn.conn
	defer c.SetAutocommit(true)

	_, err := c.Query(context.Background(), sql)

	return err
}

// sqlStmt is a prepared statement as database/sql uses it.
type sqlStmt struct {
	conn *sqlConn
	st   *Statement
}

// Close deallocates the statement.
func (s *sqlStmt) Close() error {
	if s.conn.conn.IsLost() {
		return nil
	}

	return s.conn.conn.Deallocate(context.Background(), s.st)
}

// NumInput gives the statement's `?`s: of a CALL whose modes the server
// told, its OUT ones too, each given an sql.Out; else its inputs.
func (s *sqlStmt) NumInput() int { return len(s.st.Parameters()) }

// Exec executes the statement.
func (s *sqlStmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.ExecContext(context.Background(), named(args))
}

// Query runs the statement, a query.
func (s *sqlStmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.QueryContext(context.Background(), named(args))
}

// named gives args as named values, by their places.
func named(args []driver.Value) []driver.NamedValue {
	all := make([]driver.NamedValue, len(args))
	for i, a := range args {
		all[i] = driver.NamedValue{Ordinal: i + 1, Value: a}
	}

	return all
}

// ExecContext executes the statement with args.
func (s *sqlStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if err := s.conn.usable(); err != nil {
		return nil, err
	}

	given, outs, err := s.bind(args)
	if err != nil {
		return nil, err
	}

	out, err := s.conn.conn.Execute(ctx, s.st, given)
	if err != nil {
		return nil, err
	}

	if err := fill(ctx, s.conn.conn, out, outs); err != nil {
		return nil, err
	}

	return resultOf(s.conn.conn, out), nil
}

// QueryContext runs the statement with args, a query's rows read through
// its cursor; another statement's, none.
func (s *sqlStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return s.rows(ctx, args)
}

// rows runs the statement with args, as QueryContext does.
func (s *sqlStmt) rows(ctx context.Context, args []driver.NamedValue) (*sqlRows, error) {
	if err := s.conn.usable(); err != nil {
		return nil, err
	}

	given, outs, err := s.bind(args)
	if err != nil {
		return nil, err
	}

	c := s.conn.conn
	if len(s.st.columns) == 0 || len(outs) > 0 {
		out, err := c.Execute(ctx, s.st, given)
		if err != nil {
			return nil, err
		}

		if err := fill(ctx, c, out, outs); err != nil {
			return nil, err
		}

		return &sqlRows{ctx: ctx, conn: c, st: s.st, rows: out.Rows}, nil
	}

	batch, err := c.OpenCursor(ctx, s.st, given, fetchRows)
	if err != nil {
		return nil, err
	}

	return &sqlRows{ctx: ctx, conn: c, st: s.st, rows: batch.Rows, more: batch.More}, nil
}

// sqlResult is a statement's result: the rows it changed, and the last
// identity value it generated, if an integer.
type sqlResult struct {
	rows      uint64
	counted   bool
	generated []string
}

// resultOf gives the result of the statement out completed.
func resultOf(c *Conn, out *Outcome) sqlResult {
	rows, counted := c.RowsAffected()
	if out != nil && out.Completion.Counted {
		rows, counted = out.Completion.Rows, true
	}

	return sqlResult{rows: rows, counted: counted, generated: c.LastGenerated()}
}

// LastInsertId gives the last identity value the statement generated, if
// it is an integer.
func (r sqlResult) LastInsertId() (int64, error) {
	if len(r.generated) == 0 {
		return 0, misused("the statement generated no identity value")
	}

	last := r.generated[len(r.generated)-1]

	id, err := strconv.ParseInt(last, decimalRadix, 64)
	if err != nil {
		return 0, misused("the identity value " + last + " is no integer an int64 holds")
	}

	return id, nil
}

// RowsAffected gives the rows the statement changed, as the server
// counted them.
func (r sqlResult) RowsAffected() (int64, error) {
	if !r.counted {
		return 0, misused("the server counted no rows: row-counts is not granted")
	}

	return int64(min(r.rows, uint64(1<<63-1))), nil
}

// sqlRows are a query's rows as database/sql reads them: those read, and
// the cursor's more.
type sqlRows struct {
	ctx        context.Context //nolint:containedctx // Next takes no context, so the rows keep their query's.
	conn       *Conn
	st         *Statement
	rows       []Row
	more       bool
	closed     bool
	deallocate bool
}

// Columns gives the columns' names.
func (r *sqlRows) Columns() []string {
	names := make([]string, len(r.st.columns))
	for i, c := range r.st.columns {
		names[i] = c.Name
	}

	return names
}

// Close closes the cursor, if it has more, and deallocates a statement
// the rows' query prepared.
func (r *sqlRows) Close() error {
	if r.closed {
		return nil
	}

	r.closed = true

	var err error
	if r.more && r.conn.lost == nil {
		err = r.conn.CloseCursor(context.Background(), r.st)
	}

	if r.deallocate && r.conn.lost == nil {
		if freed := r.conn.Deallocate(context.Background(), r.st); err == nil {
			err = freed
		}
	}

	return err
}

// Next reads the next row into dest, fetching another batch as needed.
func (r *sqlRows) Next(dest []driver.Value) error {
	for len(r.rows) == 0 {
		if !r.more {
			return io.EOF
		}

		batch, err := r.conn.Fetch(r.ctx, r.st, fetchRows)
		if err != nil {
			r.more = false

			return err
		}

		r.rows, r.more = batch.Rows, batch.More
	}

	row := r.rows[0]
	r.rows = r.rows[1:]

	for i := range dest {
		v, err := driverValue(r.ctx, r.conn, row[i])
		if err != nil {
			return err
		}

		dest[i] = v
	}

	return nil
}

// ColumnTypeDatabaseTypeName gives the name of column i's type: that
// of the type it was declared with, as the Information Schema's
// DATA_TYPE gives it, where the server told it, else that of the type
// its tag says.
func (r *sqlRows) ColumnTypeDatabaseTypeName(i int) string {
	if d := r.st.columns[i].Declared; d != nil {
		return d.Name
	}

	return typeName(r.st.columns[i].Type)
}

// ColumnTypeNullable says whether column i may hold a null, as the
// server knows.
func (r *sqlRows) ColumnTypeNullable(i int) (bool, bool) { return !r.st.columns[i].NotNull, true }

// ColumnTypeScanType gives the Go type Next gives of column i.
func (r *sqlRows) ColumnTypeScanType(i int) reflect.Type { return scanType(r.st.columns[i].Type) }

// ColumnTypeLength gives column i's length, of a string type.
func (r *sqlRows) ColumnTypeLength(i int) (int64, bool) {
	switch t := r.st.columns[i].Type.(type) {
	case CharacterType:
		return int64(t.Length), true
	case VaryingType:
		return int64(t.Length), true
	case BinaryType:
		return int64(t.Length), true
	case VarBinaryType:
		return int64(t.Length), true
	case CLOBType:
		return int64(min(t.Length, uint64(1<<63-1))), true
	case BLOBType:
		return int64(min(t.Length, uint64(1<<63-1))), true
	}

	return 0, false
}

// ColumnTypePrecisionScale gives column i's precision and scale, of an
// exact number.
func (r *sqlRows) ColumnTypePrecisionScale(i int) (int64, int64, bool) {
	if t, ok := r.st.columns[i].Type.(ExactType); ok {
		return int64(t.Precision), int64(t.Scale), true
	}

	return 0, 0, false
}
