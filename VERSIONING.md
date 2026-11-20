# Versioning

How turborel-go's releases are numbered, and how one is published.

[Release numbers](#release-numbers) · [Alpha](#alpha) ·
[The protocol](#the-protocol) · [Where the version is](#where-the-version-is) ·
[Publishing](#publishing)

---

## Release numbers

The driver has no version scheme of its own. A release of it carries the
number of the turborel release it is made for, the server's own, so a
turborel release and each driver's release share the number (RULES.md
D8). turborel numbers its releases **MAJOR.MINOR.PATCH**, as in
[Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html), and
says what each kind of release may change in its own
[VERSIONING.md](https://github.com/turborel/turborel/blob/main/VERSIONING.md).
A driver of a release and a server of the same release always work
together.

A fix to the driver alone waits for turborel's next release, whose number
it then takes.

## Alpha

turborel-go is in alpha, version **0.0.0**, as turborel is, until
turborel's first release. While its version is 0.y.z nothing is
promised: its API and behaviour may change in any build.

## The protocol

The alpha speaks the native protocol's version 1, the only version, as
turborel 0.0.0 does. The protocol grows by capabilities, never by a new
version, and the driver asks for those it has as it connects.

## Where the version is

- `turborel.Version`, the constant in `version.go`, is the number.
- The module's git tag, `v` and the number, as `v0.0.0`, once a release
  is published. Go modules carry no version in `go.mod`.

## Publishing

Go has no registry to upload to: the Go module proxy, proxy.golang.org,
takes a module's versions from its git tags. Nothing is published until
every turborel driver is done, and then the owner publishes them
together. Until then no version tag is pushed.

A release is published from a clean checkout of a pushed commit of main
that has passed `go run ./internal/xtask verify` and the CI workflow
`verify`:

1. Run the workflow `release` by hand (Actions, release, Run workflow).
   It builds and checks the module and publishes nothing.
2. Set `Version` to the turborel release's number, in its own commit.
3. Tag that commit `v<number>`, signed, and push the tag.
4. Ask the proxy for the version, so pkg.go.dev lists it:
   `GOPROXY=https://proxy.golang.org go list -m github.com/turborel/turborel-go@v<number>`.

The workflow `release` holds steps 3 and 4, left disabled until the
owner enables them. A version the proxy has fetched stays in it for
good; a release found wrong is retracted by a `retract` line in `go.mod`
in the next release, never retagged.
