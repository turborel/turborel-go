# Appendix B: Errors

[Contents](README.md)

This appendix will map the SQLSTATE classes to the driver's errors,
and list every failure the driver finds itself, with its SQLCODE and
SQLSTATE. Today there is one: a frame whose body is longer than the
longest taken, 16 MiB, fails the connection with SQLCODE -919 and
SQLSTATE 08006. The rest is written as the codec and statements are
built, milestones M1 and M3.
