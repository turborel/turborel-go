# Connections

[Contents](README.md)

## NAME

`Conn`: a connection to a server, a session of the server's.

## SYNOPSIS

```go
func Connect(ctx context.Context, address, user string, mechanism Mechanism) (*Conn, error)
func ConnectLocal(ctx context.Context, path, user string, mechanism Mechanism) (*Conn, error)
func ConnectTo(ctx context.Context, target Target, user string, mechanism Mechanism, config *Config) (*Conn, error)
func Open(ctx context.Context, s io.ReadWriteCloser, user string, mechanism Mechanism, config *Config) (*Conn, error)

func TCP(address string) Target
func Local(path string) Target
func TLS(address, serverName string, config *tls.Config) Target

type Config struct {
	Timeouts Timeouts
	Offer    *Offer
}
type Timeouts struct{ Connect, Read time.Duration }
type Offer struct {
	Capabilities []string
	Attributes   []Attribute
	CompressFrom int
}
func DefaultConfig() *Config
func DefaultTimeouts() Timeouts
func DefaultOffer() Offer
func NoOffer() Offer
func (o Offer) Compressed() Offer
func (o Offer) WithAttribute(name, value string) Offer

type Mechanism interface {
	Name() string
	Initial() []byte
	Answer(challenge []byte) ([]byte, error)
}
type Trust struct{}
type Password string
type Certificate struct{}
func NewScram(password string) *Scram
func (s *Scram) InTheClear() *Scram
func (s *Scram) WithChannelBinding(binding ChannelBinding) *Scram
func (s *Scram) WithCache(cache *KeyCache) *Scram
func NewKeyCache() *KeyCache
func ChannelOf(conn *tls.Conn) TLSChannel

func Trusting(roots string) (*tls.Config, error)
func Presenting(roots, chain, key string) (*tls.Config, error)
func Roots(file string, platform bool) (*x509.CertPool, error)

func (c *Conn) Authorization() string
func (c *Conn) Server() string
func (c *Conn) Granted(name string) bool
func (c *Conn) GrantedCapabilities() []string
func (c *Conn) Setting(name string) (string, bool)
func (c *Conn) Settings() map[string]string
func (c *Conn) InTransaction() bool
func (c *Conn) SetReadTimeout(within time.Duration)
func (c *Conn) SetMaxBody(n uint32)
func (c *Conn) Ping(ctx context.Context) error
func (c *Conn) Canceller() (*Canceller, bool)
func (k *Canceller) Cancel(ctx context.Context) error
func (k *Canceller) CancelWithin(ctx context.Context, within time.Duration) error
func (c *Conn) Terminate() error
func (c *Conn) Close() error
```

## DESCRIPTION

A connection is made to a `Target` as a user, proved by a `Mechanism`,
and is then a session of the server's: Hello, the mechanism's exchange,
then Ready, which names the session's authorization identifier, the
capabilities granted and the server's product. `Connect` and
`ConnectLocal` make one as `DefaultConfig` says; `ConnectTo` takes a
`Config` of its own, and `Open` a stream the program made, whose privacy
and binding the program answers for.

Over TLS, a `BindingMechanism`, as `Scram`, is bound to the connection
before its first bytes: by `tls-exporter` over TLS 1.3, by the server's
certificate over TLS 1.2. A program opening a TLS stream of its own
binds it by `ChannelOf` first. A `KeyCache` given to `Scram` keeps the
keys a password derived, at most 64, so a later login of the same salt
and iterations skips the derivation; it finds an entry by a tag of the
password under a secret of its own, so it holds no password.

`Setting` gives a session setting as the server last told it, with no
round trip, and the server's status: AUTHORIZATION, SCHEMA, READ_ONLY,
SERVER_VERSION, SERVER_READ_ONLY, CATALOG and OWNER. `Ping` asks whether
the server and the connection are alive, with `ping` granted. A context
done while a call waits gives the call up, the connection lost.

`Terminate` ends the session as SESSION.COMMIT_AT_END says; `Close` closes
at once. A `Canceller` cancels the statement the connection runs, from
another goroutine. Neither a `Password`, a `Scram` nor a `Canceller`
shows its secret in its string.

## ERRORS

| SQLSTATE | When |
|---|---|
| 28000 | A password or SCRAM over plain TCP, refused before connecting; credentials the server refused, as a wrong password; the server's SCRAM signature false |
| 08004 | The server refused the connection |
| 08006 | SQLCODE -919: the server could not be reached, the TLS handshake failed, a wait ran out, a reply was malformed or too long, a capability not granted was needed, or the connection was lost before |
| 0A000 | SQLCODE -900: an offer of more than 32 attributes, or of one whose name is not 1 to 64 bytes or whose value is longer than 1024 |
| TB910 | SQLCODE -910: a TLS target with no TLS configuration, or a target of no kind |
| 40002 | The commit Terminate made failed, as of a deferred constraint not satisfied |
