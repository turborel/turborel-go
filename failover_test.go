package turborel

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestShuffleBySeed orders hosts by a seed.
//
// Verifies: [CLIENT Several servers] hosts ordered at random are each
// host once, in an order the seed decides, the same for the same seed;
// different seeds give different orders; one host or none stays as it
// is; each seed is the system's own.
func TestShuffleBySeed(t *testing.T) {
	t.Parallel()

	hosts := []int{0, 1, 2, 3, 4, 5, 6, 7}
	order := func(seed uint64) []int {
		h := slices.Clone(hosts)
		shuffle(h, seed)

		return h
	}

	sorted := slices.Sorted(slices.Values(order(7)))
	if !slices.Equal(sorted, hosts) || !slices.Equal(order(7), order(7)) {
		t.Error("a shuffle is not of the hosts, or not the seed's")
	}

	orders := map[string]bool{}
	for seed := range uint64(16) {
		orders[fmt.Sprint(order(seed))] = true
	}

	if len(orders) < 8 {
		t.Errorf("16 seeds gave %d orders", len(orders))
	}

	one := []int{1}
	shuffle(one, 3)
	shuffle([]int{}, 3)

	first, second := shuffleSeed(), shuffleSeed()
	if one[0] != 1 || first == second {
		t.Error("one host moved, or two seeds alike")
	}
}

// TestNoneKept says why each server was not kept.
//
// Verifies: [CLIENT Several servers] of one server tried, its own
// failure is given; of several, SQLSTATE 08001 names each and why, or
// 28000 where the last refused the user; of none, 08001.
func TestNoneKept(t *testing.T) {
	t.Parallel()

	lost := broken("cannot connect")
	refused := &Error{SQLCode: sqlcodeConnection, SQLState: sqlstateInvalidAuthorization, Message: "no"}

	if got := noneKept([]tried{{"a", refused}}); got != refused {
		t.Errorf("one server gave %v", got)
	}

	both := noneKept([]tried{{"a", refused}, {"b", lost}})
	if both.SQLState != sqlstateUnableToConnect || both.Message != "no server was kept: a: no; b: cannot connect" {
		t.Errorf("two servers gave %v", both)
	}

	if got := noneKept([]tried{{"b", lost}, {"a", refused}}); got.SQLState != sqlstateInvalidAuthorization {
		t.Errorf("the last refusing gave %v", got)
	}

	if got := noneKept(nil); got.SQLState != sqlstateUnableToConnect || got.Message != "no server is named" {
		t.Errorf("none gave %v", got)
	}

	if got := asError(errDiskGone); got.SQLState != sqlstateConnectionFailure || !errors.Is(got, errDiskGone) {
		t.Errorf("a failure not the driver's gave %v", got)
	}
}

// TestRoleOfReplicas reads a server's role from REPLICAS.
//
// Verifies: [CLIENT Several servers] a server alone, which has no
// REPLICAS (class 2A or 42), takes changes; of a replica, PRIMARY is
// read-write, BACKUP or UNKNOWN read-only; no row, as a session not the
// owner's sees, is unknown; another failure is the query's.
func TestRoleOfReplicas(t *testing.T) {
	t.Parallel()

	shown := func(role string) *Outcome { return &Outcome{Columns: []Column{{}}, Rows: []Row{{Char(role)}}} }
	cases := []struct {
		outcome *Outcome
		err     error
		want    Role
	}{
		{nil, &Error{SQLState: "42000"}, RoleReadWrite},
		{nil, &Error{SQLState: "2A000"}, RoleReadWrite},
		{shown("PRIMARY"), nil, RoleReadWrite},
		{shown("BACKUP "), nil, RoleReadOnly},
		{shown("UNKNOWN"), nil, RoleReadOnly},
		{&Outcome{Columns: []Column{{}}}, nil, RoleUnknown},
		{&Outcome{}, nil, RoleUnknown},
	}

	for _, c := range cases {
		if got, err := roleOf(c.outcome, c.err); err != nil || got != c.want {
			t.Errorf("%+v gave %v, %v", c, got, err)
		}
	}

	if _, err := roleOf(nil, broken("lost")); err == nil {
		t.Error("a lost connection gave a role")
	}

	if RoleUnknown.String() != "of a role this session cannot see" || RoleReadOnly.String() != "read-only" {
		t.Error("roles in words")
	}
}

// listen serves each connection made to a local port by the next of
// scripts, a nil one closing it once it has read; it gives the port.
func listen(t *testing.T, scripts ...func(f *fake)) uint16 {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(context.Background(), networkTCP, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})

	go func() {
		defer close(done)

		for _, script := range scripts {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			if script == nil {
				_, _ = conn.Read(make([]byte, 1))
			} else {
				script(&fake{t: t, conn: conn, r: bufio.NewReader(conn)})
			}

			_ = conn.Close()
		}
	}()

	t.Cleanup(func() {
		_ = listener.Close()
		<-done
	})

	return portOf(t, listener)
}

// closedPort is a local port no server listens on.
func closedPort(t *testing.T) uint16 {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(context.Background(), networkTCP, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	port := portOf(t, listener)
	_ = listener.Close()

	return port
}

// portOf gives the port a TCP listener listens on.
func portOf(t *testing.T, listener net.Listener) uint16 {
	t.Helper()

	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("%v is no TCP address", listener.Addr())
	}

	return uint16(address.Port) //nolint:gosec // G115: a port fits 16 bits.
}

// localSettings are settings of user u at the local ports given.
func localSettings(ports ...uint16) *Settings {
	s := &Settings{User: "u", ConnectTimeout: 2 * time.Second, KeyCache: NewKeyCache()}
	for _, p := range ports {
		s.Hosts = append(s.Hosts, Host{Name: "127.0.0.1", Port: p})
	}

	return s
}

// greeted serves a login: Hello, then the Status replies of told, then
// Ready granting capabilities.
func greeted(told map[string]string, capabilities ...string) func(f *fake) {
	return func(f *fake) {
		f.expect(kindHello)

		for name, value := range told {
			f.reply(kindStatus, func(w *writer) {
				w.str(name)
				w.str(value)
			})
		}

		f.ready(capabilities...)
	}
}

// TestConnectBySettings connects to the first server kept.
//
// Verifies: [CLIENT Several servers] hosts are tried in order, one
// unreachable passed for the next; under prefer a server whose TLS
// handshake fails is connected to again over plain TCP [CLIENT TLS by
// mode]; with no password, trust; the role is told by SERVER_READ_ONLY,
// a server of another role passed (SQLSTATE 08001); of one that does not
// tell it, read from REPLICAS; a name that does not resolve is passed,
// saying so [CLIENT Connecting by settings].
func TestConnectBySettings(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	preferring := localSettings(closedPort(t), listen(t, nil, greeted(nil)))

	c, err := preferring.Connect(ctx)
	if err != nil || c.target == nil || !c.target.IsPlainTCP() {
		t.Fatalf("prefer gave %v, %v", c, err)
	}

	_ = c.Close()

	readOnly := localSettings(listen(t, greeted(map[string]string{StatusServerReadOnly: "YES"})))
	readOnly.TLSMode, readOnly.Target = TLSDisable, TargetReadWrite

	_, err = readOnly.Connect(ctx)
	if err == nil || !strings.Contains(err.Error(), "the server is read-only, and read-write is asked") {
		t.Errorf("a replica for read-write gave %v", err)
	}

	alone := localSettings(listen(t, func(f *fake) {
		greeted(nil)(f)
		f.expect(kindQuery)
		f.refuse("42000", false, "")
	}))
	alone.TLSMode, alone.Target = TLSDisable, TargetReadOnly

	_, err = alone.Connect(ctx)
	assertUnable(t, "a server alone for read-only", err)

	unresolved := localSettings()
	unresolved.Hosts = []Host{{Name: "nowhere.invalid", Port: 1}, {Name: "nowhere.invalid", Port: 2}}

	_, err = unresolved.Connect(ctx)
	if err == nil || !strings.Contains(err.Error(), "nowhere.invalid:1: cannot resolve the name") {
		t.Errorf("a name that does not resolve gave %v", err)
	}
}

// TestSettingsRefuseProof refuses a proof the settings cannot make
// before anything is proved.
//
// Verifies: [CLIENT TLS by mode] channel binding required refuses a
// connection that cannot bind, before any connection is proved (SQLSTATE
// 28000); a password over plain TCP is refused before it is sent
// [CLIENT Connecting by settings]; a TLS mode that verifies needs the
// authorities trusted; a client certificate needs its key and must be
// read.
func TestSettingsRefuseProof(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	bound := localSettings(listen(t, nil))
	bound.TLSMode, bound.ChannelBinding = TLSDisable, BindingRequire

	_, err := bound.Connect(ctx)
	assertState(t, err, sqlstateInvalidAuthorization)

	inClear := localSettings(listen(t, nil))
	inClear.TLSMode, inClear.Password, inClear.HasPassword = TLSDisable, "secret", true

	_, err = inClear.Connect(ctx)
	assertState(t, err, sqlstateInvalidAuthorization)

	verifying := localSettings(1)
	verifying.TLSMode = TLSVerifyFull

	if _, err := verifying.Connect(ctx); err == nil {
		t.Error("verify-full trusted no authority")
	}

	verifying.TLSMode = TLSVerifyCA
	if _, err := verifying.Connect(ctx); err == nil {
		t.Error("verify-ca trusted no authority")
	}

	presenting := localSettings(1)
	presenting.TLS.Certificate = "client.pem"

	if _, err := presenting.Connect(ctx); err == nil || !strings.Contains(err.Error(), "none is named") {
		t.Errorf("a certificate without its key gave %v", err)
	}

	presenting.TLS.Key = "client.key"
	if _, err := presenting.Connect(ctx); err == nil || !strings.Contains(err.Error(), "cannot read client.pem") {
		t.Errorf("a certificate not there gave %v", err)
	}
}

// TestVerifyChain checks a server's chain whatever its name.
//
// Verifies: [CLIENT TLS by mode] verify-ca takes a certificate an
// authority trusted signed, of any name, and refuses a server that
// presents none or one no authority trusted signed.
func TestVerifyChain(t *testing.T) {
	t.Parallel()

	block, _ := pem.Decode(testCertificatePEM(t))

	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}

	roots := x509.NewCertPool()
	roots.AddCert(certificate)

	state := tls.ConnectionState{PeerCertificates: []*x509.Certificate{certificate, certificate}}
	if err := verifyChain(state, roots); err != nil {
		t.Errorf("a trusted chain: %v", err)
	}

	if err := verifyChain(state, x509.NewCertPool()); err == nil {
		t.Error("an untrusted chain was taken")
	}

	if err := verifyChain(tls.ConnectionState{}, roots); !errors.Is(err, errNoCertificate) {
		t.Errorf("no certificate gave %v", err)
	}

	if _, err := Resolve(optionsOf(t, OptionUser, "u", OptionHost, "h")); err != nil {
		t.Errorf("Resolve: %v", err)
	}
}
