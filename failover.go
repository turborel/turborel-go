package turborel

// Connecting as Settings say, to one of several servers [CLIENT Several
// servers]: each host tried in the order given, or in an order chosen at
// random for each connection under LoadBalanceRandom, and each address a
// host's name resolves to in turn, every step within the connect timeout.
// A server reached, logged in to and of the role the settings ask is
// kept; another is left and the next tried. No server kept, the failure
// says why each was not.

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"net"
	"strconv"
	"strings"
)

// The generator that orders hosts at random: Knuth's MMIX linear
// congruential generator, as turborel-client's, the high bits of its
// state taken for each draw.
const (
	shuffleMultiplier uint64 = 6364136223846793005
	shuffleIncrement  uint64 = 1442695040888963407
	shuffleDrawShift         = 33
)

// shuffleSeed is a seed of the system's randomness.
func shuffleSeed() uint64 {
	var b [8]byte

	_, _ = rand.Read(b[:]) // crypto/rand's Read never fails.

	return binary.LittleEndian.Uint64(b[:])
}

// shuffle orders items as drawn from seed (Fisher and Yates, as
// Durstenfeld gives it): each order as likely, for a seed drawn at
// random.
func shuffle[T any](items []T, seed uint64) {
	state := seed

	for i := len(items) - 1; i >= 1; i-- {
		state = state*shuffleMultiplier + shuffleIncrement
		drawn := (state >> shuffleDrawShift) % len64(i+1)
		items[i], items[drawn] = items[drawn], items[i]
	}
}

// tried is why the server at an address was not kept.
type tried struct {
	at      string
	failure *Error
}

// Connect connects to the first server, of the hosts in the order the
// settings say, that can be reached, logs the user in and is of the role
// asked. It fails with the one server's failure, if there was one; else
// with SQLSTATE 08001, saying why each was not kept, of the last one's
// SQLSTATE where that was a refusal of the user (class 28); or with
// SQLSTATE 28000 before any connection, for a channel binding required
// by a mechanism that binds none.
func (s *Settings) Connect(ctx context.Context) (*Conn, error) {
	hosts := append([]Host(nil), s.Hosts...)
	if s.LoadBalance == LoadBalanceRandom {
		shuffle(hosts, shuffleSeed())
	}

	config, err := s.tlsConfig()
	if err != nil {
		return nil, err
	}

	var all []tried

	for _, host := range hosts {
		for _, t := range s.targets(ctx, host, config) {
			if t.failure != nil {
				all = append(all, tried{at: t.at, failure: t.failure})

				continue
			}

			c, err := s.attempt(ctx, host, t.target)
			if err == nil {
				return c, nil
			}

			all = append(all, tried{at: t.at, failure: asError(err)})
		}
	}

	return nil, noneKept(all)
}

// asError gives err as the driver's failure.
func asError(err error) *Error {
	if failure, ok := errors.AsType[*Error](err); ok {
		return failure
	}

	return &Error{SQLCode: sqlcodeConnection, SQLState: sqlstateConnectionFailure, Message: err.Error(), cause: err}
}

// noneKept is the failure of a connection none of all gave.
func noneKept(all []tried) *Error {
	if len(all) == 1 {
		return all[0].failure
	}

	state := sqlstateUnableToConnect
	if len(all) > 0 && strings.HasPrefix(all[len(all)-1].failure.SQLState, classAuthorization) {
		state = sqlstateInvalidAuthorization
	}

	reasons := make([]string, len(all))
	for i, t := range all {
		reasons[i] = t.at + ": " + t.failure.Message
	}

	message := "no server is named"
	if len(reasons) > 0 {
		message = "no server was kept: " + strings.Join(reasons, "; ")
	}

	return &Error{SQLCode: sqlcodeConnection, SQLState: state, Message: message}
}

// hostTarget is a target of a host, named; or why there is none.
type hostTarget struct {
	at      string
	target  Target
	failure *Error
}

// targets gives the targets of host, each named: its local socket, or
// each address its name resolves to, over TLS as config says, the
// server's certificate of the server name the settings give, else the
// host's.
func (s *Settings) targets(ctx context.Context, host Host, config *tls.Config) []hostTarget {
	if host.Path != "" {
		return []hostTarget{{at: host.Path, target: Local(host.Path)}}
	}

	port := strconv.Itoa(int(host.Port))

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host.Name)
	if err != nil {
		return []hostTarget{{at: host.Name + ":" + port, failure: ioFailed("resolve the name", err)}}
	}

	server := host.Name
	if s.TLS.ServerName != "" {
		server = s.TLS.ServerName
	}

	targets := make([]hostTarget, len(ips))

	for i, ip := range ips {
		address := net.JoinHostPort(ip.String(), port)

		target := TCP(address)
		if config != nil {
			target = TLS(address, server, config)
		}

		targets[i] = hostTarget{at: host.Name + " (" + address + ")", target: target}
	}

	return targets
}

// attempt connects to host over target, logged in and of the role asked;
// under TLSPrefer, over plain TCP if TLS cannot be begun.
func (s *Settings) attempt(ctx context.Context, host Host, target Target) (*Conn, error) {
	opened, channel, used, err := s.open(ctx, target)
	if err != nil {
		return nil, err
	}

	mechanism, err := s.proof(host, used)
	if err != nil {
		_ = opened.Close() // The proof's failure is the one given.

		return nil, err
	}

	if used.IsPlainTCP() {
		if err := inTheClear(mechanism); err != nil {
			_ = opened.Close() // The mechanism's refusal is the one given.

			return nil, err
		}
	}

	offer := DefaultOffer()
	if s.ApplicationName != "" {
		offer = offer.WithAttribute(AttributeApplication, s.ApplicationName)
	}

	config := &Config{Timeouts: Timeouts{Connect: s.ConnectTimeout}, Offer: &offer}

	c, err := connectOpened(ctx, used, opened, channel, login{user: s.User, mechanism: mechanism, config: config})
	if err != nil {
		return nil, err
	}

	if err := s.ofRole(ctx, c); err != nil {
		_ = c.Close() // A server not of the role asked is left.

		return nil, err
	}

	return c, nil
}

// open reaches target within the connect timeout: under TLSPrefer, a
// server reached whose TLS handshake fails is connected to again over
// plain TCP. It gives the stream, its TLS channel, and the target used.
func (s *Settings) open(ctx context.Context, target Target) (stream, *TLSChannel, Target, error) {
	if s.TLSMode != TLSPrefer || !target.IsTLS() {
		opened, channel, err := target.open(ctx, s.ConnectTimeout)

		return opened, channel, target, err
	}

	c, err := dialTCP(ctx, target.address, s.ConnectTimeout)
	if err != nil {
		return nil, nil, target, err
	}

	_ = c.SetDeadline(deadlineOf(s.ConnectTimeout)) // A TCP connection takes a deadline; the handshake is bounded by it.

	secured, err := secure(ctx, c, tlsServer{name: target.serverName, config: target.config})
	if err == nil {
		channel := channelOfState(secured.ConnectionState())

		return secured, &channel, target, nil
	}

	plain := TCP(target.address)
	opened, channel, err := plain.open(ctx, s.ConnectTimeout)

	return opened, channel, plain, err
}

// proof gives what proves the user at host over target: SCRAM of the
// password, if there is one; else the certificate named; else trust.
func (s *Settings) proof(host Host, target Target) (Mechanism, error) {
	var (
		mechanism Mechanism = Trust{}
		binds     bool
	)

	if password, ok := s.PasswordFor(host); ok {
		mechanism, binds = NewScram(password).WithChannelBinding(s.ChannelBinding).WithCache(s.KeyCache), true
	} else if s.TLS.Certificate != "" {
		mechanism = Certificate{}
	}

	if s.ChannelBinding == BindingRequire && (!binds || !target.IsTLS()) {
		return nil, &Error{
			SQLCode: sqlcodeConnection, SQLState: sqlstateInvalidAuthorization,
			Message: "channel binding is required: SCRAM over TLS alone binds one",
		}
	}

	return mechanism, nil
}

// ofRole refuses c if its server is not of the role the settings ask
// (SQLSTATE 08001).
func (s *Settings) ofRole(ctx context.Context, c *Conn) error {
	wanted := RoleReadWrite

	switch s.Target {
	case TargetAny:
		return nil
	case TargetReadOnly:
		wanted = RoleReadOnly
	case TargetReadWrite:
	}

	found, err := c.Role(ctx)
	if err != nil {
		return err
	}

	if found != wanted {
		return invalid("the server is " + found.String() + ", and " + wanted.String() + " is asked")
	}

	return nil
}

// tlsConfig gives the TLS the settings' mode makes: none of TLSDisable.
// Under TLSPrefer and TLSRequire the server's certificate is not checked,
// but the handshake proves it holds its key; under TLSVerifyCA it is
// checked against the authorities trusted, not its name; under
// TLSVerifyFull, both. Each presents the client's certificate if one is
// named.
func (s *Settings) tlsConfig() (*tls.Config, error) {
	config := &tls.Config{MinVersion: tls.VersionTLS12}

	switch s.TLSMode {
	case TLSDisable:
		return nil, nil //nolint:nilnil // No TLS is no configuration and no failure.
	case TLSPrefer, TLSRequire:
		// Prefer and require check no certificate, as turborel-client's.
		config.InsecureSkipVerify = true
	case TLSVerifyCA:
		roots, err := Roots(s.TLS.Roots, s.TLS.SystemRoots)
		if err != nil {
			return nil, err
		}

		// verify-ca checks the chain itself, below, not the name.
		config.InsecureSkipVerify = true
		config.VerifyConnection = func(state tls.ConnectionState) error { return verifyChain(state, roots) }
	case TLSVerifyFull:
		roots, err := Roots(s.TLS.Roots, s.TLS.SystemRoots)
		if err != nil {
			return nil, err
		}

		config.RootCAs = roots
	}

	if s.TLS.Certificate != "" {
		if s.TLS.Key == "" {
			return nil, broken("a client certificate is presented with its key, and none is named")
		}

		identity, err := tls.LoadX509KeyPair(s.TLS.Certificate, s.TLS.Key)
		if err != nil {
			return nil, broken("cannot read " + s.TLS.Certificate + ": " + err.Error())
		}

		config.Certificates = []tls.Certificate{identity}
	}

	return config, nil
}

// verifyChain checks the server's certificate against roots, whatever
// name it is of.
func verifyChain(state tls.ConnectionState, roots *x509.CertPool) error {
	if len(state.PeerCertificates) == 0 {
		return errNoCertificate
	}

	intermediates := x509.NewCertPool()
	for _, c := range state.PeerCertificates[1:] {
		intermediates.AddCert(c)
	}

	_, err := state.PeerCertificates[0].Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates})

	return err //nolint:wrapcheck // The handshake gives it, as crypto/tls gives its own.
}

// errNoCertificate is a server that presented no certificate.
var errNoCertificate = errors.New("the server presented no certificate")
