package register

import (
	"bytes"
	"fmt"
	"math/bits"

	errs "github.com/zhh2001/p4runtime-go-controller/errors"
	"github.com/zhh2001/p4runtime-go-controller/internal/codec"
)

func unsignedBytes(value []byte, width int32) ([]byte, error) {
	if width < 0 {
		return nil, fmt.Errorf("negative bitwidth %d: %w", width, errs.ErrInvalidBitWidth)
	}
	if width == 0 {
		for _, b := range value {
			if b != 0 {
				return nil, fmt.Errorf("non-zero value for bit<0>: %w", errs.ErrInvalidBitWidth)
			}
		}
		return []byte{0}, nil
	}
	return codec.EncodeBytes(value, int(width))
}

func signedBytes(value []byte, width int32) ([]byte, error) {
	if width <= 0 {
		return nil, fmt.Errorf("signed bitwidth %d must be positive: %w", width, errs.ErrInvalidBitWidth)
	}
	if len(value) == 0 {
		return []byte{0}, nil
	}
	for len(value) > 1 {
		if (value[0] == 0 && value[1]&0x80 == 0) || (value[0] == 0xff && value[1]&0x80 != 0) {
			value = value[1:]
		} else {
			break
		}
	}
	if len(value)-1 > (int(width)-1)/8 {
		return nil, fmt.Errorf("value exceeds int<%d>: %w", width, errs.ErrInvalidBitWidth)
	}
	var sign byte
	if value[0]&0x80 != 0 {
		sign = 0xff
	}
	remaining := int(width) - (len(value)-1)*8
	if 9-bits.LeadingZeros8(value[0]^sign) > remaining {
		return nil, fmt.Errorf("value exceeds int<%d>: %w", width, errs.ErrInvalidBitWidth)
	}
	return bytes.Clone(value), nil
}
