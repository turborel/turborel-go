package turborel

// The values of connection settings that are one of a few words [CLIENT
// Where settings come from]: how TLS is used, whether SCRAM binds its
// channel, what a server must be to be taken, and whether servers are
// tried in order. Each is written in lower case, as a setting, the
// environment and a URI give it.

import "strings"

// TLSMode says how a connection over TCP uses TLS [CLIENT TLS by mode]. A
// local connection, over a Unix socket or a named pipe, is private, and
// uses none whatever this says.
type TLSMode uint8

// The modes of TLS.
const (
	// TLSPrefer uses TLS if the server takes it, else plain TCP, its
	// certificate not checked.
	TLSPrefer TLSMode = iota
	// TLSDisable never uses TLS.
	TLSDisable
	// TLSRequire uses TLS or makes no connection, the server's
	// certificate not checked.
	TLSRequire
	// TLSVerifyCA uses TLS, the server's certificate signed by an
	// authority trusted, its name not checked.
	TLSVerifyCA
	// TLSVerifyFull uses TLS, the server's certificate signed by an
	// authority trusted and of the server's name.
	TLSVerifyFull
)

// TargetRole is what a server must be for a connection to it to be kept
// [CLIENT Several servers].
type TargetRole uint8

// The roles a server may be asked to be.
const (
	// TargetAny takes any server.
	TargetAny TargetRole = iota
	// TargetReadWrite takes one that takes changes: a server alone, or its
	// group's primary.
	TargetReadWrite
	// TargetReadOnly takes one that takes none: a replica following its
	// group's log.
	TargetReadOnly
)

// LoadBalance says whether servers are tried in the order given [CLIENT
// Several servers].
type LoadBalance uint8

// The orders servers are tried in.
const (
	// LoadBalanceDisable tries them in order.
	LoadBalanceDisable LoadBalance = iota
	// LoadBalanceRandom tries them in an order chosen at random for each
	// connection, so connections spread over them.
	LoadBalanceRandom
)

// The words several kinds share.
const (
	wordDisable = "disable"
	wordPrefer  = "prefer"
	wordRequire = "require"
)

// choice is a value and its word.
type choice[T comparable] struct {
	text  string
	value T
}

// tlsModes are TLSMode's words, in their order.
func tlsModes() []choice[TLSMode] {
	return []choice[TLSMode]{
		{wordDisable, TLSDisable}, {wordPrefer, TLSPrefer}, {wordRequire, TLSRequire},
		{"verify-ca", TLSVerifyCA}, {"verify-full", TLSVerifyFull},
	}
}

// channelBindings are ChannelBinding's words.
func channelBindings() []choice[ChannelBinding] {
	return []choice[ChannelBinding]{
		{wordDisable, BindingDisable}, {wordPrefer, BindingPrefer}, {wordRequire, BindingRequire},
	}
}

// targetRoles are TargetRole's words.
func targetRoles() []choice[TargetRole] {
	return []choice[TargetRole]{{"any", TargetAny}, {"read-write", TargetReadWrite}, {"read-only", TargetReadOnly}}
}

// loadBalances are LoadBalance's words.
func loadBalances() []choice[LoadBalance] {
	return []choice[LoadBalance]{{wordDisable, LoadBalanceDisable}, {"random", LoadBalanceRandom}}
}

// booleans are a yes or no's words.
func booleans() []choice[bool] { return []choice[bool]{{"true", true}, {"false", false}} }

// parseWord gives the value of key the word text is, among choices. It
// fails with SQLSTATE 08001 if text is none of them.
func parseWord[T comparable](key, text string, choices []choice[T]) (T, error) {
	known := make([]string, len(choices))

	for i, w := range choices {
		if w.text == text {
			return w.value, nil
		}

		known[i] = w.text
	}

	var none T

	return none, invalid(key + " is " + text + ", not one of " + strings.Join(known, ", "))
}

// textOf gives value's word among choices.
func textOf[T comparable](value T, choices []choice[T]) string {
	for _, w := range choices {
		if w.value == value {
			return w.text
		}
	}

	return ""
}

// ParseTLSMode gives the mode text names, the value of key.
func ParseTLSMode(key, text string) (TLSMode, error) { return parseWord(key, text, tlsModes()) }

// ParseChannelBinding gives the binding text names, the value of key.
func ParseChannelBinding(key, text string) (ChannelBinding, error) {
	return parseWord(key, text, channelBindings())
}

// ParseTargetRole gives the role text names, the value of key.
func ParseTargetRole(key, text string) (TargetRole, error) {
	return parseWord(key, text, targetRoles())
}

// ParseLoadBalance gives the choice text names, the value of key.
func ParseLoadBalance(key, text string) (LoadBalance, error) {
	return parseWord(key, text, loadBalances())
}

// String gives the mode's word.
func (m TLSMode) String() string { return textOf(m, tlsModes()) }

// Verifies says whether the server's certificate is checked against the
// authorities trusted.
func (m TLSMode) Verifies() bool { return m == TLSVerifyCA || m == TLSVerifyFull }

// String gives the binding's word.
func (b ChannelBinding) String() string { return textOf(b, channelBindings()) }

// String gives the role's word.
func (r TargetRole) String() string { return textOf(r, targetRoles()) }

// String gives the choice's word.
func (l LoadBalance) String() string { return textOf(l, loadBalances()) }
