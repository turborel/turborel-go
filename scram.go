package turborel

// SCRAM-SHA-256 and SCRAM-SHA-256-PLUS, the client's side [PROTOCOL
// Mechanisms] [CLIENT Connecting] (RFC 5802, RFC 7677): the password
// never travels, and the connection is refused unless the server proves
// it knows the user's verifier. Over TLS the exchange is bound to the
// connection, by `tls-exporter` (RFC 9266) or `tls-server-end-point`
// (RFC 5929), as turborel-protocol's scram::client binds it.
//
//	client-first:  <GS2 header>n=<user>,r=<client nonce>
//	server-first:  r=<client nonce><server nonce>,s=<salt>,i=<iterations>
//	client-final:  c=<GS2 header and the data bound>,r=<both nonces>,p=<proof>
//	server-final:  v=<server signature>

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"strconv"
	"strings"
)

// The constants of SCRAM-SHA-256 [RFC 5802 §3].
const (
	// scramKeyBytes is a key's size, SHA-256's.
	scramKeyBytes = sha256.Size
	// scramNonceBytes is the random bytes of the client's nonce, as
	// turborel-protocol's NONCE_BYTES.
	scramNonceBytes = 18
	// scramClientKey and scramServerKey name the keys derived.
	scramClientKey = "Client Key"
	scramServerKey = "Server Key"
)

// The GS2 headers [RFC 5802 §7]: of a client binding none, of one that
// could but sees the server offer none, and of each binding.
const (
	gs2None      = "n,,"
	gs2Unoffered = "y,,"
	gs2EndPoint  = "p=tls-server-end-point,,"
	gs2Exporter  = "p=tls-exporter,,"
)

// The attributes of the messages read [RFC 5802 §5.1].
const (
	attributeNonce      = 'r'
	attributeSalt       = 's'
	attributeIterations = 'i'
	attributeVerifier   = 'v'
	attributeError      = "e="
	serverFinalPrefix   = "v="
	// serverFirstAttributes is server-first's count of attributes.
	serverFirstAttributes = 3
)

// ChannelBinding says whether SCRAM binds its exchange to the TLS
// connection carrying it [CLIENT TLS by mode].
type ChannelBinding uint8

// The kinds of channel binding.
const (
	// BindingPrefer binds where the connection has a binding.
	BindingPrefer ChannelBinding = iota
	// BindingDisable binds nothing.
	BindingDisable
	// BindingRequire refuses a connection whose exchange cannot be bound.
	BindingRequire
)

// channelKind is how an exchange is bound.
type channelKind uint8

const (
	channelUnbound channelKind = iota
	channelUnoffered
	channelEndPoint
	channelExporter
)

// scramChannel is the binding of an exchange and its data.
type scramChannel struct {
	kind channelKind
	data []byte
}

// header is the channel's GS2 header.
func (c scramChannel) header() string {
	switch c.kind {
	case channelUnbound:
		return gs2None
	case channelUnoffered:
		return gs2Unoffered
	case channelEndPoint:
		return gs2EndPoint
	case channelExporter:
		return gs2Exporter
	default:
		return gs2None
	}
}

// bound says whether the exchange is bound to its channel.
func (c scramChannel) bound() bool {
	return c.kind == channelEndPoint || c.kind == channelExporter
}

// Scram proves a password by SCRAM-SHA-256, or SCRAM-SHA-256-PLUS over
// TLS. It is used for one connection at a time. Its String never shows
// the password.
type Scram struct {
	password string
	clear    bool
	binding  ChannelBinding
	cache    *KeyCache
	channel  scramChannel
	// user is client-first's `n=`, empty: the user is Hello's. nonceOf
	// makes the client's nonce, crypto/rand's unless a test fixes it.
	user    string
	nonceOf func() string
	// What the exchange under way has said and expects.
	bare     string
	nonce    string
	expected []byte
	verified bool
	used     *keyIndexParts
}

// keyIndexParts are what a key cache's entry was found by.
type keyIndexParts struct {
	salt       []byte
	iterations uint32
}

// NewScram proves password by SCRAM. It is used only over TLS, a Unix
// socket or a named pipe, unless InTheClear says otherwise.
func NewScram(password string) *Scram {
	return &Scram{password: password}
}

// InTheClear lets the exchange go over plain TCP too, for a server whose
// SERVER.SCRAM_WITHOUT_TLS is on: overheard, it lets the password be
// guessed at leisure.
func (s *Scram) InTheClear() *Scram {
	c := *s
	c.clear = true

	return &c
}

// WithChannelBinding binds the exchange as binding says.
func (s *Scram) WithChannelBinding(binding ChannelBinding) *Scram {
	c := *s
	c.binding = binding

	return &c
}

// WithCache keeps the keys a password derives in cache [CLIENT The SCRAM
// key cache].
func (s *Scram) WithCache(cache *KeyCache) *Scram {
	c := *s
	c.cache = cache

	return &c
}

// String never shows the password.
func (*Scram) String() string { return "Scram(..)" }

// GoString never shows the password.
func (*Scram) GoString() string { return "Scram(..)" }

// Name is scram-sha-256, or scram-sha-256-plus once bound.
func (s *Scram) Name() string {
	if s.channel.bound() {
		return mechanismScramPlus
	}

	return mechanismScram
}

// Secret is true unless InTheClear said otherwise.
func (s *Scram) Secret() bool { return !s.clear }

// Bind binds the exchange to the TLS channel: by `tls-exporter` where
// the channel exports it, else by the server's certificate; unoffered
// where it has neither.
func (s *Scram) Bind(channel TLSChannel) {
	if s.binding == BindingDisable {
		return
	}

	switch {
	case channel.Exporter != nil:
		s.channel = scramChannel{kind: channelExporter, data: channel.Exporter}
	case endPoint(channel.Certificate) != nil:
		s.channel = scramChannel{kind: channelEndPoint, data: endPoint(channel.Certificate)}
	default:
		s.channel = scramChannel{kind: channelUnoffered}
	}
}

// Initial begins the exchange: client-first, with a fresh nonce. The
// user is the one Hello names, so `n=` is empty.
func (s *Scram) Initial() []byte {
	if s.nonceOf != nil {
		s.nonce = s.nonceOf()
	} else {
		s.nonce = randomNonce()
	}

	s.bare = "n=" + saslName(s.user) + ",r=" + s.nonce
	s.expected, s.verified, s.used = nil, false, nil

	return []byte(s.channel.header() + s.bare)
}

// Answer answers server-first with client-final, and checks
// server-final's signature, answering it with nothing.
func (s *Scram) Answer(challenge []byte) ([]byte, error) {
	var (
		answer []byte
		err    error
	)

	text := string(challenge)
	if strings.HasPrefix(text, serverFinalPrefix) || strings.HasPrefix(text, attributeError) {
		err = s.verify(text)
	} else {
		answer, err = s.prove(text)
	}

	if err != nil {
		s.forget()
	}

	return answer, err
}

// Verified refuses Ready unless the server proved it knows the user's
// verifier.
func (s *Scram) Verified() error {
	if s.verified {
		return nil
	}

	return broken("the server did not prove it knows the user's verifier")
}

// Refused forgets the keys a refused exchange used.
func (s *Scram) Refused() { s.forget() }

// forget drops the key cache's entry the exchange used.
func (s *Scram) forget() {
	if s.cache != nil && s.used != nil {
		s.cache.forget(s.password, s.used.salt, s.used.iterations)
	}

	s.used = nil
}

// randomNonce is a fresh client nonce: 18 random bytes, base 64.
func randomNonce() string {
	random := make([]byte, scramNonceBytes)
	_, _ = rand.Read(random) // crypto/rand's Read never fails.

	return base64.StdEncoding.EncodeToString(random)
}

// saslName escapes a user's name as SCRAM writes it [RFC 5802 §5.1].
func saslName(user string) string {
	return strings.NewReplacer("=", "=3D", ",", "=2C").Replace(user)
}

// scramRefused is the exchange refused: SQLCODE -919, SQLSTATE 28000.
func scramRefused(why string) *Error {
	return &Error{SQLCode: sqlcodeConnection, SQLState: sqlstateInvalidAuthorization, Message: "SCRAM: " + why}
}

// prove reads server-first and gives client-final.
func (s *Scram) prove(serverFirst string) ([]byte, error) {
	if s.nonce == "" || s.expected != nil {
		return nil, scramRefused("the server's first message came out of turn")
	}

	if s.binding == BindingRequire && !s.channel.bound() {
		return nil, scramRefused("channel binding is required, and this connection is not bound")
	}

	both, salt, iterations, err := s.readFirst(serverFirst)
	if err != nil {
		return nil, err
	}

	keys := s.derived(salt, iterations)
	bound := append([]byte(s.channel.header()), s.channel.data...)
	withoutProof := "c=" + base64.StdEncoding.EncodeToString(bound) + ",r=" + both
	auth := []byte(s.bare + "," + serverFirst + "," + withoutProof)
	stored := sha256.Sum256(keys.client)
	signature := scramHMAC(stored[:], auth)

	proof := make([]byte, scramKeyBytes)
	subtle.XORBytes(proof, keys.client, signature)

	s.expected = scramHMAC(keys.server, auth)

	return []byte(withoutProof + ",p=" + base64.StdEncoding.EncodeToString(proof)), nil
}

// readFirst reads server-first: the nonces, which must extend the
// client's, the salt and the iterations.
func (s *Scram) readFirst(message string) (string, []byte, uint32, error) {
	fields := strings.Split(message, ",")
	if len(fields) != serverFirstAttributes {
		return "", nil, 0, scramRefused("the server's first message is not of three attributes")
	}

	both, okNonce := attribute(fields[0], attributeNonce)
	saltText, okSalt := attribute(fields[1], attributeSalt)
	count, okCount := attribute(fields[2], attributeIterations)

	if !okNonce || !okSalt || !okCount {
		return "", nil, 0, scramRefused("the server's first message is not r=, s= and i=")
	}

	if !isNonce(both) || len(both) <= len(s.nonce) || !strings.HasPrefix(both, s.nonce) {
		return "", nil, 0, scramRefused("the server's nonce does not extend the client's")
	}

	salt, ok := decodeBase64(saltText)
	if !ok {
		return "", nil, 0, scramRefused("the salt is not base 64")
	}

	iterations, err := strconv.ParseUint(strings.TrimPrefix(count, "+"), decimalRadix, 32)
	if err != nil || iterations == 0 {
		return "", nil, 0, scramRefused("the iterations are no positive number")
	}

	return both, salt, uint32(iterations), nil
}

// verify checks server-final's signature against the one expected.
func (s *Scram) verify(serverFinal string) error {
	if s.expected == nil {
		return scramRefused("the server's final message came out of turn")
	}

	if refusal, ok := strings.CutPrefix(serverFinal, attributeError); ok {
		return scramRefused("the server refused: " + refusal)
	}

	text, _ := attribute(serverFinal, attributeVerifier)

	signature, ok := decodeBase64(text)
	if !ok {
		return scramRefused("the server's signature is not base 64")
	}

	if !hmac.Equal(signature, s.expected) {
		return scramRefused("the server did not prove it knows the user")
	}

	s.verified = true

	return nil
}

// derived gives the keys the password derives of salt and iterations,
// from the key cache where one is kept.
func (s *Scram) derived(salt []byte, iterations uint32) derivedKeys {
	s.used = &keyIndexParts{salt: salt, iterations: iterations}
	if s.cache != nil {
		return s.cache.derived(s.password, salt, iterations)
	}

	return derive(s.password, salt, iterations)
}

// derivedKeys are ClientKey and ServerKey, what a password derives.
type derivedKeys struct {
	client []byte
	server []byte
}

// derive makes the keys of password, salt and iterations by PBKDF2, the
// slow step [RFC 5802 §3]. The password is taken as its UTF-8, as
// turborel takes it, without SASLprep.
func derive(password string, salt []byte, iterations uint32) derivedKeys {
	salted, err := pbkdf2.Key(sha256.New, password, salt, int(iterations), scramKeyBytes)
	if err != nil {
		// pbkdf2.Key fails only of a key length of 0 or past its bound,
		// never of 32 bytes; a key of zeros proves nothing to a server.
		salted = make([]byte, scramKeyBytes)
	}

	return derivedKeys{
		client: scramHMAC(salted, []byte(scramClientKey)),
		server: scramHMAC(salted, []byte(scramServerKey)),
	}
}

// scramHMAC is HMAC-SHA-256 of data under key.
func scramHMAC(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(data) // A hash's Write never fails.

	return mac.Sum(nil)
}

// attribute gives the value of field, which must be name=value.
func attribute(field string, name byte) (string, bool) {
	if len(field) < 2 || field[0] != name || field[1] != '=' {
		return "", false
	}

	return field[2:], true
}

// isNonce says whether nonce is printable, with no comma [RFC 5802 §7].
func isNonce(nonce string) bool {
	if nonce == "" {
		return false
	}

	for i := range len(nonce) {
		if c := nonce[i]; c < '!' || c > '~' || c == ',' {
			return false
		}
	}

	return true
}

// decodeBase64 reads base 64 as SCRAM writes it: the standard alphabet,
// padded, canonical, nothing else in it; false if text is not.
func decodeBase64(text string) ([]byte, bool) {
	if strings.ContainsAny(text, "\r\n") {
		return nil, false
	}

	b, err := base64.StdEncoding.Strict().DecodeString(text)

	return b, err == nil
}
