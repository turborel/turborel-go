package turborel

import (
	"bytes"
	"strings"
	"testing"
)

// The exchange of RFC 7677 §3's test vector.
const (
	vectorUser        = "user"
	vectorPassword    = "pencil"
	vectorNonce       = "rOprNGfwEbeRWgbNEkqO"
	vectorServerFirst = "r=rOprNGfwEbeRWgbNEkqO%hvYDpWUa2RaTCAfuxFIlj)hNlF$k0,s=W22ZaJ0SNY7soEsUEjb6gQ==,i=4096"
	vectorClientFinal = "c=biws,r=rOprNGfwEbeRWgbNEkqO%hvYDpWUa2RaTCAfuxFIlj)hNlF$k0," +
		"p=dHzbZapWIk4jUhN+Ute9ytag9zjfMHgsqmmiz7AndVQ="
	vectorServerFinal = "v=6rriTRBi23WpRR/wtup+mMhUZUn/dB5nLTJRsjl95G4="
)

// vectorScram is a Scram of the test vector's user, password and nonce.
func vectorScram() *Scram {
	s := NewScram(vectorPassword)
	s.user, s.nonceOf = vectorUser, func() string { return vectorNonce }

	return s
}

// TestScramVector runs RFC 7677's test vector.
//
// Verifies: [PROTOCOL Mechanisms] [RFC 7677 §3] scram-sha-256 sends
// client-first in Hello, answers server-first with client-final, checks
// server-final's signature and answers it with nothing, byte for byte as
// the RFC's vector.
func TestScramVector(t *testing.T) {
	t.Parallel()

	s := vectorScram()
	if got := string(s.Initial()); got != "n,,n=user,r="+vectorNonce {
		t.Errorf("client-first %q", got)
	}

	final, err := s.Answer([]byte(vectorServerFirst))
	if err != nil || string(final) != vectorClientFinal {
		t.Fatalf("client-final %q, %v", final, err)
	}

	if err := s.Verified(); err == nil {
		t.Error("Ready taken before the server's signature")
	}

	last, err := s.Answer([]byte(vectorServerFinal))
	if err != nil || len(last) != 0 || s.Verified() != nil {
		t.Errorf("after server-final: %q, %v, %v", last, err, s.Verified())
	}

	if s.Name() != mechanismScram || !s.Secret() || s.InTheClear().Secret() {
		t.Errorf("name %s, secret %v", s.Name(), s.Secret())
	}
}

// TestScramRefusals refuses each malformed or false step of the server.
//
// Verifies: [PROTOCOL Mechanisms] [RFC 5802 §5] a server-first out of
// turn, not of r=, s= and i=, whose nonce does not extend the client's,
// whose salt is not base 64 or whose iterations are not a positive
// number, a server-final out of turn, of a refusal, or whose signature
// is false, each refuse the exchange (SQLSTATE 28000).
func TestScramRefusals(t *testing.T) {
	t.Parallel()

	first := map[string]string{
		"fields":     "r=x,s=y",
		"names":      "s=x,r=y,i=1",
		"nonce":      "r=other,s=W22ZaJ0SNY7soEsUEjb6gQ==,i=4096",
		"short":      "r=" + vectorNonce + ",s=W22ZaJ0SNY7soEsUEjb6gQ==,i=4096",
		"comma":      "r=" + vectorNonce + "\x01,s=W22ZaJ0SNY7soEsUEjb6gQ==,i=4096",
		"salt":       "r=" + vectorNonce + "x,s=!!,i=4096",
		"iterations": "r=" + vectorNonce + "x,s=W22ZaJ0SNY7soEsUEjb6gQ==,i=0",
		"newline":    "r=" + vectorNonce + "x,s=W22Z\naJ0SNY7soEsUEjb6gQ==,i=1",
	}
	for name, message := range first {
		s := vectorScram()
		s.Initial()

		_, err := s.Answer([]byte(message))
		assertState(t, err, sqlstateInvalidAuthorization)

		if t.Failed() {
			t.Logf("case %s", name)
		}
	}

	_, err := vectorScram().Answer([]byte(vectorServerFirst))
	assertState(t, err, sqlstateInvalidAuthorization)

	for _, final := range []string{"e=invalid-proof", "v=AAAA", "v=!!"} {
		s := vectorScram()
		s.Initial()

		if _, err := s.Answer([]byte(vectorServerFirst)); err != nil {
			t.Fatal(err)
		}

		_, err := s.Answer([]byte(final))
		assertState(t, err, sqlstateInvalidAuthorization)
	}

	_, err = vectorScram().Answer([]byte(vectorServerFinal))
	assertState(t, err, sqlstateInvalidAuthorization)
}

// TestScramBinding binds the exchange as the channel and the program say.
//
// Verifies: [PROTOCOL Mechanisms] scram-sha-256-plus binds by tls-exporter
// where the channel exports it, else by tls-server-end-point, as its GS2
// header names, the data bound in client-final's c=; a channel of
// neither is unoffered, `y`; a binding disabled binds nothing; one
// required refuses an exchange not bound.
func TestScramBinding(t *testing.T) {
	t.Parallel()

	exported := vectorScram()
	exported.Bind(TLSChannel{Exporter: bytes.Repeat([]byte{7}, exporterBytes)})

	if got := string(exported.Initial()); !strings.HasPrefix(got, gs2Exporter) || exported.Name() != mechanismScramPlus {
		t.Errorf("exporter: %q, %s", got, exported.Name())
	}

	final, err := exported.Answer([]byte(vectorServerFirst))
	if err != nil || !strings.HasPrefix(string(final), "c=cD10bHMtZXhwb3J0ZXIsLAcHBwcH") {
		t.Errorf("exporter's client-final %q, %v", final, err)
	}

	unoffered := vectorScram()
	unoffered.Bind(TLSChannel{})

	if got := string(unoffered.Initial()); !strings.HasPrefix(got, gs2Unoffered) {
		t.Errorf("unoffered: %q", got)
	}

	disabled := vectorScram().WithChannelBinding(BindingDisable)
	disabled.Bind(TLSChannel{Exporter: []byte{1}})

	if disabled.Name() != mechanismScram {
		t.Errorf("disabled binding bound: %s", disabled.Name())
	}

	required := vectorScram().WithChannelBinding(BindingRequire)
	required.Initial()

	_, err = required.Answer([]byte(vectorServerFirst))
	assertState(t, err, sqlstateInvalidAuthorization)

	if got := (&Scram{}).String() + vectorScram().GoString(); strings.Contains(got, vectorPassword) {
		t.Errorf("a Scram shows its password: %q", got)
	}
}

// TestSaslName escapes a user's name as SCRAM writes it.
//
// Verifies: [PROTOCOL Mechanisms] [RFC 5802 §5.1] a name's `=` and `,`
// are written =3D and =2C.
func TestSaslName(t *testing.T) {
	t.Parallel()

	if got := saslName("a=b,c"); got != "a=3Db=2Cc" {
		t.Errorf("saslName = %q", got)
	}

	first, second := randomNonce(), randomNonce()
	if first == second || len(first) != 24 {
		t.Error("nonces repeat, or are not 18 bytes of base 64")
	}
}
