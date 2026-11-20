# Appendix D: Versions

[Contents](README.md)

The driver has no version of its own: each release carries the number
of the turborel release it is made for, the server's own, so a turborel
release and the driver's release share the number. This one is 0.0.0,
the alpha, as turborel's is, and the program reads it as
`turborel.Version`. While the version is 0.y.z, the driver's API and
behaviour may change in any release. The alpha speaks the native
protocol's version 1, the only version, which grows by capabilities
rather than by new versions. [VERSIONING.md](../../VERSIONING.md) says
how a release is published.
