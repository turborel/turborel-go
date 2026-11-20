package turborel

// A reply the driver cannot decode [PROTOCOL Values and fields]: a frame
// itself malformed, cut short, with bytes after its last field or of a
// kind no reply has, loses the connection (SQLSTATE 08006); a frame read
// whole that holds a value the driver cannot take fails its request alone,
// with the value's own failure, of no class 08, once the rest of the
// request's replies are read past, so the next request takes its own,
// and the connection goes on [CLIENT Running statements].

// undecoded takes a frame of kind and body that did not decode, for err:
// a malformed one loses the connection; one holding a value the driver
// cannot take has its Complete's head noted and, unless its request is
// answered by it alone, the rest of its request's replies read past.
func (c *Conn) undecoded(kind byte, body []byte, err error) error {
	failure := asError(err)
	if endsConnection(failure) {
		return c.loseBy(failure)
	}

	c.skipped(kind, body)

	if !c.answeredByOne {
		if lost := c.drained(kind, body); lost != nil {
			return lost
		}
	}

	return failure
}

// skipped notes what the head of a Complete that did not decode says, as
// its whole would.
func (c *Conn) skipped(kind byte, body []byte) {
	if head, ok := completedHead(kind, body); ok {
		c.noted(completeReply{completion: head})
	}
}

// drained reads past the rest of a request's replies after one, of kind
// and body, held a value the driver cannot take: up to the one that ends
// them, each taken as receive takes it, or skipped as that one was. It
// gives the connection's failure if one comes before the last.
func (c *Conn) drained(kind byte, body []byte) error {
	for !endsRequest(kind, body) {
		var err error

		kind, body, err = readFrame(c.r, c.maxBody, c.compressing)
		if err != nil {
			return c.failed(err)
		}

		rep, err := decodeReply(kind, body)
		if err != nil {
			if failure := asError(err); endsConnection(failure) {
				return c.loseBy(failure)
			}

			c.skipped(kind, body)

			continue
		}

		c.took(rep)
	}

	return nil
}

// endsRequest says whether a reply of kind and body, decoded or not, is
// the last its request is answered with: a Description, Rows, a Notice
// or a Status has more after it; a Complete has if its tail says more;
// any other reply ends its request's.
func endsRequest(kind byte, body []byte) bool {
	switch kind {
	case kindDescription, kindRows, kindNotice, kindStatus:
		return false
	case kindComplete:
		head, ok := completedHead(kind, body)

		return !ok || !head.More
	default:
		return true
	}
}

// completedHead reads the head of a Complete: its SQLCODE, SQLSTATE and
// whether a transaction is active, and of its tail the commit's position
// and whether more replies follow, which come before any field the driver
// may fail to take; false if it is no Complete, or its head does not
// read.
func completedHead(kind byte, body []byte) (Completion, bool) {
	if kind != kindComplete {
		return Completion{}, false
	}

	r := newReader(body, "reply")

	c := Completion{SQLCode: signed32(r.u32()), SQLState: r.str(), Active: r.flag()}
	if r.err != nil {
		return Completion{}, false
	}

	t := tail{taken: []byte{tailPosition, tailMore, tailRows, tailGenerated, tailConditions, tailTime}}

	for tag, ok := t.next(r); ok && (tag == tailPosition || tag == tailMore); tag, ok = t.next(r) {
		if tag == tailMore {
			c.More = true

			continue
		}

		if c.Position, c.Committed = r.u64(), true; r.err != nil {
			return Completion{}, false
		}
	}

	// A tail that goes wrong past its position and more leaves the head
	// as read.
	return c, true
}
