package turborel

// The Lob messages [PROTOCOL Lob 'L']: what the client asks of a large
// object it reaches by its locator, and the reply that is not a
// Complete, the octets read or the locator made.

// The operations of a Lob request.
const (
	lobRead   byte = 0
	lobCreate byte = 1
	lobAppend byte = 2
	lobFree   byte = 3
	lobHold   byte = 4
)

// The kinds of a Lob reply.
const (
	lobData    byte = 0
	lobLocator byte = 1
)

// lobReadRequest reads at most count octets, or characters, from from.
type lobReadRequest struct {
	locator    uint32
	from       uint64
	count      uint64
	characters bool
}

func (q lobReadRequest) encode(w *writer) {
	w.frame(kindLobRequest, func() {
		w.u8(lobRead)
		w.u32(q.locator)
		w.u64(q.from)
		w.u64(q.count)
		w.flag(q.characters)
	})
}

// lobCreateRequest makes an empty large object of a kind.
type lobCreateRequest struct {
	kind LOBKind
}

func (q lobCreateRequest) encode(w *writer) {
	w.frame(kindLobRequest, func() {
		w.u8(lobCreate)
		w.u8(byte(q.kind))
	})
}

// lobAppendRequest adds octets to a large object Create made.
type lobAppendRequest struct {
	locator uint32
	octets  []byte
}

func (q lobAppendRequest) encode(w *writer) {
	w.frame(kindLobRequest, func() {
		w.u8(lobAppend)
		w.u32(q.locator)
		w.bytes(q.octets)
	})
}

// lobFreeRequest ends a locator.
type lobFreeRequest struct {
	locator uint32
}

func (q lobFreeRequest) encode(w *writer) {
	w.frame(kindLobRequest, func() {
		w.u8(lobFree)
		w.u32(q.locator)
	})
}

// lobHoldRequest keeps a locator valid past its transaction's commit.
type lobHoldRequest struct {
	locator uint32
}

func (q lobHoldRequest) encode(w *writer) {
	w.frame(kindLobRequest, func() {
		w.u8(lobHold)
		w.u32(q.locator)
	})
}

// lobDataReply is the data a Lob read asked for.
type lobDataReply struct {
	data []byte
}

// lobLocatorReply is the locator a Lob create made.
type lobLocatorReply struct {
	locator uint32
}

// lobReply reads a Lob reply's body.
func (r *reader) lobReply() reply {
	switch r.u8() {
	case lobData:
		return lobDataReply{data: r.bytes()}
	case lobLocator:
		return lobLocatorReply{locator: r.u32()}
	default:
		r.fail("no Lob reply is of this kind")

		return nil
	}
}
