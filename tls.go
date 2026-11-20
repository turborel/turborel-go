package turborel

// TLS for a client [CLIENT Connecting], by crypto/tls. TLS begins on the
// server's own port, the handshake done before Hello, so a server the
// client does not trust is a connection failure (SQLSTATE 08006) that
// says why [PROTOCOL The Native Protocol]. The client trusts the
// authorities of the files it is given, and the platform's only when
// asked: those of the files SSL_CERT_FILE and SSL_CERT_DIR name, if
// either is set, else the platform's own store.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
)

// The environment's variables that name a store of authorities in place
// of the platform's, as turborel-client reads them.
const (
	envCertFile = "SSL_CERT_FILE"
	envCertDir  = "SSL_CERT_DIR"
)

// Trusting is a TLS configuration that trusts the authorities of the PEM
// file roots, alone.
func Trusting(roots string) (*tls.Config, error) {
	pool, err := Roots(roots, false)
	if err != nil {
		return nil, err
	}

	return &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}, nil
}

// Presenting is a TLS configuration that trusts the authorities of the
// PEM file roots, and presents the certificate chain and key of the PEM
// files chain and key, to log in by certificate.
func Presenting(roots, chain, key string) (*tls.Config, error) {
	config, err := Trusting(roots)
	if err != nil {
		return nil, err
	}

	pair, err := tls.LoadX509KeyPair(chain, key)
	if err != nil {
		return nil, broken("cannot present " + chain + ": " + err.Error())
	}

	config.Certificates = []tls.Certificate{pair}

	return config, nil
}

// Roots is the authorities trusted: those of the PEM file, if one is
// named, and the platform's too if platform. It fails if it trusts none.
func Roots(file string, platform bool) (*x509.CertPool, error) {
	pool := x509.NewCertPool()

	if platform {
		system, err := platformRoots()
		if err != nil {
			return nil, err
		}

		pool = system
	}

	if file != "" {
		pem, err := os.ReadFile(file) //nolint:gosec // G304: the file the program named to trust.
		if err != nil {
			return nil, broken("cannot read " + file + ": " + err.Error())
		}

		if !pool.AppendCertsFromPEM(pem) {
			return nil, broken(file + " holds no certificate")
		}
	}

	if !platform && file == "" {
		return nil, broken("no certificate authority is trusted: name a file of them, or the platform's")
	}

	return pool, nil
}

// platformRoots is the platform's authorities: those of SSL_CERT_FILE and
// SSL_CERT_DIR if either is set, else the system's store.
func platformRoots() (*x509.CertPool, error) {
	file, dir := os.Getenv(envCertFile), os.Getenv(envCertDir)
	if file == "" && dir == "" {
		pool, err := x509.SystemCertPool()
		if err != nil {
			return nil, broken("cannot trust the platform's certificate authorities: " + err.Error())
		}

		return pool, nil
	}

	pool := x509.NewCertPool()
	paths := []string{}

	if file != "" {
		paths = append(paths, file)
	}

	if dir != "" {
		for _, d := range filepath.SplitList(dir) {
			found, _ := filepath.Glob(filepath.Join(d, "*")) // A pattern of a directory and * is always well formed.
			paths = append(paths, found...)
		}
	}

	for _, path := range paths {
		if pem, err := os.ReadFile(path); err == nil { //nolint:gosec // G304: the stores the environment names.
			pool.AppendCertsFromPEM(pem)
		}
	}

	return pool, nil
}

// tlsServer is the server TLS begins with: the name its certificate is
// checked against, and how.
type tlsServer struct {
	name   string
	config *tls.Config
}

// secure begins TLS over conn, its handshake done within the
// connection's deadline already set, or until ctx is done.
func secure(ctx context.Context, conn net.Conn, server tlsServer) (*tls.Conn, error) {
	c := server.config.Clone()
	if c.ServerName == "" {
		c.ServerName = server.name
	}

	secured := tls.Client(conn, c)
	if err := secured.HandshakeContext(ctx); err != nil {
		_ = conn.Close() // The handshake's failure is the one given.

		return nil, broken("the TLS handshake failed: " + err.Error())
	}

	return secured, nil
}
