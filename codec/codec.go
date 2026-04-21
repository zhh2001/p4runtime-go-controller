// Package codec re-exports the canonical byte encoders used by the
// p4runtime-go-controller SDK so external callers can produce wire
// values without reaching into an internal package.
//
// All helpers follow the P4Runtime 1.3.0 canonical encoding rule:
// integer-typed byte strings have no leading zero bytes; the value
// zero is encoded as an empty byte slice.
package codec

import (
	internalcodec "github.com/zhh2001/p4runtime-go-controller/internal/codec"
)

// EncodeUint returns the canonical byte encoding of v for the given
// bit width. bitwidth must be in the range [1, 64]. Returns an error if
// v does not fit in the requested width.
func EncodeUint(v uint64, bitwidth int) ([]byte, error) {
	return internalcodec.EncodeUint(v, bitwidth)
}

// MustEncodeUint is the panic-on-error variant of EncodeUint.
func MustEncodeUint(v uint64, bitwidth int) []byte {
	return internalcodec.MustEncodeUint(v, bitwidth)
}

// EncodeBytes canonicalises a big-endian byte string for the declared
// bit width.
func EncodeBytes(value []byte, bitwidth int) ([]byte, error) {
	return internalcodec.EncodeBytes(value, bitwidth)
}

// DecodeUint interprets a canonical byte string as uint64. An empty
// slice decodes to zero.
func DecodeUint(b []byte) (uint64, error) {
	return internalcodec.DecodeUint(b)
}

// MAC parses "aa:bb:cc:dd:ee:ff" / "aa-bb-cc-dd-ee-ff" / "aabbccddeeff"
// and returns the canonical P4Runtime encoding.
func MAC(s string) ([]byte, error) {
	return internalcodec.MAC(s)
}

// MustMAC is the panic-on-error variant of MAC.
func MustMAC(s string) []byte {
	return internalcodec.MustMAC(s)
}

// IPv4 parses a dotted-quad literal.
func IPv4(s string) ([]byte, error) {
	return internalcodec.IPv4(s)
}

// MustIPv4 is the panic-on-error variant of IPv4.
func MustIPv4(s string) []byte {
	return internalcodec.MustIPv4(s)
}

// IPv6 parses a colon-separated IPv6 literal.
func IPv6(s string) ([]byte, error) {
	return internalcodec.IPv6(s)
}

// MustIPv6 is the panic-on-error variant of IPv6.
func MustIPv6(s string) []byte {
	return internalcodec.MustIPv6(s)
}

// LPMMask truncates value to prefixLen bits and returns the canonical
// encoding. prefixLen must be in [0, bitwidth]; a zero prefix yields an
// empty slice (caller should omit the match field entirely in that
// case).
func LPMMask(value []byte, prefixLen int, bitwidth int) ([]byte, error) {
	return internalcodec.LPMMask(value, prefixLen, bitwidth)
}

// TernaryMask builds a prefix-style ternary mask with the top prefixLen
// bits set.
func TernaryMask(prefixLen, bitwidth int) ([]byte, error) {
	return internalcodec.TernaryMask(prefixLen, bitwidth)
}

// TernaryApply returns value & mask in canonical encoding.
func TernaryApply(value, mask []byte, bitwidth int) ([]byte, error) {
	return internalcodec.TernaryApply(value, mask, bitwidth)
}

// ValidateRange checks that low <= high for the declared bit width.
func ValidateRange(low, high []byte, bitwidth int) error {
	return internalcodec.ValidateRange(low, high, bitwidth)
}

// ParseHex parses an optionally-prefixed and colon-separated hex literal.
func ParseHex(s string) ([]byte, error) {
	return internalcodec.ParseHex(s)
}

// FormatHex renders v as a colon-separated hex string.
func FormatHex(v []byte) string {
	return internalcodec.FormatHex(v)
}
