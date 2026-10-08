package codec_test

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	errs "github.com/zhh2001/p4runtime-go-controller/errors"
	"github.com/zhh2001/p4runtime-go-controller/internal/codec"
)

func TestValidateRange_NormalizedEndpoints(t *testing.T) {
	for _, tc := range []struct {
		name    string
		low     []byte
		high    []byte
		width   int
		wantErr bool
	}{
		{name: "padded low", low: []byte{0, 1}, high: []byte{2}, width: 8},
		{name: "padded high", low: []byte{1}, high: []byte{0, 2}, width: 8},
		{name: "both padded", low: []byte{0, 0, 1}, high: []byte{0, 2}, width: 8},
		{name: "padded equality", low: []byte{0, 0, 1}, high: []byte{1}, width: 8},
		{name: "padded reverse", low: []byte{0, 2}, high: []byte{0, 0, 1}, width: 8, wantErr: true},
		{name: "nil zero", high: []byte{1}, width: 1},
		{name: "empty zero", low: []byte{}, high: []byte{}, width: 1},
		{name: "padded zero", low: make([]byte, 32), high: []byte{0}, width: 1},
		{name: "one above zero", low: []byte{1}, high: make([]byte, 32), width: 1, wantErr: true},
		{name: "byte boundary", low: []byte{0xff}, high: []byte{1, 0}, width: 9},
		{name: "reverse byte boundary", low: []byte{1, 0}, high: []byte{0xff}, width: 9, wantErr: true},
		{name: "nonaligned maximum", low: []byte{0, 1, 0xfe}, high: []byte{0, 0, 1, 0xff}, width: 9},
		{name: "wide boundary", low: bytes.Repeat([]byte{0xff}, 8), high: append([]byte{1}, make([]byte, 8)...), width: 65},
		{name: "wide equal", low: make([]byte, 32), high: nil, width: 128},
	} {
		t.Run(tc.name, func(t *testing.T) {
			beforeLow, beforeHigh := bytes.Clone(tc.low), bytes.Clone(tc.high)
			var err error
			require.NotPanics(t, func() { err = codec.ValidateRange(tc.low, tc.high, tc.width) })
			if tc.wantErr {
				require.ErrorContains(t, err, "low > high")
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, beforeLow, tc.low)
			require.Equal(t, beforeHigh, tc.high)
		})
	}
}

func TestValidateRange_WidthValidation(t *testing.T) {
	for _, tc := range []struct {
		endpoint string
		low      []byte
		high     []byte
	}{
		{endpoint: "low", low: []byte{2, 0}, high: []byte{1, 0xff}},
		{endpoint: "high", low: []byte{1}, high: []byte{0, 2, 0}},
	} {
		t.Run(tc.endpoint, func(t *testing.T) {
			var err error
			require.NotPanics(t, func() { err = codec.ValidateRange(tc.low, tc.high, 9) })
			require.ErrorIs(t, err, errs.ErrInvalidBitWidth)
			require.ErrorContains(t, err, tc.endpoint)
		})
	}
	for _, width := range []int{-16, -1, 0} {
		var err error
		require.NotPanics(t, func() { err = codec.ValidateRange(nil, nil, width) })
		require.Error(t, err)
	}
}

func TestValidateRange_AllFieldWidths(t *testing.T) {
	for width := 1; width <= 129; width++ {
		maximum := new(big.Int).Lsh(big.NewInt(1), uint(width))
		maximum.Sub(maximum, big.NewInt(1))
		middle := new(big.Int).Lsh(big.NewInt(1), uint(width-1))
		values := []*big.Int{big.NewInt(0), big.NewInt(1), middle, maximum}
		for _, low := range values {
			for _, high := range values {
				lowBytes := append(make([]byte, width/8+2), low.Bytes()...)
				highBytes := append(make([]byte, width/8+3), high.Bytes()...)
				err := codec.ValidateRange(lowBytes, highBytes, width)
				if low.Cmp(high) > 0 {
					require.ErrorContains(t, err, "low > high", "width=%d", width)
				} else {
					require.NoError(t, err, "width=%d", width)
				}
			}
		}
	}
}

func FuzzValidateRange_Numeric(f *testing.F) {
	f.Add([]byte{0, 1}, []byte{2}, int16(8))
	f.Add([]byte{2}, []byte{0, 1}, int16(8))
	f.Add([]byte{0, 0}, []byte{}, int16(1))
	f.Add([]byte{0xff}, []byte{1, 0}, int16(9))
	f.Add([]byte{1, 0}, []byte{0xff}, int16(9))
	f.Add([]byte{0, 2, 0}, []byte{1}, int16(9))
	f.Add([]byte{1}, []byte{0, 2, 0}, int16(9))
	f.Add(bytes.Repeat([]byte{0xff}, 8), append([]byte{1}, make([]byte, 8)...), int16(65))
	f.Add([]byte{}, []byte{}, int16(0))
	f.Add([]byte{1}, []byte{2}, int16(-1))
	f.Fuzz(func(t *testing.T, low, high []byte, fieldWidth int16) {
		width := int(fieldWidth)
		l, h := new(big.Int).SetBytes(low), new(big.Int).SetBytes(high)
		err := codec.ValidateRange(low, high, width)
		switch {
		case width <= 0:
			require.Error(t, err)
		case l.BitLen() > width || h.BitLen() > width:
			require.ErrorIs(t, err, errs.ErrInvalidBitWidth)
		case l.Cmp(h) > 0:
			require.ErrorContains(t, err, "low > high")
		default:
			require.NoError(t, err)
		}
	})
}
