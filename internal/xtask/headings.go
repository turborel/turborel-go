package main

// The headings a test or a comment may cite (RULES.md D6, R-TRACE-1,
// R-TRACE-2), every heading of the three documents, their titles among
// them, as Turborel's main branch has them at commit b48f85b2. A citation of a
// heading not listed fails `verify`, so none is guessed; a change to
// the spec's headings changes these lists in the same commit.

// protocolHeadings are the headings of doc/client/05-protocol.md, The
// Native Protocol, cited as `[PROTOCOL Frames]`.
const protocolHeadings = `The Native Protocol
Frames
Compressed frames
Values and fields
Data types
Values
JSON values
Composite fields
The tail
Opening a connection
Mechanisms
Capabilities
Requests
Hello 'H'
Answer 'A'
Query 'Q'
Prepare 'P'
Execute 'E'
Fetch 'F'
Close 'C'
Deallocate 'D'
Lob 'L'
Cancel 'Z'
Terminate 'X'
Describe 'Y'
Ping 'G'
Reset 'W'
ChangeUser 'U'
CopyIn 'I'
CopyData 'J'
CopyEnd 'N'
Replies
Challenge 'C'
Ready 'R'
Prepared 'P'
Description 'D'
Rows 'B'
Suspended 'S'
Complete 'K'
Error 'E'
Notice 'N'
Status 'T'
Pong 'G'
Conversations
Errors and notices
A worked example`

// clientHeadings are the headings of doc/client/02-library.md, The
// Client Library, and doc/client/02-library-connections.md, Settings,
// Pipelines and Pools, cited as `[CLIENT Pipelines]`. No heading is in
// both.
const clientHeadings = `The Client Library
Connecting
Timeouts
Running statements
Prepared statements
Large objects
XML values
UUIDs
Importing
What a connection asks for
What the server tells
Controlling the session
Copying and exporting
The SQL-agent
Connecting by settings
Where settings come from
The connection URI
The service file
The password file
Several servers
TLS by mode
Pipelines
Pools
Settings, Pipelines and Pools
The SCRAM key cache`
