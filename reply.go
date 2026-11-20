package turborel

// What the server sends [PROTOCOL Replies], each reply a frame of its
// kind, decoded from its body. A kind no reply has, a body cut short, or
// bytes left over fail the connection [PROTOCOL Frames].

import "fmt"

// The kinds of the replies.
const (
	kindChallenge   byte = 'C'
	kindReady       byte = 'R'
	kindPrepared    byte = 'P'
	kindDescription byte = 'D'
	kindRows        byte = 'B'
	kindSuspended   byte = 'S'
	kindComplete    byte = 'K'
	kindError       byte = 'E'
	kindLobReply    byte = 'L'
	kindNotice      byte = 'N'
	kindStatus      byte = 'T'
	kindPong        byte = 'G'
)

// reply is a message the server sends.
type reply interface {
	isReply()
}

// challengeReply is the next step of an authentication exchange
// [PROTOCOL Challenge 'C'].
type challengeReply struct {
	challenge []byte
}

// readyReply says the client is authenticated [PROTOCOL Ready 'R'].
type readyReply struct {
	authorization string
	key           [keyBytes]byte
	capabilities  []string
	server        string
}

// preparedReply is a statement prepared, or described [PROTOCOL Prepared
// 'P'].
type preparedReply struct {
	statement  uint32
	parameters []Parameter
	columns    []Column
}

// descriptionReply is the columns of the rows that follow [PROTOCOL
// Description 'D'].
type descriptionReply struct {
	columns []Column
}

// rowsReply is a batch of rows [PROTOCOL Rows 'B'].
type rowsReply struct {
	rows []Row
}

// suspendedReply says the cursor has more rows [PROTOCOL Suspended 'S'].
type suspendedReply struct{}

// completeReply is a statement's completion [PROTOCOL Complete 'K'].
type completeReply struct {
	completion Completion
}

// errorReply is a statement's failure, or the connection's [PROTOCOL
// Error 'E'].
type errorReply struct {
	failure *Error
}

// noticeReply is a condition a statement raised, or a notice of the
// server's own [PROTOCOL Notice 'N'].
type noticeReply struct {
	notice Notice
}

// statusReply is a setting of the session [PROTOCOL Status 'T'].
type statusReply struct {
	name  string
	value string
}

// pongReply answers Ping [PROTOCOL Pong 'G'].
type pongReply struct{}

func (challengeReply) isReply()   {}
func (readyReply) isReply()       {}
func (preparedReply) isReply()    {}
func (descriptionReply) isReply() {}
func (rowsReply) isReply()        {}
func (suspendedReply) isReply()   {}
func (completeReply) isReply()    {}
func (errorReply) isReply()       {}
func (lobDataReply) isReply()     {}
func (lobLocatorReply) isReply()  {}
func (noticeReply) isReply()      {}
func (statusReply) isReply()      {}
func (pongReply) isReply()        {}

// Completion is a statement's completion [PROTOCOL Complete 'K']: its
// SQLCODE, 0 or 100, its SQLSTATE, whether a transaction is active after
// it, and what its tail told, each as the capability that adds it was
// granted.
type Completion struct {
	SQLCode  int32
	SQLState string
	Active   bool
	// Position is the log position of the commit the statement made, if
	// Committed.
	Position  uint64
	Committed bool
	// More says another statement's replies follow, of a Query of
	// several.
	More bool
	// Rows is the rows the statement counted, if Counted.
	Rows    uint64
	Counted bool
	// Generated is each identity value an INSERT generated, as SQL
	// writes it.
	Generated []string
	// Conditions is each condition the statement raised, in order.
	Conditions []*Error
	// Time is the server's time for the statement, nil if not told.
	Time *Timing
}

// Timing is the server's time for a statement, in microseconds: total,
// from when a worker began it until its completion was ready; queued,
// waiting for a worker before that; locks, the part of total spent
// waiting for locks.
type Timing struct {
	Total  uint64
	Queued uint64
	Locks  uint64
}

// Notice is a condition a statement raised, of class 01, or a notice of
// the server's own, with what the server told of it.
type Notice struct {
	SQLState    string
	Message     string
	Diagnostics Diagnostics
}

// decodeReply decodes the reply of kind whose body is body.
func decodeReply(kind byte, body []byte) (reply, error) {
	r := newReader(body, "reply")

	rep := r.reply(kind)
	if err := r.finish(); err != nil {
		return nil, err
	}

	if rep == nil {
		return nil, broken(fmt.Sprintf("no reply is of kind %d", kind))
	}

	return rep, nil
}

// reply reads the body of a reply of kind; nil for a kind no reply has.
func (r *reader) reply(kind byte) reply {
	switch kind {
	case kindChallenge:
		return challengeReply{challenge: r.bytes()}
	case kindReady:
		return readyReply{authorization: r.str(), key: r.key(), capabilities: r.strs(), server: r.str()}
	case kindPrepared:
		return preparedReply{statement: r.u32(), parameters: r.parameters(), columns: r.describedColumns()}
	case kindDescription:
		return descriptionReply{columns: r.describedColumns()}
	case kindRows:
		return rowsReply{rows: r.rows()}
	case kindSuspended:
		return suspendedReply{}
	case kindComplete:
		return completeReply{completion: r.completion()}
	case kindError:
		return errorReply{failure: r.failure()}
	case kindLobReply:
		return r.lobReply()
	case kindNotice:
		return noticeReply{notice: r.notice()}
	case kindStatus:
		return statusReply{name: r.str(), value: r.str()}
	case kindPong:
		return pongReply{}
	default:
		r.at = len(r.buf)

		return nil
	}
}

// completion reads a Complete's body and its tail [PROTOCOL Complete
// 'K'].
func (r *reader) completion() Completion {
	c := Completion{SQLCode: signed32(r.u32()), SQLState: r.str(), Active: r.flag()}

	t := tail{taken: []byte{tailPosition, tailMore, tailRows, tailGenerated, tailConditions, tailTime}}
	for tag, ok := t.next(r); ok; tag, ok = t.next(r) {
		switch tag {
		case tailPosition:
			c.Position, c.Committed = r.u64(), true
		case tailMore:
			c.More = true
		case tailRows:
			c.Rows, c.Counted = r.u64(), true
		case tailGenerated:
			c.Generated = r.strs()
		case tailConditions:
			c.Conditions = r.conditions()
		default:
			c.Time = &Timing{Total: r.u64(), Queued: r.u64(), Locks: r.u64()}
		}
	}

	return c
}

// conditions reads tag 17: a list, each a failure as bytes, so its own
// tail ends where its length does.
func (r *reader) conditions() []*Error {
	n := r.count()

	var list []*Error
	for i := uint32(0); r.more(i, n); i++ {
		inner := newReader(r.bytes(), "condition")
		if r.err != nil {
			break
		}

		condition := inner.failure()
		if err := inner.finish(); err != nil {
			r.failValue(corruptValue("a condition is malformed: " + asError(err).Message))

			break
		}

		list = append(list, condition)
	}

	return list
}

// notice reads a Notice: its SQLSTATE, message and diagnostics.
func (r *reader) notice() Notice {
	n := Notice{SQLState: r.str(), Message: r.str()}
	if d := r.diagnostics(); d != nil {
		n.Diagnostics = *d
	}

	return n
}
