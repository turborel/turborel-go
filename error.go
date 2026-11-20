package turborel

// The driver's one error type (RULES.md R-ERR-1). A failure the server
// sends and one the driver finds itself are told the same way, by
// SQLCODE and SQLSTATE, so a program handles both alike.

import "fmt"

// SQLCODE and SQLSTATE of the failures the driver makes itself, as the
// protocol gives them [PROTOCOL Errors and notices].
const (
	// sqlcodeConnection is the SQLCODE of a connection that failed.
	sqlcodeConnection int32 = -919
	// sqlstateConnectionFailure is the SQLSTATE of a connection that
	// failed: class 08, connection exception, subclass 006.
	sqlstateConnectionFailure = "08006"
)

// Error is every failure the driver returns: one the server sent, or one
// the driver found, such as a malformed reply.
type Error struct {
	// SQLCode is the failure's SQLCODE, a negative number.
	SQLCode int32
	// SQLState is the failure's SQLSTATE, five characters.
	SQLState string
	// Message says what went wrong, in plain words.
	Message string
}

// Error gives the failure as one line: its SQLCODE, SQLSTATE and
// message, in the form turborel-client gives it.
func (e *Error) Error() string {
	return fmt.Sprintf("SQLCODE %d (SQLSTATE %s): %s", e.SQLCode, e.SQLState, e.Message)
}

// broken is a connection failure, SQLCODE -919 and SQLSTATE 08006. The
// protocol fails the connection for a malformed frame, so after it the
// connection is not used again [PROTOCOL Frames].
func broken(message string) *Error {
	return &Error{SQLCode: sqlcodeConnection, SQLState: sqlstateConnectionFailure, Message: message}
}
