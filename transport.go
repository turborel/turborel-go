package turborel

// Where a connection reaches its server [PROTOCOL The Native Protocol]
// [CLIENT Connecting]: TCP, on port 7422 unless told another; a Unix
// socket, or on Windows a named pipe, as `\\.\pipe\turborel`; or TLS,
// begun on the server's TCP port.

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"strconv"
	"time"
)

// DefaultPort is the TCP port a server listens on, and a client connects
// to, unless told another.
const DefaultPort = 7422

// The networks Go's dialer names.
const (
	networkTCP  = "tcp"
	networkUnix = "unix"
)

// stream is what a connection reads and writes: a TCP or TLS connection,
// a Unix socket, or a named pipe, whose deadlines bound its waits where
// it has them.
type stream interface {
	io.ReadWriteCloser
	SetDeadline(t time.Time) error
}

// targetKind says how a target is reached.
type targetKind uint8

const (
	targetTCP targetKind = iota
	targetLocal
	targetTLS
)

// Target is where a server is, and how it is reached: by TCP, by a local
// socket or pipe, or by TLS.
type Target struct {
	kind       targetKind
	address    string
	path       string
	serverName string
	config     *tls.Config
}

// TCP is the server at address, host and port; with no port, 7422.
func TCP(address string) Target {
	return Target{kind: targetTCP, address: withPort(address)}
}

// Local is the server at the Unix socket path, or on Windows the named
// pipe path, as `\\.\pipe\turborel`.
func Local(path string) Target {
	return Target{kind: targetLocal, path: path}
}

// TLS is the server at address, reached over TLS as config says, its
// certificate checked against serverName.
func TLS(address, serverName string, config *tls.Config) Target {
	return Target{kind: targetTLS, address: withPort(address), serverName: serverName, config: config}
}

// withPort is address, given the default port if it names none.
func withPort(address string) string {
	if _, _, err := net.SplitHostPort(address); err == nil {
		return address
	}

	return net.JoinHostPort(address, strconv.Itoa(DefaultPort))
}

// String says where the target is and how it is reached, never its TLS
// configuration's secrets.
func (t Target) String() string {
	switch t.kind {
	case targetTCP:
		return "tcp " + t.address
	case targetLocal:
		return "local " + t.path
	case targetTLS:
		return "tls " + t.address + " (" + t.serverName + ")"
	default:
		return "target"
	}
}

// IsTLS says whether the target is reached over TLS.
func (t Target) IsTLS() bool { return t.kind == targetTLS }

// IsPlainTCP says whether the target is reached over TCP without TLS,
// where a secret mechanism is refused.
func (t Target) IsPlainTCP() bool { return t.kind == targetTCP }

// open reaches the target within the connect timeout: the stream, and
// the TLS channel a mechanism may bind to.
func (t Target) open(ctx context.Context, within time.Duration) (stream, *TLSChannel, error) {
	switch t.kind {
	case targetLocal:
		s, err := openLocal(ctx, t.path, within)

		return s, nil, err
	case targetTLS:
		return t.openTLS(ctx, within)
	case targetTCP:
		c, err := dialTCP(ctx, t.address, within)

		return c, nil, err
	default:
		return nil, nil, broken("a target of no kind")
	}
}

// openTLS reaches the target by TCP, then begins TLS within the connect
// timeout.
func (t Target) openTLS(ctx context.Context, within time.Duration) (stream, *TLSChannel, error) {
	c, err := dialTCP(ctx, t.address, within)
	if err != nil {
		return nil, nil, err
	}

	if t.config == nil {
		_ = c.Close() // The missing configuration is the failure given.

		return nil, nil, broken("a TLS target needs a TLS configuration")
	}

	_ = c.SetDeadline(deadlineOf(within)) // A TCP connection takes a deadline; the handshake is bounded by it.

	secured, err := secure(ctx, c, tlsServer{name: t.serverName, config: t.config})
	if err != nil {
		return nil, nil, err
	}

	channel := channelOfState(secured.ConnectionState())

	return secured, &channel, nil
}

// dialTCP connects to address within the timeout.
func dialTCP(ctx context.Context, address string, within time.Duration) (net.Conn, error) {
	dialer := net.Dialer{Timeout: within}

	c, err := dialer.DialContext(ctx, networkTCP, address)
	if err != nil {
		return nil, ioFailed(verbConnect, err)
	}

	return c, nil
}
