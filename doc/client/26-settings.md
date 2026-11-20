# Settings

[Contents](README.md)

## NAME

`Settings`, `Options`, `Settings.Connect`: connecting as settings from
outside the program say.

## SYNOPSIS

```go
type Options struct{ /* unexported */ }
func (o *Options) Set(key, value string) error
func (o Options) Get(key string) (string, bool)
func (o Options) Over(lower Options) Options
func (o Options) IsEmpty() bool
func OptionsFromURI(uri string) (Options, error)
func EnvironmentOptions(variable func(name string) (string, bool)) Options
func Service(file, name string) (Options, error)
func OptionKeys() []string
func EnvironmentVariables() [][2]string

type Host struct {
	Name string
	Port uint16
	Path string
}
type TLSFiles struct {
	Roots       string
	SystemRoots bool
	Certificate string
	Key         string
	ServerName  string
}
type Settings struct {
	Hosts           []Host
	User            string
	Password        string
	HasPassword     bool
	TLSMode         TLSMode
	ChannelBinding  ChannelBinding
	ApplicationName string
	ConnectTimeout  time.Duration
	Passfile        string
	Target          TargetRole
	LoadBalance     LoadBalance
	TLS             TLSFiles
	KeyCache        *KeyCache
}
func Resolve(explicit Options) (*Settings, error)
func ResolveIn(explicit Options, variable func(name string) (string, bool)) (*Settings, error)
func SettingsFromURI(uri string) (*Settings, error)
func (s *Settings) PasswordFor(host Host) (string, bool)
func (s *Settings) Connect(ctx context.Context) (*Conn, error)

type TLSMode uint8     // TLSPrefer, TLSDisable, TLSRequire, TLSVerifyCA, TLSVerifyFull
type TargetRole uint8  // TargetAny, TargetReadWrite, TargetReadOnly
type LoadBalance uint8 // LoadBalanceDisable, LoadBalanceRandom
type Role uint8        // RoleReadWrite, RoleReadOnly, RoleUnknown
func (c *Conn) Role(ctx context.Context) (Role, error)
```

## DESCRIPTION

`Resolve` lays the program's `Options` over a service's section, the
service named by them or by TURBOREL_SERVICE, over the environment, read
by `os.LookupEnv`, over the defaults; `ResolveIn` reads the environment
through the program's function. A variable set but empty gives nothing.
`Options.Set` refuses a name no setting has. `String` and `GoString` of
both never show the password.

`Settings.Connect` tries each host, in order or shuffled under
`LoadBalanceRandom`, each address its name resolves to, a host whose
`Path` is set being its Unix socket or named pipe. Over TCP it opens TLS
as `TLSMode` says, under `TLSPrefer` connecting again over plain TCP to
a server whose handshake fails. It proves a password, the settings' or
`PasswordFor`'s, by SCRAM with the settings' channel binding and key
cache; with none, the certificate `TLS.Certificate` names, else trust.
It asks for every capability but compression and, if named, the
connection attribute `application`. It keeps a server of the role
`Target` asks: `Conn.Role` reads SERVER_READ_ONLY, or of a server that
does not tell it, runs `RoleQuery` as its own transaction, a server
with no INFORMATION_SCHEMA.REPLICAS taking changes.

## ERRORS

| SQLSTATE | When |
|---|---|
| 08001 | SQLCODE -919: a setting that cannot be read; a service file unread, malformed, or without the section; no user; no server kept, the message naming each address and why |
| 28000 | SQLCODE -919: channel binding required by a connection that cannot bind; a password over plain TCP; the last server tried refused the user |
| 08006 | SQLCODE -919: a TLS file that cannot be read; verify-ca or verify-full with no authority trusted; a client certificate with no key |

Of one address tried, its own failure is given.

## EXAMPLES

The examples of [Settings](../user/09-settings.md).

## STANDARD

[CLIENT Where settings come from], [CLIENT The connection URI], [CLIENT The service file], [CLIENT The password file], [CLIENT Several servers], [CLIENT TLS by mode] and [CLIENT The SCRAM key cache] of [Settings, Pipelines and Pools](https://github.com/turborel/turborel/blob/main/doc/client/02-library-connections.md).

## SEE ALSO

[Appendix C](../appendix/c-settings.md), [Connections](21-connections.md), [Pools](28-pools.md).
