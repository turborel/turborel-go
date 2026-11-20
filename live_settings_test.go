package turborel

import (
	"context"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/turborel/turborel-go/internal/testserver"
)

// liveSettings are the settings of a URI to the live server, its query
// query, as the owner proved by password.
func liveSettings(t *testing.T, hosts, query string) *Settings {
	t.Helper()

	s := liveServer(t)
	if hosts == "" {
		hosts = s.Address
	}

	uri := URIScheme + testserver.Owner + ":" + url.PathEscape(testserver.OwnerPassword) + "@" + hosts + "/?tls_roots=" +
		url.QueryEscape(s.Files.CA) + "&server_name=localhost"
	if query != "" {
		uri += "&" + query
	}

	settings, err := ResolveIn(mustURI(t, uri), environment())
	if err != nil {
		t.Fatal(err)
	}

	return settings
}

// connectSettings connects by settings, failing the test if it fails.
func connectSettings(t *testing.T, s *Settings) *Conn {
	t.Helper()

	c, err := s.Connect(context.Background())
	if err != nil {
		t.Fatalf("connecting by %v: %v", s, err)
	}

	t.Cleanup(func() { _ = c.Close() })

	return c
}

// TestLiveSettingsTLS connects by settings over TLS, by SCRAM.
//
// Verifies: [CLIENT Connecting by settings] settings connect as a
// target's connection does, a password proved by SCRAM over TLS checked
// against server_name; application_name is sent as the connection
// attribute application [CLIENT Where settings come from]; every
// connection the settings make shares one key cache, the second skipping
// the derivation [CLIENT The SCRAM key cache]; the role a session sees is
// read-write of a server alone [CLIENT Several servers].
func TestLiveSettingsTLS(t *testing.T) {
	t.Parallel()

	settings := liveSettings(t, "", "tls_mode=verify-full&application_name=go%20settings")

	var c *Conn
	for range 2 {
		c = connectSettings(t, settings)
	}

	rows := mustQuery(t, c, "SELECT ATTRIBUTE_VALUE FROM INFORMATION_SCHEMA.CONNECTION_ATTRIBUTES "+
		"WHERE ATTRIBUTE_NAME = 'application'")
	if !slices.ContainsFunc(rows, func(r Row) bool { return r[0] == Char("go settings") }) {
		t.Errorf("attributes %v", rows)
	}

	if hits, misses := settings.KeyCache.Lookups(); hits != 1 || misses != 1 || c.Authorization() != testserver.Owner {
		t.Errorf("hits %d, misses %d, as %s", hits, misses, c.Authorization())
	}

	if role, err := c.Role(context.Background()); err != nil || role != RoleReadWrite {
		t.Errorf("role %v, %v", role, err)
	}
}

// TestLiveSettingsModes connects in each TLS mode.
//
// Verifies: [CLIENT TLS by mode] require takes any certificate;
// verify-ca checks it against the authorities trusted, not its name,
// refusing one of an authority not trusted whatever its name;
// verify-full checks its name too, server_name's; disable is plain TCP,
// where a password is refused before it is sent (SQLSTATE 28000) and
// trust is taken; channel binding disable and require each connect over
// TLS by SCRAM, and require refuses a connection that cannot bind
// (SQLSTATE 28000).
func TestLiveSettingsModes(t *testing.T) {
	t.Parallel()

	for _, query := range []string{
		"tls_mode=require", "tls_mode=verify-ca&server_name=wrong.example", "tls_mode=verify-full&channel_binding=require",
		"tls_mode=verify-full&channel_binding=disable",
	} {
		if c := connectSettings(t, liveSettings(t, "", query)); c.Authorization() != testserver.Owner {
			t.Errorf("%s: as %s", query, c.Authorization())
		}
	}

	misnamed := liveSettings(t, "", "tls_mode=verify-full&server_name=wrong.example")
	if _, err := misnamed.Connect(context.Background()); err == nil {
		t.Error("verify-full took a certificate of another name")
	}

	untrusted := liveSettings(t, "", "tls_mode=verify-ca&server_name=wrong.example")
	untrusted.TLS.Roots = writePEM(t, "other.pem", testCertificatePEM(t))

	if _, err := untrusted.Connect(context.Background()); err == nil {
		t.Error("verify-ca took a certificate of an authority not trusted")
	}

	_, err := liveSettings(t, "", "tls_mode=disable").Connect(context.Background())
	assertState(t, err, sqlstateInvalidAuthorization)

	trusted := liveSettings(t, "", "tls_mode=disable")
	trusted.HasPassword, trusted.Passfile = false, ""

	if c := connectSettings(t, trusted); c.target == nil || !c.target.IsPlainTCP() {
		t.Errorf("disable connected over %v", c.target)
	}

	trusted.ChannelBinding = BindingRequire

	_, err = trusted.Connect(context.Background())
	assertState(t, err, sqlstateInvalidAuthorization)
}

// TestLiveSettingsLocalAndCertificate connects over the local socket and
// by a client certificate.
//
// Verifies: [CLIENT TLS by mode] a host that is a path is the server's
// Unix socket, over which no TLS is used whatever the mode; with no
// password and a client certificate named, the certificate proves the
// user [CLIENT Connecting by settings].
func TestLiveSettingsLocalAndCertificate(t *testing.T) {
	t.Parallel()

	s := liveServer(t)

	if s.Socket != "" {
		local := liveSettings(t, url.PathEscape(s.Socket), "tls_mode=verify-full")
		if c := connectSettings(t, local); c.target == nil || c.target.IsTLS() || c.target.IsPlainTCP() {
			t.Errorf("the socket connected over %v", c.target)
		}
	}

	presenting := liveSettings(t, "", "tls_mode=verify-full&tls_certificate="+url.QueryEscape(s.Files.ClientCert)+
		"&tls_key="+url.QueryEscape(s.Files.ClientKey))
	presenting.HasPassword, presenting.Passfile = false, ""

	if c := connectSettings(t, presenting); c.Authorization() != testserver.Owner {
		t.Errorf("the certificate logged in as %s", c.Authorization())
	}
}

// TestLiveSettingsHosts connects to the first of several servers kept.
//
// Verifies: [CLIENT Several servers] hosts are tried in order, one
// unreachable passed for the next, each address a name resolves to
// tried; read-write takes a server alone; read-only passes it, and with
// none kept the failure names each address and why (SQLSTATE 08001);
// load_balance random tries them in a random order.
func TestLiveSettingsHosts(t *testing.T) {
	t.Parallel()

	s := liveServer(t)
	_, port, _ := net.SplitHostPort(s.Address)
	closed := "127.0.0.1:" + strconv.Itoa(int(closedPort(t)))
	hosts := closed + ",localhost:" + port

	connectSettings(t, liveSettings(t, hosts, "tls_mode=require&target=read-write"))
	connectSettings(t, liveSettings(t, s.Address+","+s.Address, "tls_mode=require&load_balance=random"))

	_, err := liveSettings(t, hosts, "tls_mode=require&target=read-only").Connect(context.Background())
	assertUnable(t, "read-only of a server alone", err)

	if err == nil || !strings.Contains(err.Error(), closed) || !strings.Contains(err.Error(), "localhost (") {
		t.Errorf("none kept: %v", err)
	}
}
