package turborel

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testCertificatePEM is a self-signed certificate, as PEM.
func testCertificatePEM(t *testing.T) []byte {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// writePEM writes text to name in a test's directory, giving its path.
func writePEM(t *testing.T, name string, text []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, text, 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

// TestRoots trusts a file's authorities, and refuses what trusts none.
//
// Verifies: [CLIENT Connecting] a client's TLS trusts the authorities of
// the file it is given; a file that cannot be read, or holds no
// certificate, or no authority named at all, is refused.
func TestRoots(t *testing.T) {
	t.Parallel()

	ca := writePEM(t, "ca.pem", testCertificatePEM(t))
	if _, err := Trusting(ca); err != nil {
		t.Errorf("Trusting: %v", err)
	}

	_, err := Trusting(filepath.Join(t.TempDir(), "none.pem"))
	assertBroken(t, err, "cannot read")

	_, err = Trusting(writePEM(t, "empty.pem", []byte("no certificate")))
	assertBroken(t, err, "holds no certificate")

	_, err = Roots("", false)
	assertBroken(t, err, "no certificate authority is trusted")

	_, err = Presenting(ca, ca, ca)
	assertBroken(t, err, "cannot present")

	_, err = Presenting(filepath.Join(t.TempDir(), "none.pem"), ca, ca)
	assertBroken(t, err, "cannot read")
}

// TestPlatformRoots trusts the stores the environment names in place of
// the platform's.
//
// Verifies: [CLIENT Connecting] the platform's authorities are those of
// the files SSL_CERT_FILE and SSL_CERT_DIR name, if either is set, else
// the system's store, and both a file's and the platform's together.
func TestPlatformRoots(t *testing.T) {
	certificate := testCertificatePEM(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "a.pem"), certificate, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv(envCertFile, writePEM(t, "file.pem", certificate))
	t.Setenv(envCertDir, dir)

	if _, err := Roots(writePEM(t, "ca.pem", certificate), true); err != nil {
		t.Errorf("Roots of both: %v", err)
	}

	t.Setenv(envCertFile, "")
	t.Setenv(envCertDir, "")

	if _, err := Roots("", true); err != nil {
		t.Errorf("Roots of the system's store: %v", err)
	}
}
