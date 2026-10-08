package cmd

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"net/netip"
	"strings"

	errs "github.com/zhh2001/p4runtime-go-controller/errors"
	"github.com/zhh2001/p4runtime-go-controller/internal/codec"
)

// decodeValue accepts unsigned decimal, 0x hex, colon-separated hex bytes,
// IPv4 and IPv6. An explicit 0x prefix takes priority over IP parsing.
func decodeValue(raw string, bitwidth int) ([]byte, error) {
	if bitwidth <= 0 {
		return nil, fmt.Errorf("value %q: bitwidth %d must be positive", raw, bitwidth)
	}
	if raw == "" {
		return nil, fmt.Errorf("value must not be empty")
	}
	var value []byte
	var err error
	switch {
	case strings.HasPrefix(raw, "0x") || strings.HasPrefix(raw, "0X"):
		value, err = decodeHex(raw[2:])
	case strings.ContainsAny(raw, ":."):
		if address, parseErr := netip.ParseAddr(raw); parseErr == nil {
			if address.Zone() != "" {
				return nil, fmt.Errorf("value %q: IP zones cannot be encoded in a P4 field", raw)
			}
			value = address.AsSlice()
		} else if strings.Contains(raw, ":") {
			value, err = decodeHex(raw)
		} else {
			return nil, fmt.Errorf("value %q: %w", raw, parseErr)
		}
	default:
		for _, digit := range raw {
			if digit < '0' || digit > '9' {
				return nil, fmt.Errorf("value %q: expected an unsigned decimal, hex or IP literal", raw)
			}
		}
		number, ok := new(big.Int).SetString(raw, 10)
		if !ok {
			return nil, fmt.Errorf("value %q: invalid unsigned decimal", raw)
		}
		if number.BitLen() > bitwidth {
			return nil, fmt.Errorf("value %q exceeds %d-bit range: %w", raw, bitwidth, errs.ErrInvalidBitWidth)
		}
		value = number.Bytes()
	}
	if err != nil {
		return nil, fmt.Errorf("value %q: %w", raw, err)
	}
	value, err = codec.EncodeBytes(value, bitwidth)
	if err != nil {
		return nil, fmt.Errorf("value %q: %w", raw, err)
	}
	return value, nil
}

func decodeHex(raw string) ([]byte, error) {
	if raw == "" {
		return nil, fmt.Errorf("hex literal must not be empty")
	}
	if strings.Contains(raw, ":") {
		for _, part := range strings.Split(raw, ":") {
			if len(part) != 2 {
				return nil, fmt.Errorf("hex byte %q must contain two digits", part)
			}
		}
	}
	cleaned := strings.ReplaceAll(raw, ":", "")
	if len(cleaned)%2 != 0 {
		cleaned = "0" + cleaned
	}
	return hex.DecodeString(cleaned)
}
