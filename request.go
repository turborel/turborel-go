package turborel

// What the client sends [PROTOCOL Requests], each request a frame of its
// kind. The kinds are read in the direction they travel, so a request's
// kind may be a reply's too.

// protocolVersion is the protocol's version, the first field of Hello
// [PROTOCOL Hello 'H'].
const protocolVersion uint32 = 1

// The kinds of the requests.
const (
	kindHello      byte = 'H'
	kindAnswer     byte = 'A'
	kindQuery      byte = 'Q'
	kindPrepare    byte = 'P'
	kindExecute    byte = 'E'
	kindFetch      byte = 'F'
	kindClose      byte = 'C'
	kindDeallocate byte = 'D'
	kindLobRequest byte = 'L'
	kindCancel     byte = 'Z'
	kindTerminate  byte = 'X'
	kindDescribe   byte = 'Y'
	kindPing       byte = 'G'
	kindReset      byte = 'W'
	kindChangeUser byte = 'U'
	kindCopyIn     byte = 'I'
	kindCopyData   byte = 'J'
	kindCopyEnd    byte = 'N'
)

// request is a message the client sends: it appends itself, framed.
type request interface {
	encode(w *writer)
}

// frame appends a frame of kind, its body written by the function given,
// its length
// filled in once the body is written [PROTOCOL Frames].
func (w *writer) frame(kind byte, body func()) {
	at := len(w.buf)
	w.buf = appendFrameHeader(w.buf, frameHeader{kind: kind})
	body()

	length := count32(len(w.buf) - at - frameHeaderBytes)
	header := appendFrameHeader(nil, frameHeader{kind: kind, length: length})
	copy(w.buf[at:], header)
}

// encodeRequest gives the frame of req.
func encodeRequest(req request) []byte {
	var w writer

	req.encode(&w)

	return w.buf
}

// helloRequest opens a connection that logs in [PROTOCOL Hello 'H'].
type helloRequest struct {
	user         string
	mechanism    string
	initial      []byte
	capabilities []string
	attributes   []Attribute
}

func (q helloRequest) encode(w *writer) {
	w.frame(kindHello, func() {
		w.u32(protocolVersion)
		w.str(q.user)
		w.str(q.mechanism)
		w.bytes(q.initial)
		w.strs(q.capabilities)
		w.attributes(tailAttributes, q.attributes)
	})
}

// answerRequest answers a Challenge [PROTOCOL Answer 'A'].
type answerRequest struct {
	answer []byte
}

func (q answerRequest) encode(w *writer) {
	w.frame(kindAnswer, func() { w.bytes(q.answer) })
}

// queryRequest runs a direct SQL statement [PROTOCOL Query 'Q'].
type queryRequest struct {
	sql        string
	ends       bool
	attributes []Attribute
	multi      bool
}

func (q queryRequest) encode(w *writer) {
	w.frame(kindQuery, func() {
		w.str(q.sql)
		w.flag(q.ends)
		w.attributes(tailAttributes, q.attributes)
		w.mark(tailMulti, q.multi)
	})
}

// prepareRequest prepares a statement under the client's number
// [PROTOCOL Prepare 'P'].
type prepareRequest struct {
	statement uint32
	sql       string
}

func (q prepareRequest) encode(w *writer) {
	w.frame(kindPrepare, func() {
		w.u32(q.statement)
		w.str(q.sql)
	})
}

// executeRequest executes a prepared statement [PROTOCOL Execute 'E'].
type executeRequest struct {
	statement  uint32
	values     []Value
	rows       uint32
	ends       bool
	attributes []Attribute
	cursor     uint32
}

func (q executeRequest) encode(w *writer) {
	w.frame(kindExecute, func() {
		w.u32(q.statement)
		encodeValues(w, q.values)
		w.u32(q.rows)
		w.flag(q.ends)
		w.attributes(tailAttributes, q.attributes)
		w.number(tailCursor, q.cursor)
	})
}

// fetchRequest sends more rows of an open cursor [PROTOCOL Fetch 'F'].
type fetchRequest struct {
	statement uint32
	rows      uint32
	cursor    uint32
}

func (q fetchRequest) encode(w *writer) {
	w.frame(kindFetch, func() {
		w.u32(q.statement)
		w.u32(q.rows)
		w.number(tailCursor, q.cursor)
	})
}

// closeRequest closes an open cursor [PROTOCOL Close 'C'].
type closeRequest struct {
	statement uint32
	cursor    uint32
}

func (q closeRequest) encode(w *writer) {
	w.frame(kindClose, func() {
		w.u32(q.statement)
		w.number(tailCursor, q.cursor)
	})
}

// deallocateRequest destroys a prepared statement [PROTOCOL Deallocate
// 'D'].
type deallocateRequest struct {
	statement uint32
}

func (q deallocateRequest) encode(w *writer) {
	w.frame(kindDeallocate, func() { w.u32(q.statement) })
}

// cancelRequest cancels the statement another connection runs, by the
// key its Ready gave [PROTOCOL Cancel 'Z'].
type cancelRequest struct {
	key [keyBytes]byte
}

func (q cancelRequest) encode(w *writer) {
	w.frame(kindCancel, func() { w.buf = append(w.buf, q.key[:]...) })
}

// terminateRequest ends the session [PROTOCOL Terminate 'X'].
type terminateRequest struct{}

func (terminateRequest) encode(w *writer) {
	w.frame(kindTerminate, func() {})
}

// The two things Describe describes [PROTOCOL Describe 'Y'].
const (
	describeStatement byte = 0
	describeCursor    byte = 1
)

// describeRequest describes a prepared statement, or an open cursor of
// one [PROTOCOL Describe 'Y'].
type describeRequest struct {
	ofCursor  bool
	statement uint32
	cursor    uint32
}

func (q describeRequest) encode(w *writer) {
	what, cursor := describeStatement, uint32(0)
	if q.ofCursor {
		what, cursor = describeCursor, q.cursor
	}

	w.frame(kindDescribe, func() {
		w.u8(what)
		w.u32(q.statement)
		w.u32(cursor)
	})
}

// pingRequest asks for a Pong [PROTOCOL Ping 'G'].
type pingRequest struct{}

func (pingRequest) encode(w *writer) {
	w.frame(kindPing, func() {})
}

// resetRequest puts the session back as it was at login [PROTOCOL Reset
// 'W'].
type resetRequest struct{}

func (resetRequest) encode(w *writer) {
	w.frame(kindReset, func() {})
}

// changeUserRequest authenticates the connection again [PROTOCOL
// ChangeUser 'U'].
type changeUserRequest struct {
	user      string
	mechanism string
	initial   []byte
}

func (q changeUserRequest) encode(w *writer) {
	w.frame(kindChangeUser, func() {
		w.str(q.user)
		w.str(q.mechanism)
		w.bytes(q.initial)
	})
}

// copyInRequest begins a copy of rows into a table [PROTOCOL CopyIn
// 'I'].
type copyInRequest struct {
	table   string
	columns []string
	ends    bool
}

func (q copyInRequest) encode(w *writer) {
	w.frame(kindCopyIn, func() {
		w.str(q.table)
		w.strs(q.columns)
		w.flag(q.ends)
	})
}

// copyDataRequest sends rows of the copy [PROTOCOL CopyData 'J'].
type copyDataRequest struct {
	rows []Row
}

func (q copyDataRequest) encode(w *writer) {
	w.frame(kindCopyData, func() {
		w.u32(count32(len(q.rows)))

		for _, row := range q.rows {
			encodeValues(w, row)
		}
	})
}

// copyEndRequest ends the copy, or abandons it [PROTOCOL CopyEnd 'N'].
type copyEndRequest struct {
	abandon bool
}

func (q copyEndRequest) encode(w *writer) {
	w.frame(kindCopyEnd, func() { w.flag(q.abandon) })
}
