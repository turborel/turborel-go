package turborel

// What the server writes, for building the bodies of replies in tests:
// the driver itself only reads these.

// failure writes a failure, as an Error carries it [PROTOCOL Composite
// fields].
func (w *writer) failure(e *Error) {
	w.u32(unsigned32(e.SQLCode))
	w.str(e.SQLState)
	w.str(e.Message)
	w.flag(e.Rule != "")

	if e.Rule != "" {
		w.str(e.Rule)
	}

	w.flag(e.Placed)

	if e.Placed {
		w.u64(e.Position)
	}

	w.flag(e.Active)
	e.Diagnostics.encode(w)
}

// encode writes the diagnostics' tail, each field told with its tag
// [PROTOCOL Errors and notices].
func (d *Diagnostics) encode(w *writer) {
	if d == nil {
		return
	}

	for _, tag := range diagnosticTags() {
		if s := *d.slot(tag); s != "" {
			w.u8(tag)
			w.str(s)
		}
	}
}
