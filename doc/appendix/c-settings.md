# Appendix C: Settings

[Contents](README.md)

Each setting has one name in the program's `Options`, a connection URI's
query and the service file; ten are read from the environment too. A
setting none of the layers gives takes its default. `Option` constants
name each, as `OptionTLSMode`.

| Setting | Variable | What it says | Default |
|---|---|---|---|
| `host` | TURBOREL_HOST | The servers, comma-separated, each `name`, `name:port`, `[IPv6]`, `[IPv6]:port`, or a path beginning `/` or `\`, a Unix socket or named pipe | `localhost` |
| `port` | TURBOREL_PORT | One port for every host, or one each in order | 7422 |
| `user` | TURBOREL_USER | Who connects | none: it must be given |
| `password` | TURBOREL_PASSWORD | The user's password | the password file's |
| `tls_mode` | TURBOREL_TLS_MODE | `disable`, `prefer`, `require`, `verify-ca` or `verify-full` | `prefer` |
| `channel_binding` | | `disable`, `prefer` or `require` | `prefer` |
| `application_name` | TURBOREL_APPLICATION_NAME | The program's name, sent as the connection attribute `application` | none |
| `connect_timeout` | TURBOREL_CONNECT_TIMEOUT | Whole seconds each step of connecting waits, 0 for no limit | 30 |
| `service` | TURBOREL_SERVICE | The service file's section to read | none |
| `service_file` | TURBOREL_SERVICE_FILE | The service file | `~/.turborel_service.conf` |
| `passfile` | TURBOREL_PASSFILE | The password file | `~/.turborel_pass` |
| `target` | | `any`, `read-write` or `read-only` | `any` |
| `load_balance` | | `disable` or `random` | `disable` |
| `tls_roots` | | A PEM file of the authorities trusted | none |
| `tls_system_roots` | | `true` to trust the platform's authorities too | `false` |
| `tls_certificate` | | A PEM file of the client's certificate chain, presented | none |
| `tls_key` | | A PEM file of its private key | none |
| `server_name` | | The name the server's certificate is checked against | the host's |

A word, a number or a path that cannot be read is refused with SQLCODE
-919, SQLSTATE 08001. A service file's section cannot set `service` or
`service_file`. The settings of a pool are listed at milestone M6.
