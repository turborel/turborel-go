package turborel

// Whether a server takes changes [CLIENT Several servers]. A server alone
// does, and of a replicated group its primary; a replica following the
// group's log refuses every change. A server that grants status tells
// every session SERVER_READ_ONLY, read with no query; of one that does
// not, the role is read from INFORMATION_SCHEMA.REPLICAS, which shows a
// replica's role to the database's owner alone, a server alone having
// no such table.

import (
	"context"
	"errors"
	"strings"
	"unicode"
)

// RoleQuery is the query of a server's own role in its group.
const RoleQuery = "SELECT ROLE FROM INFORMATION_SCHEMA.REPLICAS WHERE IS_SELF = 'YES'"

// The words the role is told by.
const (
	readOnlyYes = "YES"
	rolePrimary = "PRIMARY"
)

// Role is what a server is, to a connection's session.
type Role uint8

// The roles a server is seen as.
const (
	// RoleReadWrite takes changes.
	RoleReadWrite Role = iota
	// RoleReadOnly takes none: a replica following its group.
	RoleReadOnly
	// RoleUnknown is a replica whose role the session cannot see.
	RoleUnknown
)

// String gives the role in words.
func (r Role) String() string {
	switch r {
	case RoleReadWrite:
		return "read-write"
	case RoleReadOnly:
		return "read-only"
	case RoleUnknown:
	}

	return "of a role this session cannot see"
}

// noSuchTable says whether a failure's SQLSTATE is of a table that does
// not exist, as a server alone answers RoleQuery: class 42, a syntax
// error or access rule violation, as turborel-client reads it.
func noSuchTable(sqlstate string) bool {
	return strings.HasPrefix(sqlstate, noSuchTableClass)
}

// noSuchTableClass is the class of a failure of a table that does not
// exist, syntax errors' and access rule violations'.
const noSuchTableClass = "42"

// roleOf gives the role the outcome of RoleQuery shows.
func roleOf(outcome *Outcome, err error) (Role, error) {
	if err != nil {
		if failure, ok := errors.AsType[*Error](err); ok && noSuchTable(failure.SQLState) {
			return RoleReadWrite, nil
		}

		return RoleUnknown, err
	}

	if len(outcome.Rows) == 0 || len(outcome.Rows[0]) == 0 {
		return RoleUnknown, nil
	}

	if strings.TrimRightFunc(valueText(outcome.Rows[0][0]), unicode.IsSpace) == rolePrimary {
		return RoleReadWrite, nil
	}

	return RoleReadOnly, nil
}

// Role gives the role of the connection's server, as its session sees
// it: as SERVER_READ_ONLY last told it, if the server tells it; else
// asked by RoleQuery as a statement that is its own transaction, so it
// leaves none open.
func (c *Conn) Role(ctx context.Context) (Role, error) {
	if told, ok := c.Setting(StatusServerReadOnly); ok {
		if told == readOnlyYes {
			return RoleReadOnly, nil
		}

		return RoleReadWrite, nil
	}

	autocommit := c.autocommit
	c.autocommit = true
	outcome, err := c.Query(ctx, RoleQuery)
	c.autocommit = autocommit

	return roleOf(outcome, err)
}
