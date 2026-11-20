package turborel

// A pool of connections [CLIENT Pools]: a bounded set a program's
// goroutines share. Pool.Get hands out an idle connection that answers,
// or makes one while fewer than PoolConfig.Max are made, or waits for one
// to come back, at most PoolConfig.Wait. A connection handed out comes
// back as its Pooled is released, reset to the state it logged in with;
// one lost, or whose reset fails, is closed instead, so no borrower sees
// another's session. Idle beyond PoolConfig.IdleTimeout, or older than
// PoolConfig.MaxLifetime, a connection is closed rather than handed out,
// and no more than PoolConfig.MaxIdle are kept idle. The pool starts no
// goroutine, each wait being the waiting borrower's.

import (
	"context"
	"strconv"
	"sync"
	"time"
)

// The defaults of a pool.
const (
	// DefaultPoolMax is the connections a pool makes at most.
	DefaultPoolMax = 10
	// DefaultPoolWait is how long Get waits for a connection to come back:
	// as long as a step of connecting.
	DefaultPoolWait = connectWithin
	// DefaultIdleTimeout is how long a connection may stay idle.
	DefaultIdleTimeout = 10 * time.Minute
	// DefaultMaxLifetime is how long a connection lives, so a pool moves
	// to servers added and settings changed in time.
	DefaultMaxLifetime = time.Hour
	// ResetWithin is how long a pooled connection's check and reset wait
	// on its server, at most.
	ResetWithin = connectWithin
	// ResetMostStatements is the most statements a borrower may leave
	// prepared for the connection to be reset by statements rather than
	// closed.
	ResetMostStatements = 256
)

// The statements a pool checks and resets a connection by, of a server
// that grants neither ping nor reset.
const (
	poolCheck        = "ROLLBACK"
	resetTimeZone    = "SET TIME ZONE LOCAL"
	resetAuthorizing = "SET SESSION AUTHORIZATION "
	resetSchema      = "SET SCHEMA "
)

// PoolSource makes a pool's connections: a Settings, or any type whose
// Connect makes one.
type PoolSource interface {
	Connect(ctx context.Context) (*Conn, error)
}

// PoolSourceFunc is a function that makes a connection, as a PoolSource.
type PoolSourceFunc func(ctx context.Context) (*Conn, error)

// Connect calls f.
func (f PoolSourceFunc) Connect(ctx context.Context) (*Conn, error) { return f(ctx) }

// PoolConfig says how a pool keeps its connections. A zero IdleTimeout or
// MaxLifetime is no limit.
type PoolConfig struct {
	// Max is the most connections made at once, handed out and idle.
	Max int
	// MaxIdle is the most kept idle.
	MaxIdle int
	// IdleTimeout is how long one may stay idle.
	IdleTimeout time.Duration
	// MaxLifetime is how long one lives from when it was made.
	MaxLifetime time.Duration
	// Wait is how long Get waits for one to come back.
	Wait time.Duration
	// Check says whether an idle connection is checked to answer before
	// it is handed out.
	Check bool
	// Reset are statements run, after the pool's own, as each connection
	// comes back.
	Reset []string
}

// DefaultPoolConfig is turborel-client's: 10 connections, 10 idle, idle
// 10 minutes, living an hour, a wait of 30 seconds, each checked.
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		Max: DefaultPoolMax, MaxIdle: DefaultPoolMax, IdleTimeout: DefaultIdleTimeout, MaxLifetime: DefaultMaxLifetime,
		Wait: DefaultPoolWait, Check: true,
	}
}

// idleConn is a connection idle in the pool: when it was made, and since
// when idle.
type idleConn struct {
	conn  *Conn
	made  time.Time
	since time.Time
}

// Pool is a pool of connections, shared by the goroutines that hold it.
type Pool struct {
	source PoolSource
	config PoolConfig

	mu       sync.Mutex
	idle     []idleConn
	made     int
	returned chan struct{}
}

// Pooled is a connection handed out by a pool, given back by Release.
type Pooled struct {
	*Conn

	made time.Time
	pool *Pool
}

// NewPool gives a pool of connections source makes, kept as config says.
func NewPool(source PoolSource, config PoolConfig) *Pool {
	return &Pool{source: source, config: config, returned: make(chan struct{})}
}

// Get gives a connection: an idle one that answers, else a new one while
// the pool has room, else the first to come back within PoolConfig.Wait.
// It fails with SQLSTATE 08001 if none comes back in time, or with the
// source's failure to make one, or the context's.
func (p *Pool) Get(ctx context.Context) (*Pooled, error) {
	deadline := time.Now().Add(p.config.Wait)

	for {
		idle, makeOne, err := p.next(ctx, deadline)
		if err != nil {
			return nil, err
		}

		if makeOne {
			return p.makeOne(ctx)
		}

		if pooled := p.checked(ctx, idle); pooled != nil {
			return pooled, nil
		}
	}
}

// Counts gives how many connections are made, handed out and idle, and
// how many of them are idle.
func (p *Pool) Counts() (int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.made, len(p.idle)
}

// signal wakes the borrowers waiting for a connection; the caller holds
// the lock.
func (p *Pool) signal() {
	close(p.returned)
	p.returned = make(chan struct{})
}

// fresh says whether idle may still be handed out at now.
func (p *Pool) fresh(idle idleConn, now time.Time) bool {
	lived := p.config.MaxLifetime <= 0 || now.Sub(idle.made) < p.config.MaxLifetime
	rested := p.config.IdleTimeout <= 0 || now.Sub(idle.since) < p.config.IdleTimeout

	return lived && rested && !idle.conn.IsLost()
}

// unmade counts one fewer connection made, so a waiter may make one.
func (p *Pool) unmade() {
	p.mu.Lock()
	p.made = max(p.made-1, 0)
	p.signal()
	p.mu.Unlock()
}

// next gives what Get takes next, an idle connection or a place to make
// one, waiting for a connection to come back until deadline; connections
// gone stale closed on the way.
func (p *Pool) next(ctx context.Context, deadline time.Time) (idleConn, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for {
		now := time.Now()

		for len(p.idle) > 0 {
			idle := p.idle[len(p.idle)-1]
			p.idle = p.idle[:len(p.idle)-1]

			if p.fresh(idle, now) {
				return idle, false, nil
			}

			_ = idle.conn.Close() // A connection gone stale is closed; its closing's failure changes nothing.
			p.made = max(p.made-1, 0)
		}

		if p.made < p.config.Max {
			p.made++

			return idleConn{}, true, nil
		}

		left := deadline.Sub(now)
		if left <= 0 {
			return idleConn{}, false, invalid("no connection of the pool's " + strconv.Itoa(p.config.Max) +
				" came back within " + p.config.Wait.String())
		}

		returned := p.returned

		p.mu.Unlock()

		timer := time.NewTimer(left)

		select {
		case <-returned:
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			p.mu.Lock()

			return idleConn{}, false, withCause(invalid("the wait for a connection of the pool was given up"), ctx.Err())
		}

		timer.Stop()
		p.mu.Lock()
	}
}

// checked gives idle, handed out if it answers or need not be checked;
// else nil, the connection closed and its place given up.
func (p *Pool) checked(ctx context.Context, idle idleConn) *Pooled {
	if p.config.Check && answers(ctx, idle.conn) != nil {
		_ = idle.conn.Close() // A connection that does not answer is closed.
		p.unmade()

		return nil
	}

	return &Pooled{Conn: idle.conn, made: idle.made, pool: p}
}

// makeOne makes a new connection, in the place next took for it.
func (p *Pool) makeOne(ctx context.Context) (*Pooled, error) {
	c, err := p.source.Connect(ctx)
	if err != nil {
		p.unmade()

		return nil, err //nolint:wrapcheck // The source's own failure, as turborel-client gives it.
	}

	return &Pooled{Conn: c, made: time.Now(), pool: p}, nil
}

// Release gives the connection back, reset; or closes it, if it is lost,
// its reset fails, it has lived its time, or enough are idle. The Pooled
// is of no use after.
func (p *Pooled) Release() {
	c, pool := p.Conn, p.pool
	if c == nil {
		return
	}

	p.Conn = nil

	lived := pool.config.MaxLifetime > 0 && time.Since(p.made) >= pool.config.MaxLifetime
	kept := !lived && reset(context.Background(), c, pool.config.Reset) == nil

	pool.mu.Lock()
	defer pool.mu.Unlock()

	if kept && len(pool.idle) < pool.config.MaxIdle {
		pool.idle = append(pool.idle, idleConn{conn: c, made: p.made, since: time.Now()})
	} else {
		_ = c.Close() // A connection not kept is closed.
		pool.made = max(pool.made-1, 0)
	}

	pool.signal()
}

// answers checks that c answers: by Ping, granted ping; else by ROLLBACK,
// which a session outside a transaction completes doing nothing.
func answers(ctx context.Context, c *Conn) error {
	return bounded(c, func() error {
		if c.granted[CapabilityPing] {
			return c.Ping(ctx)
		}

		_, err := c.Query(ctx, poolCheck)

		return err
	})
}

// bounded runs run with each of c's waits at most ResetWithin, or the
// connection's own limit if shorter, the limit put back after.
func bounded(c *Conn, run func() error) error {
	before := c.step

	within := ResetWithin
	if before > 0 && before < within {
		within = before
	}

	c.step = within
	err := run()
	c.step = before

	return err
}

// reset gives c's session back as it logged in, and runs more: by the
// protocol's Reset, granted reset; else by statements in one pipeline.
// Either way autocommit is off after.
func reset(ctx context.Context, c *Conn, more []string) error {
	return bounded(c, func() error { return resetNow(ctx, c, more) })
}

// resetNow resets c as reset says, unbounded.
func resetNow(ctx context.Context, c *Conn, more []string) error {
	if c.IsLost() {
		return broken("a lost connection is not given back")
	}

	c.autocommit = false

	if c.granted[CapabilityReset] {
		if err := c.Reset(ctx); err != nil {
			return err
		}

		// Every statement it prepared went with the session it had.
		c.nextStatement = 0

		for _, sql := range more {
			if _, err := c.Query(ctx, sql); err != nil {
				return err
			}
		}

		return nil
	}

	return resetByStatements(ctx, c, more)
}

// resetByStatements resets c by statements, of a server that grants no
// reset: its transaction rolled back, the statements it prepared closed
// and destroyed, its user, schema and zone those of its login, then more.
func resetByStatements(ctx context.Context, c *Conn, more []string) error {
	prepared := c.nextStatement
	if prepared > ResetMostStatements {
		return broken("too many statements were left prepared to destroy")
	}

	login := delimitedLiteral(c.Authorization())

	p := c.Pipeline().Query(rollbackStatement)
	for number := range prepared {
		p.release(number)
	}

	p.Query(resetAuthorizing + login).Query(resetSchema + login).Query(resetTimeZone)

	for _, sql := range more {
		p.Query(sql)
	}

	outcomes, err := p.Run(ctx)
	if err != nil {
		return err
	}

	releases := int(prepared + prepared)
	for i, o := range outcomes {
		if o.Failure != nil && (i == 0 || i > releases) {
			return o.Failure
		}
	}

	c.nextStatement = 0

	return nil
}

// delimitedLiteral gives name as a character string literal of a
// delimited identifier, as SET SESSION AUTHORIZATION and SET SCHEMA take
// one, so a name of any case or character is the same name.
func delimitedLiteral(name string) string {
	return quoted(delimited(name))
}
