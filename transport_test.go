package turborel

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestTargets names where a server is and how it is reached.
//
// Verifies: [PROTOCOL The Native Protocol] a server listens on TCP port
// 7422 unless told another, so an address of no port reaches 7422; a
// target says whether it is TLS or plain TCP, and never shows its
// configuration.
func TestTargets(t *testing.T) {
	t.Parallel()

	if got := TCP("db.example").address; got != "db.example:7422" {
		t.Errorf("TCP of no port: %s", got)
	}

	if got := TCP("[::1]:7500").address; got != "[::1]:7500" {
		t.Errorf("TCP of a port: %s", got)
	}

	tlsTarget := TLS("db.example", "db.example", nil)
	if !tlsTarget.IsTLS() || tlsTarget.IsPlainTCP() || !TCP("x").IsPlainTCP() || Local("/s").IsPlainTCP() {
		t.Error("IsTLS or IsPlainTCP misread")
	}

	got := []string{TCP("x:1").String(), Local("/s").String(), tlsTarget.String(), (Target{kind: 9}).String()}
	if !slices.Equal(got, []string{"tcp x:1", "local /s", "tls db.example:7422 (db.example)", "target"}) {
		t.Errorf("String() = %q", got)
	}
}

// TestTargetsRefused fails a target that cannot be reached.
//
// Verifies: [CLIENT Connecting] a server that cannot be reached, over
// TCP, a Unix socket or TLS, is a connection failure (SQLSTATE 08006);
// a TLS target needs a configuration.
func TestTargetsRefused(t *testing.T) {
	t.Parallel()

	listener, err := (&net.ListenConfig{}).Listen(context.Background(), networkTCP, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	address := listener.Addr().String()
	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}

			_ = c.Close()
		}
	}()

	t.Cleanup(func() { _ = listener.Close() })

	_, err = ConnectTo(context.Background(), TLS(address, "localhost", nil), "ANN", Trust{}, nil)
	assertFailure(t, err, sqlcodeMisuse, sqlstateMisuse, "needs a TLS configuration")

	config, err := Trusting(writePEM(t, "ca.pem", testCertificatePEM(t)))
	if err != nil {
		t.Fatal(err)
	}

	_, err = ConnectTo(context.Background(), TLS(address, "localhost", config), "ANN", Trust{}, nil)
	assertBroken(t, err, "TLS handshake failed")

	_, err = ConnectLocal(context.Background(), filepath.Join(t.TempDir(), "none"), "ANN", Trust{})
	assertBroken(t, err, "cannot connect")

	_ = listener.Close()

	_, err = Connect(context.Background(), address, "ANN", Trust{})
	assertBroken(t, err, "cannot connect")
}

// TestSecretOverPlainTCP refuses a secret mechanism over plain TCP before
// connecting.
//
// Verifies: [CLIENT Connecting] a password, or SCRAM, is refused over
// plain TCP before anything is sent (SQLSTATE 28000); SCRAM in the clear,
// asked for, is not.
func TestSecretOverPlainTCP(t *testing.T) {
	t.Parallel()

	for _, m := range []Mechanism{Password("p"), NewScram("p")} {
		_, err := Connect(context.Background(), "127.0.0.1:1", "ANN", m)
		assertState(t, err, sqlstateInvalidAuthorization)
	}

	_, err := Connect(context.Background(), "127.0.0.1:1", "ANN", NewScram("p").InTheClear())
	assertBroken(t, err, "cannot connect")

	if got := Password("secret").String() + Password("secret").GoString(); strings.Contains(got, "secret") {
		t.Errorf("a password shows itself: %q", got)
	}
}

// TestMechanismsHaveNoAnswer refuses a challenge to a mechanism with no
// exchange.
//
// Verifies: [PROTOCOL Mechanisms] trust, password and certificate are
// accepted or refused at once, so a Challenge to them is answered by a
// connection failure; password sends its UTF-8 and is secret.
func TestMechanismsHaveNoAnswer(t *testing.T) {
	t.Parallel()

	for _, m := range []Mechanism{Trust{}, Password("p"), Certificate{}} {
		if _, err := m.Answer([]byte("c")); err == nil {
			t.Errorf("%s answered a challenge", m.Name())
		}
	}

	if string(Password("pé").Initial()) != "pé" || (Trust{}).Initial() != nil || (Certificate{}).Initial() != nil {
		t.Error("first bytes misread")
	}

	if !isSecret(Password("p")) || isSecret(Trust{}) || isSecret(Certificate{}) {
		t.Error("secret misread")
	}
}

// pipeStream is a stream of two pipes, with no deadline.
type pipeStream struct {
	io.Reader
	io.Writer

	closed bool
}

func (p *pipeStream) Close() error {
	p.closed = true

	return nil
}

// TestOpenWithoutDeadlines logs in over a stream that takes no deadline.
//
// Verifies: [CLIENT Timeouts] a connection over a stream the program
// made waits as its maker set the stream to; the stream needs no
// deadlines.
func TestOpenWithoutDeadlines(t *testing.T) {
	t.Parallel()

	var w writer

	w.frame(kindReady, func() {
		w.str("ANN")
		w.buf = append(w.buf, make([]byte, keyBytes)...)
		w.strs(nil)
		w.str("turborel 0.0.0 (alpha)")
	})

	var sent bytes.Buffer

	s := &pipeStream{Reader: bytes.NewReader(w.buf), Writer: &sent}

	c, err := Open(context.Background(), s, "ANN", Trust{}, nil)
	if err != nil || sent.Len() == 0 || sent.Bytes()[0] != kindHello {
		t.Fatalf("Open: %v, sent % X", err, sent.Bytes())
	}

	if _, ok := c.Target(); ok || deadlineless(s).SetDeadline(time.Now()) != nil {
		t.Error("a program's stream has a target, or refused a deadline")
	}

	_ = c.Close()

	if !s.closed || !c.IsLost() {
		t.Error("Close did not close the stream")
	}
}

// TestCancelSendsKey cancels by the key over a connection of its own.
//
// Verifies: [PROTOCOL Cancel 'Z'] Cancel is the key Ready gave, the first
// and only message of a connection of its own, which the server closes
// with no reply; a cancel gives up once its limit passes, 10 seconds
// unless the program says [CLIENT Running statements]; the key is kept
// as the client's secret, a canceller never showing it [PROTOCOL Ready
// 'R'].
func TestCancelSendsKey(t *testing.T) {
	t.Parallel()

	listener, err := (&net.ListenConfig{}).Listen(context.Background(), networkTCP, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = listener.Close() })

	got := make(chan []byte, 1)
	go func() {
		c, err := listener.Accept()
		if err != nil {
			return
		}

		b, _ := io.ReadAll(io.LimitReader(c, frameHeaderBytes+keyBytes))
		got <- b

		_ = c.Close()
	}()

	key := [keyBytes]byte{1, 15: 9}
	k := &Canceller{target: TCP(listener.Addr().String()), key: key}

	if err := k.Cancel(context.Background()); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if b := <-got; !bytes.Equal(b, encodeRequest(cancelRequest{key: key})) {
		t.Errorf("sent % X", b)
	}

	held := make(chan net.Conn, 1)
	go func() {
		c, _ := listener.Accept() // Accepts, holds and never answers.
		held <- c
	}()

	assertBroken(t, k.CancelWithin(context.Background(), 30*time.Millisecond), "did not answer in time")

	if c := <-held; c != nil {
		_ = c.Close()
	}

	if strings.Contains(k.String()+k.GoString(), "\x01") {
		t.Error("a canceller shows its key")
	}
}

// TestOffers asks for what a connection asks for.
//
// Verifies: [CLIENT What a connection asks for] by default every
// capability but compression, in the protocol's order; an offer of none;
// one of compression added once; connection attributes added. A
// connection waits 30 seconds to connect and log in, and without limit
// after, by default [CLIENT Timeouts].
func TestOffers(t *testing.T) {
	t.Parallel()

	all := Capabilities()
	if len(all) != 18 || all[0] != CapabilityNotices || all[17] != CapabilityConditions {
		t.Errorf("capabilities %q", all)
	}

	d := DefaultOffer()
	if slices.Contains(d.Capabilities, CapabilityCompression) || len(d.Capabilities) != 17 || d.CompressFrom != 1024 {
		t.Errorf("default offer %+v", d)
	}

	z := d.Compressed().Compressed()
	if n := len(z.Capabilities); n != 18 || len(NoOffer().Capabilities) != 0 {
		t.Errorf("compressed offer of %d", n)
	}

	a := d.WithAttribute(AttributeApplication, "x")
	if len(a.Attributes) != 1 || len(d.Attributes) != 0 {
		t.Error("an attribute changed the offer it was added to")
	}

	if DefaultTimeouts().Connect != 30*time.Second || DefaultTimeouts().Read != 0 || DefaultConfig().Offer == nil {
		t.Error("the defaults are not turborel-client's")
	}
}

// TestIOFailed says a wait that ran out was the server not answering in
// time.
//
// Verifies: [CLIENT Timeouts] a wait that runs out loses the connection,
// saying the server did not answer in time; another failure says what it
// was.
func TestIOFailed(t *testing.T) {
	t.Parallel()

	assertBroken(t, ioFailed(verbReceive, os.ErrDeadlineExceeded), "did not answer in time")
	assertBroken(t, ioFailed(verbReceive, io.ErrUnexpectedEOF), "cannot receive: unexpected EOF")

	if !deadlineOf(0).IsZero() || deadlineOf(time.Second).IsZero() {
		t.Error("deadlineOf misread")
	}
}
