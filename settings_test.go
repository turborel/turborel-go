package turborel

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// environment is an environment of pairs, as ResolveIn reads one.
func environment(pairs ...string) func(string) (string, bool) {
	values := map[string]string{}
	for i := 0; i+1 < len(pairs); i += 2 {
		values[pairs[i]] = pairs[i+1]
	}

	return func(name string) (string, bool) {
		value, ok := values[name]

		return value, ok
	}
}

// scratch writes a file of contents in the test's temporary directory.
func scratch(t *testing.T, name, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

// optionsOf is options of key and value pairs.
func optionsOf(t *testing.T, pairs ...string) Options {
	t.Helper()

	var o Options

	for i := 0; i+1 < len(pairs); i += 2 {
		if err := o.Set(pairs[i], pairs[i+1]); err != nil {
			t.Fatal(err)
		}
	}

	return o
}

// assertUnable checks that err is SQLSTATE 08001.
func assertUnable(t *testing.T, what string, err error) {
	t.Helper()

	if failure, ok := errors.AsType[*Error](err); !ok || failure.SQLState != sqlstateUnableToConnect ||
		failure.SQLCode != sqlcodeConnection {
		t.Errorf("%s: %v", what, err)
	}
}

// TestURIGivesSettings reads a connection URI.
//
// Verifies: [CLIENT The connection URI] a URI gives its user, password,
// hosts each of its port, an IPv6 address in brackets, a local socket
// percent-encoded, and its query's settings; percent-encoding is decoded
// in each part; the password is never shown [CLIENT Where settings come
// from].
func TestURIGivesSettings(t *testing.T) {
	t.Parallel()

	uri := "turborel://ann:p%40ss@db1:7000,[::1]:7001,db3,%2Frun%2Fs.sock/" +
		"?tls_mode=verify-full&target=read-write&connect_timeout=5"

	options, err := OptionsFromURI(uri)
	if err != nil {
		t.Fatal(err)
	}

	user, _ := options.Get(OptionUser)
	password, _ := options.Get(OptionPassword)

	settings, err := ResolveIn(options, environment())
	if err != nil || user != "ann" || password != "p@ss" {
		t.Fatalf("%s, %s, %v", user, password, err)
	}

	want := []Host{
		{Name: "db1", Port: 7000}, {Name: "::1", Port: 7001}, {Name: "db3", Port: DefaultPort}, {Path: "/run/s.sock"},
	}
	if !reflect.DeepEqual(settings.Hosts, want) || settings.TLSMode != TLSVerifyFull ||
		settings.Target != TargetReadWrite || settings.ConnectTimeout != 5*time.Second {
		t.Errorf("settings %+v", settings)
	}

	shown := options.String() + options.GoString() + settings.String() + settings.GoString()
	if strings.Contains(shown, "p@ss") {
		t.Errorf("the password shown: %s", shown)
	}

	bare, err := ResolveIn(mustURI(t, "turborel://ann@/"), environment())
	if err != nil || !reflect.DeepEqual(bare.Hosts, []Host{{Name: DefaultHost, Port: DefaultPort}}) {
		t.Errorf("a URI of no host gave %v, %v", bare, err)
	}
}

// mustURI gives the options of uri.
func mustURI(t *testing.T, uri string) Options {
	t.Helper()

	o, err := OptionsFromURI(uri)
	if err != nil {
		t.Fatal(err)
	}

	return o
}

// TestMalformedURIRefused refuses a malformed URI or setting.
//
// Verifies: [CLIENT The connection URI] a URI of another scheme, of a
// path, of a key that names no setting, of a pair without =, of a % not
// followed by two hexadecimal digits, or not UTF-8 once decoded is
// refused (SQLSTATE 08001); so is a word no setting takes, a port that is
// no number, a timeout that is no whole number of seconds, and no user
// [CLIENT Where settings come from].
func TestMalformedURIRefused(t *testing.T) {
	t.Parallel()

	for _, uri := range []string{
		"other://a@b", "turborel://a@b/db", "turborel://a@b/?colour=red", "turborel://a@b/?tls_mode", "turborel://a%4@b",
		"turborel://a%ff@b", "turborel://a%zz@b",
	} {
		_, err := OptionsFromURI(uri)
		assertUnable(t, uri, err)
	}

	for _, uri := range []string{
		"turborel://a@b/?tls_mode=always", "turborel://a@b:port", "turborel://a@b/?connect_timeout=-1", "turborel://b",
	} {
		_, err := ResolveIn(mustURI(t, uri), environment())
		assertUnable(t, uri, err)
	}

	if _, err := SettingsFromURI("turborel://a@b/db"); err == nil {
		t.Error("a URI of a path was taken")
	}
}

// TestSettingsLaidInOrder lays the program's settings over a service's,
// the environment's and the defaults.
//
// Verifies: [CLIENT Where settings come from] a setting the program gives
// prevails over the service's, which prevails over the environment's,
// which prevails over the default; the environment names the service and
// its file when the program does not; an empty variable gives nothing;
// the environment is read through a function the program gives.
func TestSettingsLaidInOrder(t *testing.T) {
	t.Parallel()

	file := scratch(t, "services.conf",
		"# servers\n[prod]\nhost = prod-db\nport = 7500\ntls_mode = require\n\n[test]\nhost = test-db\n")
	env := environment("TURBOREL_SERVICE", "prod", "TURBOREL_SERVICE_FILE", file, "TURBOREL_USER", "env-user",
		"TURBOREL_PORT", "7600", "TURBOREL_TLS_MODE", "disable", "TURBOREL_APPLICATION_NAME", "")
	explicit := optionsOf(t, OptionTLSMode, "verify-ca")

	settings, err := ResolveIn(explicit, env)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(settings.Hosts, []Host{{Name: "prod-db", Port: 7500}}) || settings.TLSMode != TLSVerifyCA ||
		settings.User != "env-user" || settings.ApplicationName != "" || settings.ChannelBinding != BindingPrefer {
		t.Errorf("settings %+v", settings)
	}

	if err := explicit.Set(OptionService, "test"); err != nil {
		t.Fatal(err)
	}

	chosen, err := ResolveIn(explicit, env)
	if err != nil || !reflect.DeepEqual(chosen.Hosts, []Host{{Name: "test-db", Port: 7600}}) {
		t.Errorf("the program names the service: %v, %v", chosen, err)
	}
}

// TestServiceFileBySection reads a service file's section.
//
// Verifies: [CLIENT The service file] a service file's section is its
// lines' settings; a section that is not there, a setting before any
// section, a line without =, a key that names no setting, and a service
// or service file set by a service are refused, naming the file and line;
// a file that cannot be read is refused (SQLSTATE 08001).
func TestServiceFileBySection(t *testing.T) {
	t.Parallel()

	options, err := serviceOf("[a]\nuser=ann\n[b]\nuser = bob\n  host =  h  \n", "b", "services.conf")
	user, _ := options.Get(OptionUser)
	host, _ := options.Get(OptionHost)

	if err != nil || user != "bob" || host != "h" {
		t.Errorf("section b gave %s, %s, %v", user, host, err)
	}

	if _, err := serviceOf("[a]\nuser=ann\n", "b", "services.conf"); err == nil ||
		!strings.Contains(err.Error(), "services.conf has no service b") {
		t.Errorf("a missing section: %v", err)
	}

	for text, why := range map[string]string{
		"user=ann\n":        "services.conf line 1: a setting comes before any section",
		"[a]\nuser\n":       "services.conf line 2: a setting is <key> = <value>",
		"[a]\ncolour=red\n": "services.conf line 2: colour is no connection setting",
		"[a]\nservice=b\n":  "services.conf line 2: a service cannot set service",
		"[a\n":              "services.conf line 1: a section's name ends with ]",
	} {
		if _, err := serviceOf(text, "a", "services.conf"); err == nil || !strings.Contains(err.Error(), why) {
			t.Errorf("%q: %v", text, err)
		}
	}

	_, err = Service(filepath.Join(t.TempDir(), "none"), "s")
	assertUnable(t, "a service file not there", err)
}

// TestPasswordFileMatches reads the password of the first line that
// matches.
//
// Verifies: [CLIENT The password file] the password file gives the
// password of its first line matching host, port and user, * matching
// any, \: and \\ taken as they are, an escaped \* matching only *;
// comments and short lines are skipped; the settings' own password
// prevails over it; a file that cannot be read gives none, and a local
// socket is matched by its path and the default port.
func TestPasswordFileMatches(t *testing.T) {
	t.Parallel()

	text := "# passwords\nshort:line\ndb1:7422:ann:first\n*:*:ann:any\\:host\ndb\\:2:*:bob:c:\\\\d\n\\*:1:carl:star\n"
	cases := []struct{ host, port, user, want string }{
		{"db1", "7422", "ann", "first"},
		{"db9", "1", "ann", "any:host"},
		{"db:2", "7", "bob", `c:\d`},
		{"x", "1", "carl", ""},
		{"*", "1", "carl", "star"},
		{"db1", "7422", "nobody", ""},
	}

	for _, c := range cases {
		if got, _ := passwordOf(text, c.host, c.port, c.user); got != c.want {
			t.Errorf("%+v gave %q", c, got)
		}
	}

	file := scratch(t, "passfile", "/run/s.sock:7422:ann:local\n"+text)
	settings, err := ResolveIn(optionsOf(t, OptionUser, "ann", OptionPassfile, file, OptionHost, "db1"), environment())
	if err != nil {
		t.Fatal(err)
	}

	if got, _ := settings.PasswordFor(Host{Name: "db1", Port: 7422}); got != "first" {
		t.Errorf("the file gave %q", got)
	}

	if got, _ := settings.PasswordFor(Host{Path: "/run/s.sock"}); got != "local" {
		t.Errorf("a local socket gave %q", got)
	}

	given := *settings
	given.Password, given.HasPassword = "given", true

	if got, _ := given.PasswordFor(Host{Name: "db1", Port: 7422}); got != "given" {
		t.Errorf("the settings' password gave %q", got)
	}

	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}

	for _, passfile := range []string{file, t.TempDir(), ""} {
		settings.Passfile = passfile
		if _, ok := settings.PasswordFor(Host{Name: "db1", Port: 7422}); ok {
			t.Errorf("%q gave a password", passfile)
		}
	}
}

// TestEnvironmentAndWords reads every variable and the words of each
// kind.
//
// Verifies: [CLIENT Where settings come from] every variable of the
// environment gives its setting.
func TestEnvironmentAndWords(t *testing.T) {
	t.Parallel()

	pairs := make([]string, 0, 2*len(EnvironmentVariables()))
	for _, v := range EnvironmentVariables() {
		pairs = append(pairs, v[0], "v")
	}

	options := EnvironmentOptions(environment(pairs...))
	for _, v := range EnvironmentVariables() {
		if got, _ := options.Get(v[1]); got != "v" {
			t.Errorf("%s gave %q", v[0], got)
		}
	}
}

// TestWordsRead reads the words of each kind.
//
// Verifies: [CLIENT Where settings come from] the words of each kind are
// read as named, and written so; a word none takes is refused saying the
// words it takes; one port each of several hosts; the platform's
// authorities trusted if asked.
func TestWordsRead(t *testing.T) {
	t.Parallel()

	m, errMode := ParseTLSMode(OptionTLSMode, "prefer")
	b, errBinding := ParseChannelBinding(OptionChannelBinding, "require")
	l, errBalance := ParseLoadBalance(OptionLoadBalance, "random")

	if errors.Join(errMode, errBinding, errBalance) != nil || m != TLSPrefer || b != BindingRequire ||
		l != LoadBalanceRandom || !TLSVerifyCA.Verifies() || TLSRequire.Verifies() {
		t.Error("words read")
	}

	if _, err := ParseTargetRole(OptionTarget, "primary"); err == nil ||
		!strings.Contains(err.Error(), "target is primary, not one of any, read-write, read-only") {
		t.Errorf("a target none takes: %v", err)
	}

	both := optionsOf(t, OptionHost, "a,b", OptionPort, "1,2", OptionUser, "u", OptionTLSSystemRoots, "true")

	settings, err := ResolveIn(both, environment())
	if err != nil || !reflect.DeepEqual(settings.Hosts, []Host{{Name: "a", Port: 1}, {Name: "b", Port: 2}}) ||
		!settings.TLS.SystemRoots {
		t.Errorf("settings %v, %v", settings, err)
	}

	written := []string{TLSVerifyFull.String(), LoadBalanceRandom.String(), BindingDisable.String(), TargetAny.String()}
	if !slices.Equal(written, []string{"verify-full", "random", "disable", "any"}) || both.IsEmpty() {
		t.Errorf("the words written %v", written)
	}
}

// TestMalformedHostsRefused refuses malformed hosts and words.
//
// Verifies: [CLIENT Where settings come from] a list of hosts with an
// empty one, an IPv6 address whose bracket is not closed or whose port is
// no number, a name whose port is no number, a list of ports with one
// that is no number, and a word no channel binding, target, load balance
// or truth takes are each refused (SQLSTATE 08001); several hosts of one
// port each take it; a name of two colons is an IPv6 address of no port;
// a path is a local socket.
func TestMalformedHostsRefused(t *testing.T) {
	t.Parallel()

	resolve := func(pairs ...string) (*Settings, error) {
		return ResolveIn(optionsOf(t, append([]string{OptionUser, "u"}, pairs...)...), environment())
	}

	for _, host := range []string{"a,,b", "[::1", "[::1]:x", "a:x"} {
		_, err := resolve(OptionHost, host)
		assertUnable(t, host, err)
	}

	_, err := resolve(OptionHost, "a", OptionPort, "1,x")
	assertUnable(t, "a port that is no number", err)

	for _, key := range []string{OptionChannelBinding, OptionTarget, OptionLoadBalance, OptionTLSSystemRoots} {
		_, err := resolve(key, "sometimes")
		assertUnable(t, key, err)
	}

	settings, err := resolve(OptionHost, `a,b,[::1],::2,\\.\pipe\t`, OptionPort, "9")
	want := []Host{
		{Name: "a", Port: 9}, {Name: "b", Port: 9}, {Name: "::1", Port: 9}, {Name: "::2", Port: 9}, {Path: `\\.\pipe\t`},
	}

	if err != nil || !reflect.DeepEqual(settings.Hosts, want) {
		t.Errorf("one port for all gave %v, %v", settings, err)
	}

	if err := (&Options{}).Set("colour", "red"); err == nil {
		t.Error("a setting of no name was set")
	}
}
