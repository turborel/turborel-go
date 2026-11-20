package turborel

// Connection settings from outside the program [CLIENT Where settings
// come from]: where the servers are, as whom to connect, how, and what a
// server must be, read from layers of Options: the program's own (or a
// connection URI's), then a service's section of the service file, then
// the environment, then the defaults, each setting taken from the first
// layer that has it. A password none of them gives is looked up in the
// password file. Settings.Connect connects as they say.

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The settings' names, as the program, a URI's query and a service file
// write them.
const (
	OptionHost            = "host"
	OptionPort            = "port"
	OptionUser            = "user"
	OptionPassword        = "password"
	OptionTLSMode         = "tls_mode"
	OptionChannelBinding  = "channel_binding"
	OptionApplicationName = "application_name"
	OptionConnectTimeout  = "connect_timeout"
	OptionService         = "service"
	OptionServiceFile     = "service_file"
	OptionPassfile        = "passfile"
	OptionTarget          = "target"
	OptionLoadBalance     = "load_balance"
	OptionTLSRoots        = "tls_roots"
	OptionTLSSystemRoots  = "tls_system_roots"
	OptionTLSCertificate  = "tls_certificate"
	OptionTLSKey          = "tls_key"
	OptionServerName      = "server_name"
)

// OptionKeys gives every setting's name.
func OptionKeys() []string {
	return []string{
		OptionHost, OptionPort, OptionUser, OptionPassword, OptionTLSMode, OptionChannelBinding, OptionApplicationName,
		OptionConnectTimeout, OptionService, OptionServiceFile, OptionPassfile, OptionTarget, OptionLoadBalance,
		OptionTLSRoots, OptionTLSSystemRoots, OptionTLSCertificate, OptionTLSKey, OptionServerName,
	}
}

// EnvironmentVariables gives the environment's variables, each with the
// setting it gives.
func EnvironmentVariables() [][2]string {
	return [][2]string{
		{"TURBOREL_HOST", OptionHost},
		{"TURBOREL_PORT", OptionPort},
		{"TURBOREL_USER", OptionUser},
		{"TURBOREL_PASSWORD", OptionPassword},
		{"TURBOREL_TLS_MODE", OptionTLSMode},
		{"TURBOREL_APPLICATION_NAME", OptionApplicationName},
		{"TURBOREL_CONNECT_TIMEOUT", OptionConnectTimeout},
		{"TURBOREL_SERVICE", OptionService},
		{"TURBOREL_SERVICE_FILE", OptionServiceFile},
		{"TURBOREL_PASSFILE", OptionPassfile},
	}
}

// The defaults of settings.
const (
	// DefaultHost is the host connected to when none is named.
	DefaultHost = "localhost"
	// DefaultServiceFile is the service file read when none is named, in
	// the home directory.
	DefaultServiceFile = ".turborel_service.conf"
	// DefaultPassfile is the password file read when none is named, in
	// the home directory.
	DefaultPassfile = ".turborel_pass" //nolint:gosec // G101: a file's name, not a credential.
)

// The text settings are written in.
const (
	listSeparator = ","
	hiddenValue   = "..."
)

// sqlstateUnableToConnect is the SQLSTATE of settings no connection can
// be made by: class 08, connection exception, subclass 001.
const sqlstateUnableToConnect = "08001"

// invalid is a setting that cannot be read, or settings no connection can
// be made by: SQLSTATE 08001, the client unable to connect.
func invalid(message string) *Error {
	return &Error{SQLCode: sqlcodeConnection, SQLState: sqlstateUnableToConnect, Message: message}
}

// Options is a layer of connection settings, each by its name.
//
//nolint:recvcheck // Set adds to the layer; the rest read it, String hiding the password of a value too.
type Options struct {
	values map[string]string
}

// Set sets key to value. It fails with SQLSTATE 08001 if key names no
// setting.
func (o *Options) Set(key, value string) error {
	if !slices.Contains(OptionKeys(), key) {
		return invalid(key + " is no connection setting")
	}

	if o.values == nil {
		o.values = map[string]string{}
	}

	o.values[key] = value

	return nil
}

// Get gives the value of key, if set.
func (o Options) Get(key string) (string, bool) {
	value, ok := o.values[key]

	return value, ok
}

// IsEmpty says whether no setting is set.
func (o Options) IsEmpty() bool { return len(o.values) == 0 }

// Over gives these settings laid over lower: each of lower's kept where
// these have none.
func (o Options) Over(lower Options) Options {
	values := maps.Clone(lower.values)
	if values == nil {
		values = map[string]string{}
	}

	maps.Copy(values, o.values)

	return Options{values: values}
}

// String gives the settings, never the password.
func (o Options) String() string {
	keys := slices.Sorted(maps.Keys(o.values))
	shown := make([]string, len(keys))

	for i, k := range keys {
		value := o.values[k]
		if k == OptionPassword {
			value = hiddenValue
		}

		shown[i] = k + "=" + value
	}

	return "Options{" + strings.Join(shown, " ") + "}"
}

// GoString gives the settings as String does, never the password.
func (o Options) GoString() string { return o.String() }

// EnvironmentOptions gives the settings the environment gives, variable
// reading each of EnvironmentVariables' variables, as os.LookupEnv does:
// one empty gives none.
func EnvironmentOptions(variable func(name string) (string, bool)) Options {
	values := map[string]string{}

	for _, pair := range EnvironmentVariables() {
		if value, ok := variable(pair[0]); ok && value != "" {
			values[pair[1]] = value
		}
	}

	return Options{values: values}
}

// Host is where a server is: a host name or address and a port, over
// TCP; or, if Path is set, a Unix socket, on Windows a named pipe.
type Host struct {
	Name string
	Port uint16
	Path string
}

// String gives the host as name:port, or its path.
func (h Host) String() string {
	if h.Path != "" {
		return h.Path
	}

	return h.Name + ":" + strconv.Itoa(int(h.Port))
}

// TLSFiles are the files a connection's TLS is made of, and the name the
// server's certificate is checked against, if not the host's.
type TLSFiles struct {
	Roots       string
	SystemRoots bool
	Certificate string
	Key         string
	ServerName  string
}

// Settings say how to connect.
type Settings struct {
	// Hosts are the servers, tried in turn.
	Hosts []Host
	// User is the user to connect as.
	User string
	// Password is the user's password, if HasPassword; else the password
	// file's.
	Password    string
	HasPassword bool
	// TLSMode is how TLS is used.
	TLSMode TLSMode
	// ChannelBinding says whether SCRAM binds its channel.
	ChannelBinding ChannelBinding
	// ApplicationName is the program's name, as it tells the server;
	// empty, none.
	ApplicationName string
	// ConnectTimeout bounds each step of connecting; 0, no limit.
	ConnectTimeout time.Duration
	// Passfile is the password file; empty, none.
	Passfile string
	// Target is what a server must be.
	Target TargetRole
	// LoadBalance says whether the servers are tried in order.
	LoadBalance LoadBalance
	// TLS are the files TLS is made of.
	TLS TLSFiles
	// KeyCache keeps the SCRAM keys of the settings' connections, so a
	// connection again skips their derivation; shared by copies of the
	// settings.
	KeyCache *KeyCache
}

// String gives the settings, never the password.
func (s *Settings) String() string {
	hosts := make([]string, len(s.Hosts))
	for i, h := range s.Hosts {
		hosts[i] = h.String()
	}

	return "Settings{hosts: " + strings.Join(hosts, listSeparator) + ", user: " + s.User + ", tls_mode: " +
		s.TLSMode.String() + ", target: " + s.Target.String() + ", ..}"
}

// GoString gives the settings as String does, never the password.
func (s *Settings) GoString() string { return s.String() }

// Resolve gives the settings explicit gives, then the service it or the
// process's environment names, then the environment, then the defaults.
// It fails with SQLSTATE 08001 if a setting cannot be read, a service
// named cannot be read, or no user is named.
func Resolve(explicit Options) (*Settings, error) {
	return ResolveIn(explicit, os.LookupEnv)
}

// SettingsFromURI gives the settings of the connection URI uri, as
// Resolve lays them.
func SettingsFromURI(uri string) (*Settings, error) {
	options, err := OptionsFromURI(uri)
	if err != nil {
		return nil, err
	}

	return Resolve(options)
}

// ResolveIn gives the settings as Resolve lays them, variable reading the
// environment's variables.
func ResolveIn(explicit Options, variable func(name string) (string, bool)) (*Settings, error) {
	environment := EnvironmentOptions(variable)
	named := explicit.Over(environment)

	var service Options

	if name, ok := named.Get(OptionService); ok {
		file, ok := named.Get(OptionServiceFile)
		if !ok {
			file = atHome(DefaultServiceFile)
		}

		var err error
		if service, err = Service(file, name); err != nil {
			return nil, err
		}
	}

	return settingsOf(explicit.Over(service).Over(environment))
}

// settingsOf gives the settings options give, laid already, the defaults
// for the rest.
func settingsOf(o Options) (*Settings, error) {
	user, ok := o.Get(OptionUser)
	if !ok {
		return nil, invalid("no user is named: by the program, a service, or TURBOREL_USER")
	}

	host, ok := o.Get(OptionHost)
	if !ok {
		host = DefaultHost
	}

	ports, _ := o.Get(OptionPort)

	hosts, err := hostsOf(host, ports)
	if err != nil {
		return nil, err
	}

	s := &Settings{Hosts: hosts, User: user, ConnectTimeout: connectWithin, KeyCache: NewKeyCache()}
	s.Password, s.HasPassword = o.Get(OptionPassword)
	s.ApplicationName, _ = o.Get(OptionApplicationName)

	if s.Passfile, ok = o.Get(OptionPassfile); !ok {
		s.Passfile = atHome(DefaultPassfile)
	}

	s.TLS.Roots, _ = o.Get(OptionTLSRoots)
	s.TLS.Certificate, _ = o.Get(OptionTLSCertificate)
	s.TLS.Key, _ = o.Get(OptionTLSKey)
	s.TLS.ServerName, _ = o.Get(OptionServerName)

	return s, s.readWords(o)
}

// atHome gives the file name in the home directory; empty if there is
// none.
func atHome(name string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return filepath.Join(home, name)
}

// parsePort gives the port text is, the value of key.
func parsePort(key, text string) (uint16, error) {
	n, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(text), "+"), decimalRadix, 16)
	if err != nil {
		return 0, invalid(key + " is " + text + ", no port")
	}

	return uint16(n), nil
}

// seconds gives the time text is, in whole seconds; 0 for no limit.
func seconds(key, text string) (time.Duration, error) {
	n, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(text), "+"), decimalRadix, 64)
	if err != nil || n > uint64(time.Duration(1<<63-1)/time.Second) {
		return 0, invalid(key + " is " + text + ", not a whole number of seconds")
	}

	return time.Duration(n) * time.Second, nil
}

// hostsOf gives the hosts names gives, each of its own port, or the port
// of the same place in ports, or the one port ports gives, or the
// default.
func hostsOf(names, ports string) ([]Host, error) {
	var given []uint16

	for p := range strings.SplitSeq(ports, listSeparator) {
		if strings.TrimSpace(p) == "" {
			continue
		}

		port, err := parsePort(OptionPort, p)
		if err != nil {
			return nil, err
		}

		given = append(given, port)
	}

	fallback := func(i int) uint16 {
		switch {
		case i < len(given):
			return given[i]
		case len(given) == 1:
			return given[0]
		default:
			return DefaultPort
		}
	}

	var hosts []Host

	for i, name := range strings.Split(names, listSeparator) {
		h, err := hostOf(strings.TrimSpace(name), fallback(i))
		if err != nil {
			return nil, err
		}

		hosts = append(hosts, h)
	}

	return hosts, nil
}

// hostOf gives the host name is: a path, of a local socket; [address] or
// [address]:port, of IPv6; name:port; or a name, at port.
func hostOf(name string, port uint16) (Host, error) {
	switch {
	case name == "":
		return Host{}, invalid("a host of the list is empty")
	case strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`):
		return Host{Path: name}, nil
	case strings.HasPrefix(name, "["):
		address, after, ok := strings.Cut(name[1:], "]")
		if !ok {
			return Host{}, invalid(name + " has no ]")
		}

		if p, ok := strings.CutPrefix(after, ":"); ok {
			var err error
			if port, err = parsePort(OptionHost, p); err != nil {
				return Host{}, err
			}
		}

		return Host{Name: address, Port: port}, nil
	}

	// A name of two colons or more is an IPv6 address unbracketed, of no
	// port.
	if h, p, ok := strings.Cut(name, ":"); ok && !strings.Contains(p, ":") {
		port, err := parsePort(OptionHost, p)
		if err != nil {
			return Host{}, err
		}

		return Host{Name: h, Port: port}, nil
	}

	return Host{Name: name, Port: port}, nil
}

// PasswordFor gives the password to prove at host: the settings', else
// the password file's for host, its port and the user, if the file can
// be read.
func (s *Settings) PasswordFor(host Host) (string, bool) {
	if s.HasPassword {
		return s.Password, true
	}

	if s.Passfile == "" {
		return "", false
	}

	text, ok := readRegular(s.Passfile)
	if !ok {
		return "", false
	}

	name, port := host.Name, strconv.Itoa(int(host.Port))
	if host.Path != "" {
		name, port = host.Path, strconv.Itoa(DefaultPort)
	}

	return passwordOf(text, name, port, s.User)
}

// readWords reads the settings that are words or numbers.
func (s *Settings) readWords(o Options) error {
	steps := []func() error{
		func() error { return readChoice(o, OptionTLSMode, tlsModes(), &s.TLSMode) },
		func() error { return readChoice(o, OptionChannelBinding, channelBindings(), &s.ChannelBinding) },
		func() error { return readSeconds(o, &s.ConnectTimeout) },
		func() error { return readChoice(o, OptionTarget, targetRoles(), &s.Target) },
		func() error { return readChoice(o, OptionLoadBalance, loadBalances(), &s.LoadBalance) },
		func() error { return readChoice(o, OptionTLSSystemRoots, booleans(), &s.TLS.SystemRoots) },
	}

	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}

	return nil
}

// readChoice reads key's word, if set, into into.
func readChoice[T comparable](o Options, key string, choices []choice[T], into *T) error {
	text, ok := o.Get(key)
	if !ok {
		return nil
	}

	value, err := parseWord(key, text, choices)
	if err != nil {
		return err
	}

	*into = value

	return nil
}

// readSeconds reads connect_timeout, if set, into into.
func readSeconds(o Options, into *time.Duration) error {
	text, ok := o.Get(OptionConnectTimeout)
	if !ok {
		return nil
	}

	d, err := seconds(OptionConnectTimeout, text)
	if err != nil {
		return err
	}

	*into = d

	return nil
}
