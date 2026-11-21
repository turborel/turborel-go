package turborel

import (
	"bufio"
	"context"
	"net"
	"testing"
	"time"
)

// fake is the server's end of a connection a test scripts.
type fake struct {
	t    *testing.T
	conn net.Conn
	r    *bufio.Reader
}

// request reads the client's next request: its kind and body.
func (f *fake) request() (byte, []byte) {
	f.t.Helper()

	kind, body, err := readFrame(f.r, maxBody, false)
	if err != nil {
		f.t.Errorf("the fake server read: %v", err)
	}

	return kind, body
}

// requestCompressed reads the next request, inflated if compressed.
func (f *fake) requestCompressed() (byte, []byte, bool) {
	f.t.Helper()

	peek, err := f.r.Peek(1)
	compressed := err == nil && peek[0] == kindCompressed

	kind, body, err := readFrame(f.r, maxBody, true)
	if err != nil {
		f.t.Errorf("the fake server read: %v", err)
	}

	return kind, body, compressed
}

// reply writes a reply of kind, its body written by write.
func (f *fake) reply(kind byte, write func(w *writer)) {
	f.t.Helper()

	var w writer

	w.frame(kind, func() {
		if write != nil {
			write(&w)
		}
	})

	if _, err := f.conn.Write(w.buf); err != nil {
		f.t.Errorf("the fake server wrote: %v", err)
	}
}

// ready replies Ready, granting capabilities.
func (f *fake) ready(capabilities ...string) {
	f.reply(kindReady, func(w *writer) {
		w.str("ANN")
		w.buf = append(w.buf, make([]byte, keyBytes)...)
		w.strs(capabilities)
		w.str("turborel 0.0.0 (alpha)")
	})
}

// failure replies Error of sqlcode and sqlstate.
func (f *fake) failure(sqlcode int32, sqlstate string) {
	f.reply(kindError, func(w *writer) { w.failure(&Error{SQLCode: sqlcode, SQLState: sqlstate, Message: "fake"}) })
}

// serve runs script as the server's end of a pipe, giving the client's
// end; the test waits for the script to end.
func serve(t *testing.T, script func(f *fake)) net.Conn {
	t.Helper()

	client, server := net.Pipe()
	done := make(chan struct{})

	go func() {
		defer close(done)
		defer func() { _ = server.Close() }()

		script(&fake{t: t, conn: server, r: bufio.NewReader(server)})
	}()

	t.Cleanup(func() {
		_ = client.Close()
		<-done
	})

	return client
}

// openFake opens a connection to a scripted server, logging in by
// mechanism as config says.
func openFake(t *testing.T, mechanism Mechanism, config *Config, script func(f *fake)) (*Conn, error) {
	t.Helper()

	return Open(context.Background(), serve(t, script), "ANN", mechanism, config)
}

// fakeWait bounds every wait of a test of a scripted server, so a test
// that hangs ends; it is long, as a slow runner may take seconds to pass
// a large frame through a pipe, and a test that passes never waits it out.
const fakeWait = 30 * time.Second

// fakeConfig is the configuration of a connection to a scripted server:
// offer, and every wait bounded by fakeWait.
func fakeConfig(offer Offer) *Config {
	return &Config{Timeouts: Timeouts{Connect: fakeWait, Read: fakeWait}, Offer: &offer}
}
