package turborel

// The client's side of the mechanisms of authentication [PROTOCOL
// Mechanisms] [CLIENT Connecting]: what a connection proves its user by.
// trust, password and certificate are here; SCRAM is scram.go.

// The mechanisms' names, as the server knows them.
const (
	mechanismTrust       = "trust"
	mechanismPassword    = "password"
	mechanismCertificate = "certificate"
	mechanismScram       = "scram-sha-256"
	mechanismScramPlus   = "scram-sha-256-plus"
)

// Mechanism is a client's side of a mechanism of authentication: its
// name, the bytes Hello carries, and its answer to each Challenge. A
// mechanism may also implement [SecretMechanism], [VerifyingMechanism],
// [BindingMechanism] and [ForgettingMechanism].
type Mechanism interface {
	// Name is the mechanism's name, as the server knows it.
	Name() string
	// Initial gives the bytes Hello carries.
	Initial() []byte
	// Answer gives the answer to the server's challenge; an error gives
	// the connection up.
	Answer(challenge []byte) ([]byte, error)
}

// SecretMechanism is a mechanism whose bytes give away what proves the
// user, or let one who overhears them guess it at leisure: a connection
// over plain TCP refuses it before anything is sent.
type SecretMechanism interface {
	Secret() bool
}

// VerifyingMechanism is a mechanism by which the server proves itself
// too: Ready, as it arrives, is refused unless the server has.
type VerifyingMechanism interface {
	Verified() error
}

// BindingMechanism is a mechanism that binds itself to the TLS channel
// carrying it, told before Name and Initial are asked for.
type BindingMechanism interface {
	Bind(channel TLSChannel)
}

// ForgettingMechanism is a mechanism that forgets what it learnt of an
// exchange the server refused.
type ForgettingMechanism interface {
	Refused()
}

// TLSChannel is what a TLS connection's handshake gives a mechanism to
// bind itself to: the certificate the server presented, as DER, and the
// keying material `tls-exporter` exports, over TLS 1.3 alone.
type TLSChannel struct {
	Certificate []byte
	Exporter    []byte
}

// Trust asks the server to trust the user as it names itself, as a
// server does only of a client of its own machine, with
// SERVER.TRUST_LOCAL on.
type Trust struct{}

// Name is "trust".
func (Trust) Name() string { return mechanismTrust }

// Initial is nothing.
func (Trust) Initial() []byte { return nil }

// Answer refuses: trust has no answer to a challenge.
func (Trust) Answer([]byte) ([]byte, error) {
	return nil, broken("trust has no answer to a challenge")
}

// Password sends the password whole, so only over TLS, a Unix socket or
// a named pipe. Its String never shows the password.
type Password string

// Name is "password".
func (Password) Name() string { return mechanismPassword }

// Initial is the password's UTF-8.
func (p Password) Initial() []byte { return []byte(p) }

// Answer refuses: password has no answer to a challenge.
func (Password) Answer([]byte) ([]byte, error) {
	return nil, broken("password has no answer to a challenge")
}

// Secret is true: a password is never sent over plain TCP.
func (Password) Secret() bool { return true }

// String never shows the password.
func (Password) String() string { return "Password(..)" }

// GoString never shows the password.
func (Password) GoString() string { return "Password(..)" }

// Certificate proves the user by the certificate presented over TLS,
// whose common name names the user.
type Certificate struct{}

// Name is "certificate".
func (Certificate) Name() string { return mechanismCertificate }

// Initial is nothing.
func (Certificate) Initial() []byte { return nil }

// Answer refuses: certificate has no answer to a challenge.
func (Certificate) Answer([]byte) ([]byte, error) {
	return nil, broken("certificate has no answer to a challenge")
}

// isSecret says whether mechanism refuses plain TCP.
func isSecret(mechanism Mechanism) bool {
	secret, ok := mechanism.(SecretMechanism)

	return ok && secret.Secret()
}

// inTheClear refuses a secret mechanism over plain TCP, before a
// connection is made, as the server would refuse it after (SQLSTATE
// 28000) [CLIENT Connecting].
func inTheClear(mechanism Mechanism) error {
	if !isSecret(mechanism) {
		return nil
	}

	return &Error{
		SQLCode:  sqlcodeConnection,
		SQLState: sqlstateInvalidAuthorization,
		Message:  mechanism.Name() + " is sent only over TLS, a Unix socket or a named pipe, never over plain TCP",
	}
}
