package turborel

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/tls"
	"hash"
	"testing"
)

// tlv is a DER element of tag and body, its length in one byte.
func tlv(tag byte, body []byte) []byte {
	return append([]byte{tag, byte(len(body))}, body...) //nolint:gosec // G115: the fixtures are shorter than 128 bytes.
}

// algorithmOf is an AlgorithmIdentifier of oid and parameters.
func algorithmOf(oid string, parameters []byte) []byte {
	return tlv(derSequence, append(tlv(derOID, []byte(oid)), parameters...))
}

// certificateOf is a certificate signed by signature's algorithm.
func certificateOf(signature []byte) []byte {
	body := append(tlv(derSequence, []byte{0x02, 1, 7}), signature...)

	return tlv(derSequence, append(body, tlv(0x03, []byte{0})...))
}

// hashedBy is der's hash by newHash.
func hashedBy(newHash func() hash.Hash, der []byte) []byte {
	h := newHash()
	h.Write(der)

	return h.Sum(nil)
}

// pssOf is an RSASSA-PSS identifier of hash and mask, each omitted if
// nil.
func pssOf(digest, mask string) []byte {
	var fields []byte

	if digest != "" {
		fields = append(fields, tlv(derPSSHash, algorithmOf(digest, []byte{0x05, 0}))...)
	}

	if mask != "" {
		fields = append(fields, tlv(derPSSMask, algorithmOf(oidMGF1, algorithmOf(mask, []byte{0x05, 0})))...)
	}

	return algorithmOf(oidRSAPSS, tlv(derSequence, fields))
}

// TestEndPointHash chooses the binding's hash by the signature's.
//
// Verifies: [PROTOCOL Mechanisms] [RFC 5929 §4.1] tls-server-end-point
// hashes the server's certificate by SHA-256 of a signature of MD5 or
// SHA-1, else by its signature's own hash, as turborel-protocol's
// end_point does.
func TestEndPointHash(t *testing.T) {
	t.Parallel()

	cases := []oidHash{
		{oidMD5RSA, sha256.New}, {oidSHA1RSA, sha256.New}, {oidSHA1ECDSA, sha256.New},
		{oidSHA256RSA, sha256.New}, {oidSHA256ECDSA, sha256.New}, {oidSHA384RSA, sha512.New384},
		{oidSHA384ECDSA, sha512.New384}, {oidSHA512RSA, sha512.New}, {oidSHA512ECDSA, sha512.New},
	}
	for _, c := range cases {
		der := certificateOf(algorithmOf(c.oid, []byte{0x05, 0}))
		if got := endPoint(der); !bytes.Equal(got, hashedBy(c.hash, der)) {
			t.Errorf("% X: % X", []byte(c.oid), got)
		}
	}

	if got := endPoint(certificateOf(algorithmOf(oidSHA384ECDSA, nil))); len(got) != sha512.Size384 {
		t.Errorf("ECDSA without parameters: %d bytes", len(got))
	}
}

// TestEndPointPSS binds RSASSA-PSS by its one hash, and nothing else.
//
// Verifies: [PROTOCOL Mechanisms] [RFC 5929 §4.1] an RSASSA-PSS signature
// is bound by its hash, SHA-1 by default, bound by SHA-256, its mask's
// hash the same; of two hashes, a mask not MGF1, a hash not known, no
// parameters, or a certificate not DER, there is no binding.
func TestEndPointPSS(t *testing.T) {
	t.Parallel()

	same := certificateOf(pssOf(oidSHA384, oidSHA384))
	if !bytes.Equal(endPoint(same), hashedBy(sha512.New384, same)) {
		t.Error("PSS of SHA-384")
	}

	defaults := certificateOf(pssOf("", ""))
	if !bytes.Equal(endPoint(defaults), hashedBy(sha256.New, defaults)) {
		t.Error("PSS of the defaults")
	}

	otherMask := tlv(derPSSMask, algorithmOf(oidSHA1RSA, algorithmOf(oidSHA256, nil)))
	masked := algorithmOf(oidRSAPSS, tlv(derSequence, append(tlv(derPSSHash, algorithmOf(oidSHA256, nil)), otherMask...)))

	none := [][]byte{
		certificateOf(pssOf(oidSHA256, "")),
		certificateOf(pssOf(oidSHA512, oidSHA256)),
		certificateOf(masked),
		certificateOf(pssOf(oidMGF1, oidMGF1)),
		certificateOf(algorithmOf(oidRSAPSS, nil)),
		certificateOf(algorithmOf("\x01\x02\x03", nil)),
		{0x30, 0x85, 0, 0, 0, 0, 1},
		{0x30, 0x82, 1},
		{0x30},
		nil,
	}
	for i, der := range none {
		if got := endPoint(der); got != nil {
			t.Errorf("case %d bound: % X", i, got)
		}
	}

	if _, body, rest, ok := derElement([]byte{0x30, 0x81, 2, 1, 2, 3}); !ok || len(body) != 2 || len(rest) != 1 {
		t.Error("a long-form length misread")
	}
}

// TestChannelOfState gives the certificate always, and the exporter over
// TLS 1.3 alone.
//
// Verifies: [PROTOCOL Mechanisms] [RFC 9266 §2] tls-exporter is bound
// over TLS 1.3 alone; over TLS 1.2 the server's certificate is.
func TestChannelOfState(t *testing.T) {
	t.Parallel()

	if c := channelOfState(tls.ConnectionState{Version: tls.VersionTLS12}); c.Exporter != nil || c.Certificate != nil {
		t.Errorf("TLS 1.2 without a certificate: %+v", c)
	}
}
