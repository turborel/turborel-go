package turborel

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/turborel/turborel-go/internal/testserver"
)

// live is the server the live tests run against, nil if none is named.
var live *testserver.Server //nolint:gochecknoglobals // The one server TestMain starts for the live tests.

// TestMain starts the live tests' server where the environment names
// one, and stops it once every test has run.
func TestMain(m *testing.M) {
	if spec, ok := testserver.Spec(); ok {
		server, err := testserver.Start(spec)
		if err != nil {
			fmt.Fprintln(os.Stderr, "live server:", err)
			os.Exit(1) //nolint:forbidigo // TestMain ends the test binary when its server cannot start.
		}

		live = server
	}

	code := m.Run()

	if live != nil {
		_ = live.Stop() // The tests' result is what matters.
	}

	os.Exit(code) //nolint:forbidigo // TestMain gives the tests' result as the binary's.
}

// liveServer gives the live server, skipping the test where none runs.
func liveServer(t *testing.T) *testserver.Server {
	t.Helper()

	if live == nil {
		t.Skip("no live server: set " + testserver.EnvServer)
	}

	return live
}

// liveConnect connects to target, failing the test if it cannot, and
// closes the connection as the test ends.
func liveConnect(t *testing.T, target Target, mechanism Mechanism, config *Config) *Conn {
	t.Helper()

	c, err := ConnectTo(context.Background(), target, testserver.Owner, mechanism, config)
	if err != nil {
		t.Fatalf("connecting to %v by %s: %v", target, mechanism.Name(), err)
	}

	t.Cleanup(func() { _ = c.Close() })

	return c
}

// liveTLS is a TLS target of the live server, trusting its authority, of
// TLS version at most most; 0, any.
func liveTLS(t *testing.T, most uint16, present bool) Target {
	t.Helper()

	s := liveServer(t)

	config, err := Trusting(s.Files.CA)
	if present {
		config, err = Presenting(s.Files.CA, s.Files.ClientCert, s.Files.ClientKey)
	}

	if err != nil {
		t.Fatal(err)
	}

	config.MaxVersion = most

	return TLS(s.Address, "localhost", config)
}

// liveQuery runs sql, giving its rows, or its failure.
func liveQuery(t *testing.T, c *Conn, sql string) ([]Row, error) {
	t.Helper()

	if err := c.send(queryRequest{sql: sql}); err != nil {
		return nil, err
	}

	var rows []Row

	for {
		rep, err := c.receive()
		if err != nil {
			return nil, err
		}

		switch r := rep.(type) {
		case rowsReply:
			rows = append(rows, r.rows...)
		case completeReply:
			return rows, nil
		case errorReply:
			return nil, r.failure
		}
	}
}

// liveCompletion runs sql, giving its Complete.
func liveCompletion(t *testing.T, c *Conn, sql string) Completion {
	t.Helper()

	if err := c.send(queryRequest{sql: sql}); err != nil {
		t.Fatal(err)
	}

	for {
		rep, err := c.receive()
		if err != nil {
			t.Fatal(err)
		}

		if done, ok := rep.(completeReply); ok {
			return done.completion
		}
	}
}

// mustQuery runs sql, failing the test if it fails.
func mustQuery(t *testing.T, c *Conn, sql string) []Row {
	t.Helper()

	rows, err := liveQuery(t, c, sql)
	if err != nil {
		t.Fatalf("%s: %v", sql, err)
	}

	return rows
}

// assertState checks that err is an *Error of sqlstate.
func assertState(t *testing.T, err error, sqlstate string) {
	t.Helper()

	var failure *Error
	if !errors.As(err, &failure) || failure.SQLState != sqlstate {
		t.Errorf("err = %v, want SQLSTATE %s", err, sqlstate)
	}
}

// TestLiveTrust logs in by trust over TCP, the Unix socket and TLS.
//
// Verifies: [PROTOCOL The Native Protocol] a client connects over TCP on
// the server's port, over its Unix socket, and over TLS begun on the
// same port, there logging in by trust, accepted at once of a client of
// the server's own machine with SERVER.TRUST_LOCAL on [PROTOCOL
// Mechanisms]; Ready gives the authorization and the server's product
// [CLIENT Connecting].
func TestLiveTrust(t *testing.T) {
	t.Parallel()

	s := liveServer(t)
	targets := []Target{TCP(s.Address), liveTLS(t, 0, false)}

	if s.Socket != "" {
		targets = append(targets, Local(s.Socket))
	}

	for _, target := range targets {
		c := liveConnect(t, target, Trust{}, nil)
		if c.Authorization() != testserver.Owner || c.Server() != "turborel 0.0.0 (alpha)" {
			t.Errorf("%v: authorization %q, server %q", target, c.Authorization(), c.Server())
		}

		if err := c.Terminate(); err != nil {
			t.Errorf("%v: Terminate: %v", target, err)
		}
	}
}

// TestLivePassword sends a password whole over TLS and the socket, and
// never over plain TCP.
//
// Verifies: [CLIENT Connecting] a password is sent whole only over TLS, a
// Unix socket or a named pipe; over plain TCP it is refused before a
// connection is made (SQLSTATE 28000); a wrong one is refused by the
// server (SQLSTATE 28000).
func TestLivePassword(t *testing.T) {
	t.Parallel()

	s := liveServer(t)
	liveConnect(t, liveTLS(t, 0, false), Password(testserver.OwnerPassword), nil)

	if s.Socket != "" {
		liveConnect(t, Local(s.Socket), Password(testserver.OwnerPassword), nil)
	}

	_, err := Connect(context.Background(), s.Address, testserver.Owner, Password(testserver.OwnerPassword))
	assertState(t, err, sqlstateInvalidAuthorization)

	_, err = ConnectTo(context.Background(), liveTLS(t, 0, false), testserver.Owner, Password("wrong"), nil)
	assertState(t, err, sqlstateInvalidAuthorization)
}

// TestLiveScramBound proves a password by SCRAM-SHA-256-PLUS over TLS 1.3
// and TLS 1.2.
//
// Verifies: [PROTOCOL Mechanisms] scram-sha-256-plus is bound by
// tls-exporter over TLS 1.3 and by tls-server-end-point over TLS 1.2, as
// its GS2 header names; the server's signature is checked before the
// last, empty Answer [RFC 7677 §3] [CLIENT Connecting].
func TestLiveScramBound(t *testing.T) {
	t.Parallel()

	for _, most := range []uint16{tls.VersionTLS13, tls.VersionTLS12} {
		scram := NewScram(testserver.OwnerPassword)
		liveConnect(t, liveTLS(t, most, false), scram, nil)

		want := channelExporter
		if most == tls.VersionTLS12 {
			want = channelEndPoint
		}

		if scram.Name() != mechanismScramPlus || scram.channel.kind != want || !scram.verified {
			t.Errorf("TLS %x: %s, channel %d, verified %v", most, scram.Name(), scram.channel.kind, scram.verified)
		}
	}
}

// TestLiveScramLocal proves a password by SCRAM-SHA-256 over the Unix
// socket, unbound, and refuses it over plain TCP.
//
// Verifies: [CLIENT Connecting] SCRAM-SHA-256 is used over a Unix socket,
// and over plain TCP only when the program asks for it in the clear: else
// refused before connecting (SQLSTATE 28000); a wrong password is refused
// (SQLSTATE 28000).
func TestLiveScramLocal(t *testing.T) {
	t.Parallel()

	s := liveServer(t)
	if s.Socket != "" {
		scram := NewScram(testserver.OwnerPassword)
		liveConnect(t, Local(s.Socket), scram, nil)

		if scram.Name() != mechanismScram {
			t.Errorf("over the socket: %s", scram.Name())
		}

		_, err := ConnectLocal(context.Background(), s.Socket, testserver.Owner, NewScram("wrong"))
		assertState(t, err, sqlstateInvalidAuthorization)
	}

	_, err := Connect(context.Background(), s.Address, testserver.Owner, NewScram(testserver.OwnerPassword))
	assertState(t, err, sqlstateInvalidAuthorization)
}

// TestLiveScramInTheClear proves SCRAM over plain TCP where the server
// takes it.
//
// Verifies: [CLIENT Connecting] SCRAM in the clear, asked for by the
// program, is refused by a server whose SERVER.SCRAM_WITHOUT_TLS is off,
// and taken by one where it is on.
func TestLiveScramInTheClear(t *testing.T) {
	t.Parallel()

	s := liveServer(t)
	owner := liveConnect(t, liveTLS(t, 0, false), Trust{}, nil)

	_, err := Connect(context.Background(), s.Address, testserver.Owner, NewScram(testserver.OwnerPassword).InTheClear())
	if err == nil {
		t.Error("SCRAM in the clear was taken with SCRAM_WITHOUT_TLS off")
	}

	mustQuery(t, owner, "SET DATABASE SERVER.SCRAM_WITHOUT_TLS = 'ON'")
	defer mustQuery(t, owner, "SET DATABASE SERVER.SCRAM_WITHOUT_TLS = 'OFF'")

	liveConnect(t, TCP(s.Address), NewScram(testserver.OwnerPassword).InTheClear(), nil)
}

// TestLiveCertificate logs in by the certificate presented over TLS.
//
// Verifies: [PROTOCOL Mechanisms] certificate is accepted where the TLS
// client certificate's common name is the user, the client trusting a
// PEM file of authorities and presenting a chain and key of two more.
func TestLiveCertificate(t *testing.T) {
	t.Parallel()

	c := liveConnect(t, liveTLS(t, 0, true), Certificate{}, nil)
	if c.Authorization() != testserver.Owner {
		t.Errorf("authorization %q", c.Authorization())
	}
}

// TestLiveKeyCache skips the derivation of a second login.
//
// Verifies: [CLIENT The SCRAM key cache] the keys a password derived are
// kept after a login the server proved, so a later login of the same salt
// and iterations finds them; a cache given to a mechanism counts its hits
// and misses.
func TestLiveKeyCache(t *testing.T) {
	t.Parallel()

	cache := NewKeyCache()
	for range 2 {
		liveConnect(t, liveTLS(t, 0, false), NewScram(testserver.OwnerPassword).WithCache(cache), nil)
	}

	if hits, misses := cache.Lookups(); hits != 1 || misses != 1 || cache.Len() != 1 {
		t.Errorf("hits %d, misses %d, held %d", hits, misses, cache.Len())
	}
}

// TestLiveStatus reads the session's status as the server tells it.
//
// Verifies: [PROTOCOL Status 'T'] with status the server tells, as the
// session begins, AUTHORIZATION, SCHEMA, READ_ONLY, SERVER_VERSION,
// SERVER_READ_ONLY, CATALOG, the catalog's name and never empty, and
// OWNER (gap G2) and every setting a
// session may set, SESSION.TIME_ZONE and TRANSACTION.ISOLATION among
// them; and after a statement each setting it changed, each given with
// no round trip [CLIENT What the server tells]. The capability status
// grants them [PROTOCOL Capabilities].
func TestLiveStatus(t *testing.T) {
	t.Parallel()

	c := liveConnect(t, liveTLS(t, 0, false), Trust{}, nil)
	want := map[string]string{
		StatusAuthorization: testserver.Owner, StatusSchema: testserver.Owner, StatusReadOnly: "NO",
		StatusServerVersion: "turborel 0.0.0 (alpha)", StatusServerReadOnly: "NO", StatusCatalog: "TURBOREL",
		StatusOwner: "YES", "TRANSACTION.ISOLATION": "READ COMMITTED", "SESSION.TIME_ZONE": "+00:00",
	}

	for name, value := range want {
		if got, ok := c.Setting(name); !ok || got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}

	mustQuery(t, c, "SET TIME ZONE INTERVAL '+02:00' HOUR TO MINUTE")

	if zone, _ := c.Setting("SESSION.TIME_ZONE"); zone != "+02:00" || len(c.Settings()) < len(want) {
		t.Errorf("SESSION.TIME_ZONE = %q after SET", zone)
	}
}

// TestLiveOffers asks for every capability but compression, for none, and
// for compression.
//
// Verifies: [CLIENT What a connection asks for] by default every
// capability but compression is asked for and granted, in the protocol's
// order; an offer of none sees the first form, no status told, and a
// method needing a capability not granted fails before sending; an offer
// of compression has it granted, Ready read uncompressed and replies
// after it inflated [PROTOCOL Capabilities] [PROTOCOL Opening a
// connection]; a client that asks for none sees the protocol's first
// form [PROTOCOL The Native Protocol].
func TestLiveOffers(t *testing.T) {
	t.Parallel()

	c := liveConnect(t, liveTLS(t, 0, false), Trust{}, nil)
	if got := c.GrantedCapabilities(); !slices.Equal(got, DefaultOffer().Capabilities) {
		t.Errorf("granted %q", got)
	}

	none := NoOffer()

	plain := liveConnect(t, liveTLS(t, 0, false), Trust{}, &Config{Offer: &none})
	if len(plain.GrantedCapabilities()) != 0 || len(plain.Settings()) != 0 {
		t.Errorf("an offer of none was granted %q, told %d settings", plain.GrantedCapabilities(), len(plain.Settings()))
	}

	if err := plain.Ping(context.Background()); err == nil {
		t.Error("Ping went without ping granted")
	}

	if done := liveCompletion(t, plain, "VALUES 1"); done.Counted || done.Time != nil || done.Committed {
		t.Errorf("a first-form Complete had a tail: %+v", done)
	}

	compressed := DefaultOffer().Compressed()
	z := liveConnect(t, liveTLS(t, 0, false), Trust{}, &Config{Offer: &compressed})

	long := repeat("x", 5000)

	rows := mustQuery(t, z, "VALUES '"+long+"'")
	if !z.Granted(CapabilityCompression) || len(rows) != 1 || rows[0][0] != Char(long) {
		t.Errorf("compression granted %v, rows %d", z.Granted(CapabilityCompression), len(rows))
	}
}

// repeat is s n times.
func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for range n {
		out = append(out, s...)
	}

	return string(out)
}

// TestLiveAttributes sends connection attributes in Hello.
//
// Verifies: [PROTOCOL Hello 'H'] connection attributes in Hello's tail,
// which the server reads and shows in
// INFORMATION_SCHEMA.CONNECTION_ATTRIBUTES, connection-attributes
// granted [PROTOCOL Capabilities].
func TestLiveAttributes(t *testing.T) {
	t.Parallel()

	offer := DefaultOffer().WithAttribute(AttributeApplication, "turborel-go live")
	c := liveConnect(t, liveTLS(t, 0, false), Trust{}, &Config{Offer: &offer})

	rows := mustQuery(t, c, "SELECT ATTRIBUTE_VALUE FROM INFORMATION_SCHEMA.CONNECTION_ATTRIBUTES "+
		"WHERE ATTRIBUTE_NAME = 'application'")
	if !slices.ContainsFunc(rows, func(r Row) bool { return r[0] == Char("turborel-go live") }) {
		t.Errorf("attributes %v", rows)
	}

	if err := c.Ping(context.Background()); err != nil {
		t.Errorf("Ping: %v", err)
	}
}
