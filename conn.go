package turborel

// A connection to a server, the native API's [CLIENT Connecting]: one
// session of the server's, opened by Hello and the mechanism's exchange
// until Ready [PROTOCOL Opening a connection], then one request at a
// time. Every failure is the driver's *Error; a failure of the
// connection loses it, and it takes no more.

import (
	"bufio"
	"context"
	"io"
	"maps"
	"slices"
	"time"
)

// Config says how a connection is made: how long it waits, and what it
// asks of the server. Its zero value is the defaults.
type Config struct {
	// Timeouts bound the connection's waits; the zero value is no limit.
	Timeouts Timeouts
	// Offer is what the connection asks for; nil, DefaultOffer.
	Offer *Offer
}

// DefaultConfig is turborel-client's: DefaultTimeouts and DefaultOffer.
func DefaultConfig() *Config {
	offer := DefaultOffer()

	return &Config{Timeouts: DefaultTimeouts(), Offer: &offer}
}

// Conn is a connection to a server: a session of its. It is used by one
// goroutine at a time; a Canceller cancels its statement from another.
type Conn struct {
	stream        stream
	r             *bufio.Reader
	target        *Target
	authorization string
	server        string
	key           [keyBytes]byte
	granted       granted
	settings      map[string]string
	notices       []Notice
	events        []DebugEvent
	handler       func(Notice)
	timeouts      Timeouts
	step          time.Duration
	maxBody       uint32
	compressFrom  int
	compressing   bool
	active        bool
	autocommit    bool
	last          *Completion
	position      *uint64
	attributes    []Attribute
	nextStatement uint32
	// answeredByOne says the request under way is answered by one reply,
	// after any Notice and Status, so a value the driver cannot take reads
	// nothing past it.
	answeredByOne bool
	// copying is the copy under way, if one is.
	copying *Copy
	lost    *Error
}

// Connect connects to the server at address, host and port, over TCP as
// user, proved by mechanism, as DefaultConfig says. A secret mechanism,
// as Password or Scram, is refused before anything is sent (SQLSTATE
// 28000).
func Connect(ctx context.Context, address, user string, mechanism Mechanism) (*Conn, error) {
	return ConnectTo(ctx, TCP(address), user, mechanism, DefaultConfig())
}

// ConnectLocal connects to the server at the Unix socket path, or on
// Windows the named pipe path, as DefaultConfig says.
func ConnectLocal(ctx context.Context, path, user string, mechanism Mechanism) (*Conn, error) {
	return ConnectTo(ctx, Local(path), user, mechanism, DefaultConfig())
}

// ConnectTo connects to target as user, proved by mechanism, as config
// says; nil, DefaultConfig. A secret mechanism is refused over plain TCP
// before a connection is made (SQLSTATE 28000); over TLS a binding
// mechanism is bound to the connection first.
func ConnectTo(ctx context.Context, target Target, user string, mechanism Mechanism, config *Config) (*Conn, error) {
	if config == nil {
		config = DefaultConfig()
	}

	if target.IsPlainTCP() {
		if err := inTheClear(mechanism); err != nil {
			return nil, err
		}
	}

	s, channel, err := target.open(ctx, config.Timeouts.Connect)
	if err != nil {
		return nil, err
	}

	return connectOpened(ctx, target, s, channel, login{user: user, mechanism: mechanism, config: config})
}

// connectOpened logs in over s, reached at target, a binding mechanism
// bound to its TLS channel first.
func connectOpened(ctx context.Context, target Target, s stream, channel *TLSChannel, l login) (*Conn, error) {
	if binder, ok := l.mechanism.(BindingMechanism); ok && channel != nil {
		binder.Bind(*channel)
	}

	c, err := logIn(ctx, s, l)
	if err != nil {
		return nil, err
	}

	c.target = &target

	return c, nil
}

// Open opens a connection over a stream the program made, as user,
// proved by mechanism, as config says. Of such a stream the program
// answers for its privacy, and for binding a mechanism to its TLS; its
// waits are bounded where it takes deadlines.
func Open(ctx context.Context, s io.ReadWriteCloser, user string, mechanism Mechanism, config *Config) (*Conn, error) {
	if config == nil {
		config = DefaultConfig()
	}

	return logIn(ctx, deadlineless(s), login{user: user, mechanism: mechanism, config: config})
}

// deadlineless gives s as a stream, its deadlines its own if it has
// them, none if not.
func deadlineless(s io.ReadWriteCloser) stream {
	if withDeadlines, ok := s.(stream); ok {
		return withDeadlines
	}

	return noDeadline{s}
}

// noDeadline is a stream that takes no deadline.
type noDeadline struct {
	io.ReadWriteCloser
}

// SetDeadline does nothing: the stream's maker bounds its waits.
func (noDeadline) SetDeadline(time.Time) error { return nil }

// login is who logs in, how, and as what config says.
type login struct {
	user      string
	mechanism Mechanism
	config    *Config
}

// logIn sends Hello over s and answers each Challenge until Ready
// [PROTOCOL Opening a connection], each step within the connect timeout.
// A context done gives the login up.
func logIn(ctx context.Context, s stream, l login) (*Conn, error) {
	offer := DefaultOffer()
	if l.config.Offer != nil {
		offer = *l.config.Offer
	}

	if err := checkAttributes(offer.Attributes); err != nil {
		_ = s.Close() // The attributes' failure is the one given.

		return nil, err
	}

	c := &Conn{
		stream: s, r: bufio.NewReader(s), settings: map[string]string{},
		timeouts: l.config.Timeouts, step: l.config.Timeouts.Connect, maxBody: maxBody,
		compressFrom: offer.CompressFrom,
	}
	stop := context.AfterFunc(ctx, func() { _ = s.Close() }) // Closing ends any wait; the login's failure is given.
	defer stop()

	hello := helloRequest{
		user: l.user, mechanism: l.mechanism.Name(), initial: l.mechanism.Initial(),
		capabilities: offer.Capabilities, attributes: offer.Attributes,
	}
	if err := c.exchange(hello, l.mechanism); err != nil {
		_ = s.Close() // The login's failure is the one given.

		return nil, withCause(err, ctx.Err())
	}

	c.step = c.timeouts.Read

	return c, nil
}

// Authorization gives the session's authorization identifier, as Ready
// told it.
func (c *Conn) Authorization() string { return c.authorization }

// Server gives the server's product, as "turborel 0.0.0 (alpha)".
func (c *Conn) Server() string { return c.server }

// Granted says whether the server granted the capability name.
func (c *Conn) Granted(name string) bool { return c.granted[name] }

// GrantedCapabilities gives every capability the server granted, in the
// protocol's order.
func (c *Conn) GrantedCapabilities() []string {
	return slices.DeleteFunc(Capabilities(), func(name string) bool { return !c.granted[name] })
}

// Setting gives the session setting name as the server last told it,
// with no round trip: the session's own settings, and AUTHORIZATION,
// SCHEMA, READ_ONLY, SERVER_READ_ONLY, CATALOG, OWNER and SERVER_VERSION.
// None unless `status` was granted [CLIENT What the server tells].
func (c *Conn) Setting(name string) (string, bool) {
	value, ok := c.settings[name]

	return value, ok
}

// Settings gives every setting the server told, by name.
func (c *Conn) Settings() map[string]string { return maps.Clone(c.settings) }

// InTransaction says whether the session's transaction is active, as the
// server said after the last statement.
func (c *Conn) InTransaction() bool { return c.active }

// IsLost says whether the connection is lost: it takes no more.
func (c *Conn) IsLost() bool { return c.lost != nil }

// Target gives where the connection reached its server; false for one
// opened over a program's stream.
func (c *Conn) Target() (Target, bool) {
	if c.target == nil {
		return Target{}, false
	}

	return *c.target, true
}

// ReadTimeout gives how long each read or write waits; 0, no limit.
func (c *Conn) ReadTimeout() time.Duration { return c.timeouts.Read }

// SetReadTimeout bounds each read or write of the server from now on;
// 0, no limit.
func (c *Conn) SetReadTimeout(within time.Duration) {
	c.timeouts.Read, c.step = within, within
}

// SetMaxBody takes replies of at most n bytes of body from now on: 16
// MiB unless this says. A longer one loses the connection.
func (c *Conn) SetMaxBody(n uint32) { c.maxBody = n }

// needs fails before anything is sent if the capability name was not
// granted [CLIENT What a connection asks for].
func (c *Conn) needs(name string) error {
	if c.granted[name] {
		return nil
	}

	return broken("the server did not grant " + name)
}

// exchange sends Hello, then answers each Challenge until Ready, which
// the mechanism must accept, or an Error.
func (c *Conn) exchange(hello helloRequest, mechanism Mechanism) error {
	if err := c.send(hello); err != nil {
		return err
	}

	for {
		rep, err := c.receive()
		if err != nil {
			return err
		}

		done, err := c.loginStep(rep, mechanism)
		if done || err != nil {
			return err
		}
	}
}

// loginStep takes one reply of the login: true once Ready has come.
func (c *Conn) loginStep(rep reply, mechanism Mechanism) (bool, error) {
	switch r := rep.(type) {
	case challengeReply:
		answer, err := mechanism.Answer(r.challenge)
		if err != nil {
			return false, asFailure(mechanism, err)
		}

		return false, c.send(answerRequest{answer: answer})
	case readyReply:
		if verifier, ok := mechanism.(VerifyingMechanism); ok {
			if err := verifier.Verified(); err != nil {
				return false, asFailure(mechanism, err)
			}
		}

		c.ready(r)

		return true, nil
	case errorReply:
		if forgetter, ok := mechanism.(ForgettingMechanism); ok {
			forgetter.Refused()
		}

		return false, r.failure
	default:
		return false, c.lose("the server sent no Ready")
	}
}

// ready takes Ready's authorization, cancel key, capabilities granted and
// server; compression begins after it, never before [PROTOCOL Ready 'R'].
func (c *Conn) ready(r readyReply) {
	c.authorization, c.key, c.server = r.authorization, r.key, r.server
	c.granted = grantedOf(r.capabilities)
	c.compressing = c.granted[CapabilityCompression]
}

// asFailure gives a mechanism's error as the driver's: its own *Error as
// it is, any other as a connection failure caused by it (R-ERR-1).
func asFailure(mechanism Mechanism, err error) error {
	if _, ok := err.(*Error); ok { //nolint:errorlint // Only the driver's own failure is given as it is.
		return err
	}

	failure := broken(mechanism.Name() + " failed: " + err.Error())
	failure.cause = err

	return failure
}
