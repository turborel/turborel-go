package turborel

// Controlling the session [CLIENT Controlling the session] and what the
// last statement's completion told [CLIENT What the server tells]: Reset
// and ChangeUser, several statements as one request, and the rows a
// statement counted, the identity values it generated, the server's
// time, the commit's position and its conditions.

import (
	"context"
	"slices"
)

// Ran is what a text of several statements gave: each statement's
// outcome, in order, and the failure that stopped the rest, if one did.
type Ran struct {
	Outcomes []*Outcome
	Failure  *Error
}

// RowsAffected gives the rows the last statement inserted, updated,
// deleted or merged, as GET DIAGNOSTICS ROW_COUNT gives them, or the
// rows a query returned; false unless row-counts told them.
func (c *Conn) RowsAffected() (uint64, bool) {
	if c.last == nil || !c.last.Counted {
		return 0, false
	}

	return c.last.Rows, true
}

// LastGenerated gives the identity values the last statement, an INSERT,
// generated, in the order of its rows, as SQL writes them.
func (c *Conn) LastGenerated() []string {
	if c.last == nil {
		return nil
	}

	return slices.Clone(c.last.Generated)
}

// ServerTime gives the server's time for the last statement; nil unless
// row-counts told it.
func (c *Conn) ServerTime() *Timing {
	if c.last == nil || c.last.Time == nil {
		return nil
	}

	t := *c.last.Time

	return &t
}

// CommitPosition gives the log position of the last commit the server
// told of, with commit-position: set as REPLICATION.READ_AFTER on a
// replica's session, its statements read what the commit made.
func (c *Conn) CommitPosition() (uint64, bool) {
	if c.position == nil {
		return 0, false
	}

	return *c.position, true
}

// Conditions gives the conditions the last statement raised, with
// conditions granted.
func (c *Conn) Conditions() []*Error {
	if c.last == nil {
		return nil
	}

	return slices.Clone(c.last.Conditions)
}

// Reset puts the session back as it was at login: its transaction rolled
// back, its prepared statements, cursors, temporary tables and settings
// gone, its user and cancel key kept. It needs reset granted.
func (c *Conn) Reset(ctx context.Context) error {
	if err := c.needs(CapabilityReset); err != nil {
		return err
	}

	return c.completed(ctx, resetRequest{})
}

// ChangeUser authenticates the connection again as user, proved by
// mechanism, as Hello does: accepted, the session is renewed as by Reset,
// under a new cancel key; refused, the connection fails. It needs
// change-user granted.
func (c *Conn) ChangeUser(ctx context.Context, user string, mechanism Mechanism) error {
	if err := c.needs(CapabilityChangeUser); err != nil {
		return err
	}

	defer c.watch(ctx)()

	req := changeUserRequest{user: user, mechanism: mechanism.Name(), initial: mechanism.Initial()}
	if err := c.send(req); err != nil {
		return withCause(err, ctx.Err())
	}

	for {
		rep, err := c.receive()
		if err != nil {
			return withCause(err, ctx.Err())
		}

		done, err := c.changeStep(rep, mechanism)
		if done || err != nil {
			return withCause(err, ctx.Err())
		}
	}
}

// changeStep takes one reply of ChangeUser: true once Ready has come.
// Ready keeps the capabilities granted at login.
func (c *Conn) changeStep(rep reply, mechanism Mechanism) (bool, error) {
	switch r := rep.(type) {
	case readyReply:
		if verifier, ok := mechanism.(VerifyingMechanism); ok {
			if err := verifier.Verified(); err != nil {
				return false, c.loseBy(broken("the new user's login failed: " + err.Error()))
			}
		}

		c.authorization, c.key, c.server = r.authorization, r.key, r.server
		// The session is renewed as Reset renews it, so no transaction is
		// active, as the spec says; turborel-client keeps its flag (RULES.md
		// D8).
		c.active = false

		return true, nil
	case errorReply:
		return false, r.failure
	default:
		return c.loginStep(rep, mechanism)
	}
}

// QueryMany runs a text of several statements as one request, each in
// turn until one fails: each outcome, and the failure that stopped the
// rest. It needs multi-statement granted.
func (c *Conn) QueryMany(ctx context.Context, sql string) (*Ran, error) {
	if err := c.needs(CapabilityMultiStatement); err != nil {
		return nil, err
	}

	defer c.during(ctx)()

	req := queryRequest{sql: sql, ends: c.autocommit, attributes: c.requestAttributes(), multi: true}
	if err := c.send(req); err != nil {
		return nil, withCause(err, ctx.Err())
	}

	ran := &Ran{}

	for {
		outcome, more, err := c.outcome()
		if err != nil {
			return c.stopped(ran, withCause(err, ctx.Err()))
		}

		ran.Outcomes = append(ran.Outcomes, outcome)
		if !more {
			return ran, nil
		}
	}
}

// stopped gives what ran gave up to a statement's failure err: the
// connection's failure as the call's, a statement's as the one that
// stopped the rest.
func (c *Conn) stopped(ran *Ran, err error) (*Ran, error) {
	failure, ok := err.(*Error) //nolint:errorlint // A statement's failure is the driver's *Error.
	if c.lost != nil || !ok {
		return nil, err
	}

	ran.Failure = failure

	return ran, nil
}
