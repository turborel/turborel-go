package turborel

// A signed 128-bit integer, the protocol's i128 [PROTOCOL Values and
// fields]: the units of an exact number or an interval and the
// coefficient of a DECFLOAT. Go has no such integer, so it is held as two
// words, its two's complement bits, and converted to math/big for
// arithmetic a program does.

import (
	"encoding/binary"
	"math/big"
)

const (
	// int128Bytes is an i128's size on the wire.
	int128Bytes = 16
	// wordBits is the bits of one of an int128's two words.
	wordBits = 64
	// int128Bits is the bits of an int128.
	int128Bits = 128
)

// int128 is a two's complement 128-bit integer: hi·2⁶⁴ + lo.
type int128 struct {
	// hi is the high word, its sign the number's.
	hi int64
	// lo is the low word.
	lo uint64
}

// int128Of is v as an int128.
func int128Of(v int64) int128 {
	hi := int64(0)
	if v < 0 {
		hi = -1
	}

	return int128{hi: hi, lo: unsigned64(v)}
}

// int128At reads the 16 little-endian bytes of b.
func int128At(b []byte) int128 {
	return int128{lo: binary.LittleEndian.Uint64(b), hi: signed64(binary.LittleEndian.Uint64(b[wordBytes:]))}
}

// appendLE appends x's 16 bytes, least significant first.
func (x int128) appendLE(dst []byte) []byte {
	dst = binary.LittleEndian.AppendUint64(dst, x.lo)

	return binary.LittleEndian.AppendUint64(dst, unsigned64(x.hi))
}

// fitsInt64 says whether an int64 holds x: whether its high word is the
// sign of its low word extended.
func (x int128) fitsInt64() bool {
	if signed64(x.lo) < 0 {
		return x.hi == -1
	}

	return x.hi == 0
}

// isZero says whether x is 0.
func (x int128) isZero() bool {
	return x.hi == 0 && x.lo == 0
}

// toBig is x as a big.Int.
func (x int128) toBig() *big.Int {
	n := big.NewInt(x.hi)
	n.Lsh(n, wordBits)

	return n.Add(n, new(big.Int).SetUint64(x.lo))
}

// int128Limits gives the least and greatest int128, −2¹²⁷ and 2¹²⁷ − 1.
func int128Limits() (*big.Int, *big.Int) {
	greatest := new(big.Int).Lsh(big.NewInt(1), int128Bits-1)
	least := new(big.Int).Neg(greatest)

	return least, greatest.Sub(greatest, big.NewInt(1))
}

// int128OfBig is n as an int128, false if n is outside its range.
func int128OfBig(n *big.Int) (int128, bool) {
	least, greatest := int128Limits()
	if n.Cmp(least) < 0 || n.Cmp(greatest) > 0 {
		return int128{}, false
	}

	bits := new(big.Int).Set(n)
	if n.Sign() < 0 {
		bits.Add(bits, new(big.Int).Lsh(big.NewInt(1), int128Bits))
	}

	mask := new(big.Int).SetUint64(^uint64(0))
	lo := new(big.Int).And(bits, mask).Uint64()
	hi := bits.Rsh(bits, wordBits).Uint64()

	return int128{hi: signed64(hi), lo: lo}, true
}
