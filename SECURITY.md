# Security

Report a vulnerability privately, never in a public issue: on the
repository's **Security** tab, choose **Report a vulnerability**. Only the
maintainers see the report.

What counts, for a driver:

- TLS not used, or not verified, where it was asked for, as by a
  `tls_mode` of `require`, `verify-ca` or `verify-full`, or channel
  binding asked for and not done;
- a password, a SCRAM key or anything else derived from a password, or a
  session's cancel key, disclosed: sent where it should not go, such as
  over plain TCP, or written to an error, a log or a string;
- a server's reply, or a file the driver reads such as a service or
  password file, that can crash the client, run code, or take its
  memory without bound.

A wrong value decoded, a hang with no attacker, or a crash only the
program's own misuse can cause is a bug: report it with the **Bug** form,
its impact in the title (CONTRIBUTING.md, Reporting issues). A
vulnerability of the server is reported to
[turborel](https://github.com/turborel/turborel/security).

Say in the report:

- what an attacker needs, such as a server of their own, a place on the
  network between client and server, or a file the client reads;
- what they gain;
- the commit you tested (`git rev-parse --short=12 HEAD`);
- the smallest steps that show it.

You will hear back within a week. The fix is made in private and
published with an advisory that credits you, unless you ask not to be
named.
