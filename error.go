package turborel

// The driver's one error type (RULES.md R-ERR-1). A failure the server
// sends and one the driver finds itself are told the same way, by
// SQLCODE and SQLSTATE, so a program handles both alike.

import (
	"fmt"
	"strconv"
)

// SQLCODE and SQLSTATE of the failures the driver makes itself.
const (
	// sqlcodeConnection is the SQLCODE of a connection that failed
	// [PROTOCOL Errors and notices].
	sqlcodeConnection int32 = -919
	// sqlstateConnectionFailure is the SQLSTATE of a connection that
	// failed: class 08, connection exception, subclass 006.
	sqlstateConnectionFailure = "08006"
	// sqlstateInvalidAuthorization is the SQLSTATE of credentials refused:
	// class 28, invalid authorization specification.
	sqlstateInvalidAuthorization = "28000"
	// sqlcodeData is the SQLCODE of a value that is not one, as
	// turborel's data exception.
	sqlcodeData int32 = -301
	// sqlstateOutOfRange is the SQLSTATE of a number out of range: class
	// 22, data exception, subclass 003.
	sqlstateOutOfRange = "22003"
	// sqlstateInvalidJSON is the SQLSTATE of a JSON value that is not
	// one: class 22, data exception, subclass 032.
	sqlstateInvalidJSON = "22032"
	// sqlstateDataException is the SQLSTATE of input that cannot be read:
	// class 22, data exception.
	sqlstateDataException = "22000"
	// sqlcodeMisuse and sqlstateMisuse are of the program's misuse of the
	// driver that turborel-client has no case of, as turborel's API
	// failure.
	sqlcodeMisuse  int32 = -910
	sqlstateMisuse       = "TB910"
	// sqlcodeBeyond and sqlstateFeatureNotSupported are of what is past
	// turborel's limits: class 0A, feature not supported.
	sqlcodeBeyond               int32 = -900
	sqlstateFeatureNotSupported       = "0A000"
)

// Error is every failure the driver returns: one the server sent, or one
// the driver found, such as a malformed reply. Its fields are those of
// the protocol's failure [PROTOCOL Composite fields].
type Error struct {
	// SQLCode is the failure's SQLCODE, a negative number.
	SQLCode int32
	// SQLState is the failure's SQLSTATE, five characters.
	SQLState string
	// Message says what went wrong, in plain words.
	Message string
	// Rule is the clause of a standard the failure cites, as turborel's
	// messages do; empty if it cites none.
	Rule string
	// Position is where in the statement the failure was found, in
	// characters, if Placed.
	Position uint64
	// Placed says whether Position is known.
	Placed bool
	// Active says whether the session's transaction is active after the
	// failure.
	Active bool
	// Diagnostics is what the failure is about, as the server told it
	// with the capability diagnostics; nil if it told nothing.
	Diagnostics *Diagnostics
	// cause is what made the driver give up, such as a context done.
	cause error
}

// Unwrap gives what made the driver give up, such as a context done, so
// errors.Is(err, context.Canceled) holds of a call a context cancelled.
func (e *Error) Unwrap() error { return e.cause }

// withCause gives err with cause as its cause, if err is an *Error and
// cause is not nil.
func withCause(err, cause error) error {
	failure, ok := err.(*Error) //nolint:errorlint // Only the driver's own failure takes a cause.
	if !ok || cause == nil {
		return err
	}

	given := *failure
	given.cause = cause

	return &given
}

// Diagnostics is what a failure or a notice says beyond its SQLSTATE and
// message [PROTOCOL Errors and notices]. A field the server did not
// tell is empty.
type Diagnostics struct {
	// Severity is ERROR, FATAL, WARNING or NOTICE.
	Severity string
	// Detail says more than the message, as the key a constraint
	// refused.
	Detail string
	// Hint says what may be done about it.
	Hint string
	// Context is the trigger or routine running, as "trigger S.T".
	Context string
	// Schema is the schema of what it is about.
	Schema string
	// Table is the table or view it is about.
	Table string
	// Column is the column it is about.
	Column string
	// DataType is the data type of the value it is about.
	DataType string
	// Constraint is the constraint broken.
	Constraint string
}

// Error gives the failure as one line, in the form turborel-client
// gives it: its SQLCODE, SQLSTATE and message, then the rule it cites
// and where, if known.
func (e *Error) Error() string {
	line := fmt.Sprintf("SQLCODE %d (SQLSTATE %s): %s", e.SQLCode, e.SQLState, e.Message)
	if e.Rule != "" {
		line += " [" + e.Rule + "]"
	}

	if e.Placed {
		line += " (at character " + strconv.FormatUint(e.Position, decimalRadix) + ")"
	}

	return line
}

// broken is a connection failure, SQLCODE -919 and SQLSTATE 08006. The
// protocol fails the connection for a malformed frame, so after it the
// connection is not used again [PROTOCOL Frames].
func broken(message string) *Error {
	return &Error{SQLCode: sqlcodeConnection, SQLState: sqlstateConnectionFailure, Message: message}
}

// outOfRange is a number a program gave that the protocol cannot carry,
// SQLSTATE 22003.
func outOfRange(message string) *Error {
	return &Error{SQLCode: sqlcodeData, SQLState: sqlstateOutOfRange, Message: message}
}

// misused is the program's misuse of the driver, SQLSTATE TB910.
func misused(message string) *Error {
	return &Error{SQLCode: sqlcodeMisuse, SQLState: sqlstateMisuse, Message: message}
}

// beyond is what is past turborel's limits, SQLSTATE 0A000.
func beyond(message string) *Error {
	return &Error{SQLCode: sqlcodeBeyond, SQLState: sqlstateFeatureNotSupported, Message: message}
}

// invalidJSON is a JSON value a program gave that is not one, SQLSTATE
// 22032.
func invalidJSON(message string) *Error {
	return &Error{SQLCode: sqlcodeData, SQLState: sqlstateInvalidJSON, Message: message}
}
