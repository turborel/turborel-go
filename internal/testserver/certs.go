package testserver

// The live tests' TLS certificates, made as the server starts so no
// secret is ever committed: an authority, a server certificate for
// localhost and 127.0.0.1, and a client certificate whose common name is
// the owner's.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// The certificates' lifetime and serial numbers.
const (
	validFor     = 24 * time.Hour
	serialCA     = 1
	serialServer = 2
	serialClient = 3
	// loopback is the address the server's certificate names.
	loopback = "127.0.0.1"
	// readable lets the server, in a container of another user, read
	// what the tests wrote.
	readable = 0o644
)

// issued is a certificate and its key.
type issued struct {
	cert *x509.Certificate
	der  []byte
	key  *ecdsa.PrivateKey
}

// issue makes a certificate of template, signed by parent, or by itself
// if parent is nil.
func issue(template *x509.Certificate, parent *issued) (*issued, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("a key: %w", err)
	}

	signer, signerKey := template, key
	if parent != nil {
		signer, signerKey = parent.cert, parent.key
	}

	der, err := x509.CreateCertificate(rand.Reader, template, signer, &key.PublicKey, signerKey)
	if err != nil {
		return nil, fmt.Errorf("a certificate: %w", err)
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("a certificate: %w", err)
	}

	return &issued{cert: cert, der: der, key: key}, nil
}

// template is a certificate of serial and common name, valid now.
func template(serial int64, name string) *x509.Certificate {
	now := time.Now()

	return &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(validFor),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
}

// writeCerts makes the authority, the server's and the client's
// certificates and keys in dir.
func writeCerts(dir, client string) (*Files, error) {
	caTemplate := template(serialCA, "turborel-go test authority")
	caTemplate.IsCA, caTemplate.BasicConstraintsValid = true, true
	caTemplate.KeyUsage |= x509.KeyUsageCertSign

	ca, err := issue(caTemplate, nil)
	if err != nil {
		return nil, err
	}

	serverTemplate := template(serialServer, "localhost")
	serverTemplate.DNSNames = []string{"localhost"}
	serverTemplate.IPAddresses = []net.IP{net.ParseIP(loopback)}
	serverTemplate.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}

	server, err := issue(serverTemplate, ca)
	if err != nil {
		return nil, err
	}

	clientTemplate := template(serialClient, client)
	clientTemplate.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}

	cl, err := issue(clientTemplate, ca)
	if err != nil {
		return nil, err
	}

	f := &Files{
		CA:         filepath.Join(dir, "ca.pem"),
		ServerCert: filepath.Join(dir, "server.pem"),
		ServerKey:  filepath.Join(dir, "server.key"),
		ClientCert: filepath.Join(dir, "client.pem"),
		ClientKey:  filepath.Join(dir, "client.key"),
	}

	return f, writeAll([]pemFile{
		{f.CA, ca, false}, {f.ServerCert, server, false}, {f.ServerKey, server, true},
		{f.ClientCert, cl, false}, {f.ClientKey, cl, true},
	})
}

// pemFile is a certificate, or its key, to write to path.
type pemFile struct {
	path  string
	of    *issued
	asKey bool
}

// writeAll writes each file as PEM.
func writeAll(files []pemFile) error {
	for _, f := range files {
		block := &pem.Block{Type: "CERTIFICATE", Bytes: f.of.der}

		if f.asKey {
			der, err := x509.MarshalPKCS8PrivateKey(f.of.key)
			if err != nil {
				return fmt.Errorf("a key: %w", err)
			}

			block = &pem.Block{Type: "PRIVATE KEY", Bytes: der}
		}

		if err := os.WriteFile(f.path, pem.EncodeToMemory(block), readable); err != nil {
			return fmt.Errorf("writing %s: %w", f.path, err)
		}
	}

	return nil
}
