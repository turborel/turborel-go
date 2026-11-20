package turborel

// The channel binding `tls-server-end-point` [RFC 5929 §4]: the hash of
// the TLS server's certificate, by a hash its signature algorithm
// chooses, as turborel-protocol's scram::end_point computes it. A
// signature of MD5 or SHA-1 is bound by SHA-256; of SHA-256, SHA-384 or
// SHA-512 by that; of RSASSA-PSS by its one hash, its mask's the same;
// of no hash, as Ed25519's, or of an algorithm not known, there is no
// binding. `tls-exporter` [RFC 9266 §2] is the keying material a TLS 1.3
// session exports under its label.

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/tls"
	"hash"
)

// The DER tags walked [ITU-T X.690 §8.1].
const (
	derSequence byte = 0x30
	derOID      byte = 0x06
	// derPSSHash and derPSSMask are RSASSA-PSS-params' explicit tags.
	derPSSHash byte = 0xa0
	derPSSMask byte = 0xa1
	// derLongForm marks a length of more octets; derMaxLengthOctets is
	// the most read.
	derLongForm        = 0x80
	derMaxLengthOctets = 4
	derLengthBits      = 8
	// derHeadBytes is an element's tag and first length octet.
	derHeadBytes = 2
)

// The exporter's label and length [RFC 9266 §2].
const (
	exporterLabel = "EXPORTER-Channel-Binding"
	exporterBytes = 32
)

// The object identifiers of the signature and hash algorithms, as DER
// writes them.
const (
	oidMD5RSA      = "\x2a\x86\x48\x86\xf7\x0d\x01\x01\x04"
	oidSHA1RSA     = "\x2a\x86\x48\x86\xf7\x0d\x01\x01\x05"
	oidMGF1        = "\x2a\x86\x48\x86\xf7\x0d\x01\x01\x08"
	oidRSAPSS      = "\x2a\x86\x48\x86\xf7\x0d\x01\x01\x0a"
	oidSHA256RSA   = "\x2a\x86\x48\x86\xf7\x0d\x01\x01\x0b"
	oidSHA384RSA   = "\x2a\x86\x48\x86\xf7\x0d\x01\x01\x0c"
	oidSHA512RSA   = "\x2a\x86\x48\x86\xf7\x0d\x01\x01\x0d"
	oidSHA1ECDSA   = "\x2a\x86\x48\xce\x3d\x04\x01"
	oidSHA256ECDSA = "\x2a\x86\x48\xce\x3d\x04\x03\x02"
	oidSHA384ECDSA = "\x2a\x86\x48\xce\x3d\x04\x03\x03"
	oidSHA512ECDSA = "\x2a\x86\x48\xce\x3d\x04\x03\x04"
	oidSHA1        = "\x2b\x0e\x03\x02\x1a"
	oidSHA256      = "\x60\x86\x48\x01\x65\x03\x04\x02\x01"
	oidSHA384      = "\x60\x86\x48\x01\x65\x03\x04\x02\x02"
	oidSHA512      = "\x60\x86\x48\x01\x65\x03\x04\x02\x03"
)

// oidHash pairs an algorithm's identifier with the hash that binds it.
type oidHash struct {
	oid  string
	hash func() hash.Hash
}

// signatureHashes are the signature algorithms and the hashes binding
// them.
func signatureHashes() []oidHash {
	return []oidHash{
		{oidMD5RSA, sha256.New}, {oidSHA1RSA, sha256.New}, {oidSHA256RSA, sha256.New},
		{oidSHA384RSA, sha512.New384}, {oidSHA512RSA, sha512.New},
		{oidSHA1ECDSA, sha256.New}, {oidSHA256ECDSA, sha256.New},
		{oidSHA384ECDSA, sha512.New384}, {oidSHA512ECDSA, sha512.New},
	}
}

// pssHashes are RSASSA-PSS's hash algorithms and the hashes binding
// them.
func pssHashes() []oidHash {
	return []oidHash{{oidSHA1, sha256.New}, {oidSHA256, sha256.New}, {oidSHA384, sha512.New384}, {oidSHA512, sha512.New}}
}

// derElement reads one element: its tag, its body and what follows; ok
// false if der is too short for it.
func derElement(der []byte) (byte, []byte, []byte, bool) {
	if len(der) < derHeadBytes {
		return 0, nil, nil, false
	}

	tag, first, rest := der[0], int(der[1]), der[2:]
	length := first

	if first >= derLongForm {
		count := first &^ derLongForm
		if count == 0 || count > derMaxLengthOctets || count > len(rest) {
			return 0, nil, nil, false
		}

		length = 0
		for _, b := range rest[:count] {
			length = length<<derLengthBits | int(b)
		}

		rest = rest[count:]
	}

	if length < 0 || length > len(rest) {
		return 0, nil, nil, false
	}

	return tag, rest[:length], rest[length:], true
}

// derExpect reads one element of tag: its body and what follows.
func derExpect(der []byte, tag byte) ([]byte, []byte, bool) {
	found, body, rest, ok := derElement(der)
	if !ok || found != tag {
		return nil, nil, false
	}

	return body, rest, true
}

// derIdentifier reads an AlgorithmIdentifier: its OID and parameters.
func derIdentifier(der []byte) ([]byte, []byte, bool) {
	body, _, ok := derExpect(der, derSequence)
	if !ok {
		return nil, nil, false
	}

	return derExpect(body, derOID)
}

// endPoint gives the `tls-server-end-point` binding of certificate, DER;
// nil where its signature algorithm defines none.
func endPoint(certificate []byte) []byte {
	body, _, ok := derExpect(certificate, derSequence)
	if !ok {
		return nil
	}

	_, after, ok := derExpect(body, derSequence)
	if !ok {
		return nil
	}

	algorithm, parameters, ok := derIdentifier(after)
	if !ok {
		return nil
	}

	newHash := signatureHash(algorithm, parameters)
	if newHash == nil {
		return nil
	}

	h := newHash()
	_, _ = h.Write(certificate) // A hash's Write never fails.

	return h.Sum(nil)
}

// signatureHash gives the hash binding a signature of algorithm, nil if
// none does.
func signatureHash(algorithm, parameters []byte) func() hash.Hash {
	if string(algorithm) == oidRSAPSS {
		return pssHash(parameters)
	}

	return lookupHash(signatureHashes(), algorithm)
}

// lookupHash finds oid's hash in table.
func lookupHash(table []oidHash, oid []byte) func() hash.Hash {
	for _, entry := range table {
		if entry.oid == string(oid) {
			return entry.hash
		}
	}

	return nil
}

// pssHash gives the hash binding an RSASSA-PSS signature of parameters:
// its hash, SHA-1 by default, which its mask's must be.
func pssHash(parameters []byte) func() hash.Hash {
	fields, _, ok := derExpect(parameters, derSequence)
	if !ok {
		return nil
	}

	digest, mask := []byte(oidSHA1), []byte(oidSHA1)

	if explicit, rest, found := derExpect(fields, derPSSHash); found {
		if digest, _, ok = derIdentifier(explicit); !ok {
			return nil
		}

		fields = rest
	}

	if explicit, _, found := derExpect(fields, derPSSMask); found {
		function, of, okMask := derIdentifier(explicit)
		if !okMask || string(function) != oidMGF1 {
			return nil
		}

		if mask, _, ok = derIdentifier(of); !ok {
			return nil
		}
	}

	if !bytes.Equal(digest, mask) {
		return nil
	}

	return lookupHash(pssHashes(), digest)
}

// ChannelOf gives what the TLS connection conn binds a mechanism to, for
// a program that opens a TLS stream of its own and gives it to Open: it
// binds its mechanism by the mechanism's Bind first [CLIENT Connecting].
func ChannelOf(conn *tls.Conn) TLSChannel {
	return channelOfState(conn.ConnectionState())
}

// channelOfState gives what a TLS connection's handshake binds an
// exchange to: the server's certificate, and `tls-exporter` over TLS 1.3
// alone.
func channelOfState(state tls.ConnectionState) TLSChannel {
	var channel TLSChannel

	if len(state.PeerCertificates) > 0 {
		channel.Certificate = state.PeerCertificates[0].Raw
	}

	if state.Version == tls.VersionTLS13 {
		if exported, err := state.ExportKeyingMaterial(exporterLabel, []byte{}, exporterBytes); err == nil {
			channel.Exporter = exported
		}
	}

	return channel
}
