package turborel

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"maps"
	"math"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// samplesFile is Turborel's doc/client/05-protocol-frames.tsv, copied at
// the commit CONFORMANCE.md names (RULES.md D15).
const samplesFile = "testdata/05-protocol-frames.tsv"

// sample is a line of the sample frames: the side that sends it, its
// name, the capabilities it needs, the whole frame and what it means.
type sample struct {
	direction string
	name      string
	needs     []string
	frame     []byte
	means     string
}

// sampleOf reads a line of five fields apart by tabs, as the page lays it
// out, failing the test of one that is not.
func sampleOf(t *testing.T, line string) sample {
	t.Helper()

	fields := strings.Split(line, "\t")
	if len(fields) != 5 {
		t.Fatalf("a line of %d fields: %q", len(fields), line)
	}

	frame, err := hex.DecodeString(fields[3])
	if err != nil || strings.ToUpper(fields[3]) != fields[3] || len(frame) < frameHeaderBytes {
		t.Fatalf("%s: its hex %v", fields[1], err)
	}

	s := sample{direction: fields[0], name: fields[1], frame: frame, means: fields[4]}
	if fields[2] != "-" {
		s.needs = strings.Split(fields[2], ",")
	}

	return s
}

// readSamples reads every line of the sample frames but its comments.
func readSamples(t *testing.T) []sample {
	t.Helper()

	file, err := os.Open(samplesFile)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = file.Close() })

	var samples []sample

	lines := bufio.NewScanner(file)
	lines.Buffer(nil, 1<<20)

	for lines.Scan() {
		if !strings.HasPrefix(lines.Text(), "#") {
			samples = append(samples, sampleOf(t, lines.Text()))
		}
	}

	if err := lines.Err(); err != nil {
		t.Fatal(err)
	}

	return samples
}

// TestSampleFramesRead reads every line of the sample frames.
//
// Verifies: [PROTOCOL Sample frames] every line but a comment has five
// fields apart by tabs: a direction of client or server, a name unique
// in the file, the capabilities it needs, each one of the protocol's, or
// `-`, the whole frame in upper-case hexadecimal, its header's length
// that of its body, and its meaning [PROTOCOL Frames] [PROTOCOL
// Capabilities].
func TestSampleFramesRead(t *testing.T) {
	t.Parallel()

	samples := readSamples(t)
	names := map[string]bool{}
	sides := map[string]int{}

	for _, s := range samples {
		if names[s.name] || s.means == "" {
			t.Errorf("%s: twice, or meaning nothing", s.name)
		}

		names[s.name] = true
		sides[s.direction]++

		for _, need := range s.needs {
			if !slices.Contains(Capabilities(), need) {
				t.Errorf("%s needs %q, no capability", s.name, need)
			}
		}

		var header [frameHeaderBytes]byte

		copy(header[:], s.frame)

		h, err := decodeFrameHeader(header, maxBody)
		if err != nil || int(h.length) != len(s.frame)-frameHeaderBytes {
			t.Errorf("%s: a header of %d for a body of %d", s.name, h.length, len(s.frame)-frameHeaderBytes)
		}
	}

	if len(samples) != 148 || sides["client"] != 38 || sides["server"] != 110 {
		t.Errorf("%d samples, %v", len(samples), sides)
	}
}

// TestSampleFramesDecoded decodes every server frame.
//
// Verifies: [PROTOCOL Sample frames] every frame a server sends read as
// a frame from a stream, inflated if compressed, and decoded to the reply
// its meaning gives, -0 of its sign bit, whatever capabilities it needs,
// as the tail of each is read where it comes [PROTOCOL Replies] [PROTOCOL The tail]
// [PROTOCOL Values] [PROTOCOL Data types] [PROTOCOL Declared types]
// [PROTOCOL Parameter modes].
func TestSampleFramesDecoded(t *testing.T) {
	t.Parallel()

	want := expectedReplies(t)

	for _, s := range readSamples(t) {
		if s.direction != "server" {
			continue
		}

		kind, body, err := readFrame(bytes.NewReader(s.frame), maxBody, true)
		if err != nil {
			t.Errorf("%s: %v", s.name, err)

			continue
		}

		got, err := decodeReply(kind, body)
		if expected, ok := want[s.name]; err != nil || !ok || !reflect.DeepEqual(got, expected) {
			t.Errorf("%s (%s): decoded %#v, %v", s.name, s.means, got, err)
		}

		delete(want, s.name)

		if s.name == "value-approximate-negative-zero" {
			assertNegativeZero(t, got)
		}
	}

	for name := range want {
		t.Errorf("no sample %s", name)
	}
}

// assertNegativeZero checks rep's one value is -0, its sign bit set,
// which reflect.DeepEqual does not tell from 0.
func assertNegativeZero(t *testing.T, rep reply) {
	t.Helper()

	rows, _ := rep.(rowsReply)
	if zero, ok := rows.rows[0][0].(Approx); !ok || !math.Signbit(float64(zero)) {
		t.Errorf("-0 read as %v", rows.rows[0][0])
	}
}

// TestSampleFramesEncoded encodes every client frame.
//
// Verifies: [PROTOCOL Sample frames] every frame a client sends encoded
// by the driver's own requests byte for byte as the file holds it, a
// compressed one but, which TestSampleFramesInflated reads [PROTOCOL
// Requests] [PROTOCOL The tail].
func TestSampleFramesEncoded(t *testing.T) {
	t.Parallel()

	want := expectedRequests()

	for _, s := range readSamples(t) {
		if s.direction != "client" || s.frame[0] == kindCompressed {
			continue
		}

		req, ok := want[s.name]
		if got := encodeRequest(req); !ok || !bytes.Equal(got, s.frame) {
			t.Errorf("%s (%s): encoded %X", s.name, s.means, got)
		}

		delete(want, s.name)
	}

	for name := range want {
		t.Errorf("no sample %s", name)
	}
}

// heldOf is what a compressed sample's meaning says it holds: the kind
// and length of its frame.
var heldOf = regexp.MustCompile(`kind (\S), length (\d+)`)

// TestSampleFramesInflated inflates every compressed frame.
//
// Verifies: [PROTOCOL Sample frames] a compressed frame's sample inflated
// to the kind and length its meaning names, the server's to the Rows its
// meaning gives, the client's to the Query the driver encodes; the
// driver's own compressed Query, of another zlib stream, inflated to the
// same frame [PROTOCOL Compressed frames].
func TestSampleFramesInflated(t *testing.T) {
	t.Parallel()

	compressed := 0

	for _, s := range readSamples(t) {
		if s.frame[0] != kindCompressed {
			continue
		}

		compressed++

		kind, body, err := decompress(s.frame[frameHeaderBytes:], maxBody)
		held := heldOf.FindStringSubmatch(s.means)

		if err != nil || held == nil || string(kind) != held[1] || strconv.Itoa(len(body)) != held[2] {
			t.Errorf("%s (%s): inflated %c of %d, %v", s.name, s.means, kind, len(body), err)
		}

		if s.direction == "client" {
			assertQueryInflated(t, kind, body)
		}
	}

	if compressed != 2 {
		t.Errorf("%d compressed samples", compressed)
	}
}

// assertQueryInflated checks the compressed client sample holds the
// Query the driver encodes, and the driver's compressed one too.
func assertQueryInflated(t *testing.T, kind byte, body []byte) {
	t.Helper()

	frame := encodeRequest(compressedQuery())
	if kind != kindQuery || !bytes.Equal(body, frame[frameHeaderBytes:]) {
		t.Errorf("the sample holds %c %q", kind, body)
	}

	ours := compressFrames(frame, defaultCompressionThreshold)

	kind, held, err := decompress(ours[frameHeaderBytes:], maxBody)
	if ours[0] != kindCompressed || err != nil || kind != kindQuery || !bytes.Equal(held, frame[frameHeaderBytes:]) {
		t.Errorf("the driver's compressed Query holds %c %q, %v", kind, held, err)
	}
}

// compressedQuery is the Query the compressed client sample holds.
func compressedQuery() request {
	return queryRequest{sql: "SELECT '" + strings.Repeat("x", 2000) + "' FROM T", ends: true}
}

// expectedRequests is the request of each client sample, as its meaning
// gives it.
func expectedRequests() map[string]request {
	trust, every := "trust", []string{
		CapabilityStatus, CapabilityDescribe, CapabilityCursors, CapabilityCompression, CapabilityDeclaredTypes,
	}
	most := make([]Attribute, maxAttributes)

	for i := range most {
		most[i] = Attribute{Name: "a" + strconv.Itoa(i)}
	}

	requests := map[string]request{
		"hello-ping": helloRequest{user: "ANN", mechanism: trust, capabilities: []string{CapabilityPing}},
		"hello-none": helloRequest{user: "ANN", mechanism: trust},
		"hello-every": helloRequest{
			user: "ANN", mechanism: trust, capabilities: every,
			attributes: []Attribute{{AttributeApplication, "app"}},
		},
		"hello-attribute-longest": helloRequest{user: "ANN", mechanism: trust, attributes: []Attribute{{
			strings.Repeat("a", maxAttributeName), strings.Repeat("v", maxAttributeValue),
		}}},
		"hello-attributes-most": helloRequest{user: "ANN", mechanism: trust, attributes: most},
		"hello-scram":           helloRequest{user: "ANN", mechanism: "scram-sha-256", initial: []byte("n,,n=,r=abc")},
		"answer":                answerRequest{answer: []byte("c=biws,r=abc,p=xyz")},
		"answer-empty":          answerRequest{},
		"cancel":                cancelRequest{key: [keyBytes]byte(bytes.Repeat([]byte{0xA5}, keyBytes))},
		"query":                 queryRequest{sql: "VALUES 1", ends: true},
		"query-empty":           queryRequest{},
		"query-attributes": queryRequest{
			sql: "VALUES QUERY_ATTRIBUTE('trace')", attributes: []Attribute{{"trace", "t1"}},
		},
		"query-multi": queryRequest{sql: "VALUES 1; VALUES 2", ends: true, multi: true},
	}

	maps.Copy(requests, statementRequests())
	maps.Copy(requests, otherRequests())

	return requests
}

// statementRequests is the request of each client sample of a prepared
// statement and its cursors.
func statementRequests() map[string]request {
	return map[string]request{
		"prepare": prepareRequest{statement: 1, sql: "SELECT K FROM T WHERE K > ?"},
		"execute": executeRequest{statement: 1, values: []Value{Null{}, Char(""), Bool(true)}, rows: 2},
		"execute-cursor": executeRequest{
			statement: 1, ends: true, attributes: []Attribute{{"a", "b"}}, cursor: 7,
		},
		"fetch":              fetchRequest{statement: 1, rows: 100},
		"fetch-cursor":       fetchRequest{statement: 1, rows: 1<<32 - 1, cursor: 7},
		"close":              closeRequest{statement: 1},
		"close-cursor":       closeRequest{statement: 1, cursor: 7},
		"deallocate":         deallocateRequest{statement: 1<<32 - 1},
		"describe-statement": describeRequest{statement: 1},
		"describe-cursor":    describeRequest{ofCursor: true, statement: 1, cursor: 7},
	}
}

// otherRequests is the request of each client sample of large objects,
// copies and the session.
func otherRequests() map[string]request {
	return map[string]request{
		"terminate":            terminateRequest{},
		"lob-read":             lobReadRequest{locator: 3, count: 1 << 16, characters: true},
		"lob-create":           lobCreateRequest{kind: BinaryLOB},
		"lob-append":           lobAppendRequest{locator: 4, octets: []byte{0xC3}},
		"lob-free":             lobFreeRequest{locator: 4},
		"lob-hold":             lobHoldRequest{locator: 3},
		"ping":                 pingRequest{},
		"reset":                resetRequest{},
		"change-user":          changeUserRequest{user: "BOB", mechanism: "password", initial: []byte("secret")},
		"copy-in":              copyInRequest{table: "S.T", columns: []string{"ID", "NAME"}, ends: true},
		"copy-in-every-column": copyInRequest{table: "T"},
		"copy-data": copyDataOf([]Row{
			{Char("1"), Null{}}, {Char("2"), Char("b")},
		}),
		"copy-end":         copyEndRequest{},
		"copy-end-abandon": copyEndRequest{abandon: true},
	}
}
