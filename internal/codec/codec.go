// Package codec encodes and decodes P4Runtime canonical byte strings.
//
// P4Runtime 1.3.0 mandates that byte strings representing integer values
// contain no leading zero bytes — the most-significant byte must have its
// most-significant bit set, with the single exception that the value zero
// is represented by an empty byte string.
//
// Beyond integers this package exposes helpers for MAC addresses (48-bit),
// IPv4 and IPv6 addresses, arbitrary bit widths, LPM prefix masking, and
// ternary mask construction. Each helper returns an error rather than
// panicking; panic-on-error variants (Must…) are provided for test and
// example code where the inputs are known-good literals.
package codec

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	errs "github.com/zhh2001/p4runtime-go-controller/errors"
)

// EncodeUint returns the canonical byte encoding of v for the given
// bitwidth. bitwidth must be in the range [1, 64]. Values that do not fit
// into bitwidth bits return ErrInvalidBitWidth.
func EncodeUint(v uint64, bitwidth int) ([]byte, error) {
	if bitwidth <= 0 || bitwidth > 64 {
		return nil, fmt.Errorf("codec.EncodeUint: bitwidth %d out of range (1..64)", bitwidth)
	}
	if bitwidth < 64 {
		if v >= uint64(1)<<bitwidth {
			return nil, fmt.Errorf("codec.EncodeUint: value %d exceeds %d-bit range: %w", v, bitwidth, errs.ErrInvalidBitWidth)
		}
	}
	if v == 0 {
		return []byte{}, nil
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], v)
	return stripLeadingZeros(buf[:]), nil
}

// MustEncodeUint is the panic-on-error variant of EncodeUint.
func MustEncodeUint(v uint64, bitwidth int) []byte {
	b, err := EncodeUint(v, bitwidth)
	if err != nil {
		panic(err)
	}
	return b
}

// EncodeBytes returns the canonical encoding of value interpreted as a
// big-endian integer of the given bit width. Any leading zero bytes are
// stripped. The bit width is used to validate that value fits; values with
// too-high bits set return ErrInvalidBitWidth.
func EncodeBytes(value []byte, bitwidth int) ([]byte, error) {
	if bitwidth <= 0 {
		return nil, fmt.Errorf("codec.EncodeBytes: bitwidth %d must be positive", bitwidth)
	}
	if len(value) == 0 {
		return []byte{}, nil
	}

	// Check for bits outside the declared width.
	maxBytes := byteLen(bitwidth)
	leadingBits := bitwidth % 8
	// First find the first non-zero byte.
	idx := 0
	for idx < len(value) && value[idx] == 0 {
		idx++
	}
	if idx == len(value) {
		return []byte{}, nil
	}
	significant := value[idx:]
	if len(significant) > maxBytes {
		return nil, fmt.Errorf("codec.EncodeBytes: value uses %d bytes, bitwidth allows %d: %w",
			len(significant), maxBytes, errs.ErrInvalidBitWidth)
	}
	if len(significant) == maxBytes && leadingBits != 0 {
		// Top byte is only allowed to have its low `leadingBits` bits set.
		validMask := byte((1 << uint(leadingBits)) - 1)
		if significant[0]&^validMask != 0 {
			return nil, fmt.Errorf("codec.EncodeBytes: high-order bits set outside %d-bit width: %w",
				bitwidth, errs.ErrInvalidBitWidth)
		}
	}
	out := make([]byte, len(significant))
	copy(out, significant)
	return out, nil
}

// DecodeUint interprets a canonical byte string as uint64. An empty byte
// slice decodes to zero. Inputs longer than 8 bytes return an error.
func DecodeUint(b []byte) (uint64, error) {
	if len(b) > 8 {
		return 0, fmt.Errorf("codec.DecodeUint: input %d bytes exceeds 64 bits", len(b))
	}
	var v uint64
	for _, c := range b {
		v = (v << 8) | uint64(c)
	}
	return v, nil
}

// MAC parses a MAC address in the canonical colon-separated form
// "aa:bb:cc:dd:ee:ff" (or dashes, or contiguous hex) and returns the
// P4Runtime canonical byte encoding (leading zeros stripped).
func MAC(s string) ([]byte, error) {
	cleaned := strings.Map(func(r rune) rune {
		switch r {
		case ':', '-', '.':
			return -1
		}
		return r
	}, s)
	if len(cleaned) != 12 {
		return nil, fmt.Errorf("codec.MAC: %q is not a 48-bit MAC", s)
	}
	raw, err := hex.DecodeString(cleaned)
	if err != nil {
		return nil, fmt.Errorf("codec.MAC: %q: %w", s, err)
	}
	return stripLeadingZeros(raw), nil
}

// MustMAC is the panic-on-error variant of MAC.
func MustMAC(s string) []byte {
	b, err := MAC(s)
	if err != nil {
		panic(err)
	}
	return b
}

// IPv4 parses a dotted-quad IPv4 literal and returns the canonical encoding.
func IPv4(s string) ([]byte, error) {
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return nil, fmt.Errorf("codec.IPv4: %w", err)
	}
	if !addr.Is4() {
		return nil, fmt.Errorf("codec.IPv4: %q is not IPv4", s)
	}
	four := addr.As4()
	return stripLeadingZeros(four[:]), nil
}

// MustIPv4 is the panic-on-error variant of IPv4.
func MustIPv4(s string) []byte {
	b, err := IPv4(s)
	if err != nil {
		panic(err)
	}
	return b
}

// IPv6 parses an IPv6 literal and returns the canonical encoding.
func IPv6(s string) ([]byte, error) {
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return nil, fmt.Errorf("codec.IPv6: %w", err)
	}
	if !addr.Is6() || addr.Is4In6() {
		return nil, fmt.Errorf("codec.IPv6: %q is not IPv6", s)
	}
	sixteen := addr.As16()
	return stripLeadingZeros(sixteen[:]), nil
}

// MustIPv6 is the panic-on-error variant of IPv6.
func MustIPv6(s string) []byte {
	b, err := IPv6(s)
	if err != nil {
		panic(err)
	}
	return b
}

// LPMMask truncates value to the first prefixLen bits and returns the
// canonical encoding. bitwidth is the total field width; prefixLen must
// satisfy 0 <= prefixLen <= bitwidth. A zero prefix returns an empty byte
// slice (callers should omit the match entirely in that case).
func LPMMask(value []byte, prefixLen int, bitwidth int) ([]byte, error) {
	if bitwidth <= 0 {
		return nil, fmt.Errorf("codec.LPMMask: bitwidth %d must be positive", bitwidth)
	}
	if prefixLen < 0 || prefixLen > bitwidth {
		return nil, fmt.Errorf("codec.LPMMask: prefixLen %d out of range 0..%d", prefixLen, bitwidth)
	}
	if prefixLen == 0 {
		return []byte{}, nil
	}
	// Pad or trim to maxBytes (ceil(bitwidth/8)).
	maxBytes := byteLen(bitwidth)
	padded := padToWidth(value, maxBytes)
	if padded == nil {
		return nil, fmt.Errorf("codec.LPMMask: value longer than %d bytes", maxBytes)
	}
	// Zero out everything after prefixLen bits.
	masked := make([]byte, len(padded))
	copy(masked, padded)
	fullBytes := prefixLen / 8
	tailBits := prefixLen % 8
	for i := fullBytes; i < len(masked); i++ {
		masked[i] = 0
	}
	if tailBits != 0 && fullBytes < len(masked) {
		keep := byte(0xff) << uint(8-tailBits)
		masked[fullBytes] = padded[fullBytes] & keep
	}
	return stripLeadingZeros(masked), nil
}

// TernaryMask builds a mask with the upper prefixLen bits set and the rest
// zero, sized for the given bit width. Useful when the caller wants a
// prefix-style ternary match without thinking about byte alignment.
func TernaryMask(prefixLen, bitwidth int) ([]byte, error) {
	if prefixLen < 0 || prefixLen > bitwidth {
		return nil, fmt.Errorf("codec.TernaryMask: prefixLen %d out of range 0..%d", prefixLen, bitwidth)
	}
	if prefixLen == 0 {
		return []byte{}, nil
	}
	maxBytes := byteLen(bitwidth)
	mask := make([]byte, maxBytes)
	fullBytes := prefixLen / 8
	for i := 0; i < fullBytes; i++ {
		mask[i] = 0xff
	}
	tail := prefixLen % 8
	if tail != 0 && fullBytes < maxBytes {
		mask[fullBytes] = byte(0xff) << uint(8-tail)
	}
	// Trim canonical.
	return stripLeadingZeros(mask), nil
}

// TernaryApply computes value & mask for a ternary match. value and mask are
// left-padded to the same length before the AND.
func TernaryApply(value, mask []byte, bitwidth int) ([]byte, error) {
	maxBytes := byteLen(bitwidth)
	v := padToWidth(value, maxBytes)
	m := padToWidth(mask, maxBytes)
	if v == nil || m == nil {
		return nil, errors.New("codec.TernaryApply: value or mask exceeds bit width")
	}
	out := make([]byte, maxBytes)
	for i := 0; i < maxBytes; i++ {
		out[i] = v[i] & m[i]
	}
	return stripLeadingZeros(out), nil
}

// ValidateRange checks that low <= high when both are interpreted as
// unsigned integers of the given bit width. It also verifies both endpoints
// fit within the width.
func ValidateRange(low, high []byte, bitwidth int) error {
	if _, err := EncodeBytes(low, bitwidth); err != nil {
		return fmt.Errorf("codec.ValidateRange low: %w", err)
	}
	if _, err := EncodeBytes(high, bitwidth); err != nil {
		return fmt.Errorf("codec.ValidateRange high: %w", err)
	}
	maxBytes := byteLen(bitwidth)
	lp := padToWidth(low, maxBytes)
	hp := padToWidth(high, maxBytes)
	for i := 0; i < maxBytes; i++ {
		if lp[i] == hp[i] {
			continue
		}
		if lp[i] > hp[i] {
			return fmt.Errorf("codec.ValidateRange: low > high")
		}
		break
	}
	return nil
}

// ParseHex parses a string of hexadecimal digits, optionally prefixed with
// "0x" and optionally containing colons, returning the canonical encoding.
func ParseHex(s string) ([]byte, error) {
	cleaned := strings.ToLower(strings.TrimPrefix(strings.ToLower(s), "0x"))
	cleaned = strings.ReplaceAll(cleaned, ":", "")
	if len(cleaned)%2 != 0 {
		cleaned = "0" + cleaned
	}
	raw, err := hex.DecodeString(cleaned)
	if err != nil {
		return nil, fmt.Errorf("codec.ParseHex: %w", err)
	}
	return stripLeadingZeros(raw), nil
}

// FormatHex renders v as a colon-separated hexadecimal literal (e.g.,
// "0a:0b:0c"). The empty slice renders as "0".
func FormatHex(v []byte) string {
	if len(v) == 0 {
		return "0"
	}
	parts := make([]string, len(v))
	for i, b := range v {
		parts[i] = strconv.FormatUint(uint64(b), 16)
		if len(parts[i]) == 1 {
			parts[i] = "0" + parts[i]
		}
	}
	return strings.Join(parts, ":")
}

func stripLeadingZeros(b []byte) []byte {
	i := 0
	for i < len(b) && b[i] == 0 {
		i++
	}
	if i == len(b) {
		return []byte{}
	}
	out := make([]byte, len(b)-i)
	copy(out, b[i:])
	return out
}

func byteLen(bitwidth int) int {
	return (bitwidth + 7) / 8
}

// padToWidth left-pads value with zeros to exactly width bytes. Returns nil
// if value is already longer than width.
func padToWidth(value []byte, width int) []byte {
	if len(value) > width {
		return nil
	}
	out := make([]byte, width)
	copy(out[width-len(value):], value)
	return out
}
