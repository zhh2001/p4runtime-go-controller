package codec_test

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	errs "github.com/zhh2001/p4runtime-go-controller/v2/errors"
	"github.com/zhh2001/p4runtime-go-controller/v2/internal/codec"
)

func TestLPMMask_FieldAlignment(t *testing.T) {
	for _, tc := range []struct {
		name   string
		value  []byte
		prefix int
		width  int
		want   []byte
	}{
		{name: "one bit", value: []byte{1}, prefix: 1, width: 1, want: []byte{1}},
		{name: "seven bits", value: []byte{0x7f}, prefix: 3, width: 7, want: []byte{0x70}},
		{name: "nine bit full prefix", value: []byte{1, 0xff}, prefix: 9, width: 9, want: []byte{1, 0xff}},
		{name: "nine bit first bit", value: []byte{1, 0xff}, prefix: 1, width: 9, want: []byte{1, 0}},
		{name: "nine bit partial prefix", value: []byte{1, 0xff}, prefix: 8, width: 9, want: []byte{1, 0xfe}},
		{name: "short value", value: []byte{0xff}, prefix: 8, width: 9, want: []byte{0xfe}},
		{name: "short value cleared", value: []byte{0xff}, prefix: 1, width: 9, want: []byte{0}},
		{name: "leading zeros", value: []byte{0, 0, 1, 0xff}, prefix: 9, width: 9, want: []byte{1, 0xff}},
		{name: "twelve bits", value: []byte{0x0a, 0xbc}, prefix: 9, width: 12, want: []byte{0x0a, 0xb8}},
		{name: "twenty bits", value: []byte{0x0a, 0xbc, 0xde}, prefix: 13, width: 20, want: []byte{0x0a, 0xbc, 0x80}},
		{name: "wide full prefix", value: append([]byte{1}, bytes.Repeat([]byte{0xff}, 8)...), prefix: 65, width: 65, want: append([]byte{1}, bytes.Repeat([]byte{0xff}, 8)...)},
		{name: "wide first bit", value: append([]byte{1}, bytes.Repeat([]byte{0xff}, 8)...), prefix: 1, width: 65, want: append([]byte{1}, make([]byte, 8)...)},
		{name: "zero prefix", value: []byte{1, 0xff}, prefix: 0, width: 9, want: []byte{0}},
		{name: "nil value", prefix: 9, width: 9, want: []byte{0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := codec.LPMMask(tc.value, tc.prefix, tc.width)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestTernaryMask_FieldAlignment(t *testing.T) {
	for _, tc := range []struct {
		prefix int
		width  int
		want   []byte
	}{
		{prefix: 1, width: 1, want: []byte{1}},
		{prefix: 3, width: 7, want: []byte{0x70}},
		{prefix: 8, width: 8, want: []byte{0xff}},
		{prefix: 0, width: 9, want: []byte{0}},
		{prefix: 1, width: 9, want: []byte{1, 0}},
		{prefix: 8, width: 9, want: []byte{1, 0xfe}},
		{prefix: 9, width: 9, want: []byte{1, 0xff}},
		{prefix: 9, width: 12, want: []byte{0x0f, 0xf8}},
		{prefix: 13, width: 20, want: []byte{0x0f, 0xff, 0x80}},
		{prefix: 65, width: 65, want: append([]byte{1}, bytes.Repeat([]byte{0xff}, 8)...)},
	} {
		got, err := codec.TernaryMask(tc.prefix, tc.width)
		require.NoError(t, err, "prefix=%d width=%d", tc.prefix, tc.width)
		require.Equal(t, tc.want, got, "prefix=%d width=%d", tc.prefix, tc.width)
	}
}

func TestMasks_RejectOutOfWidthInputs(t *testing.T) {
	for _, prefix := range []int{0, 1, 9} {
		_, err := codec.LPMMask([]byte{3, 0xff}, prefix, 9)
		require.ErrorIs(t, err, errs.ErrInvalidBitWidth, "prefix=%d", prefix)
	}
	for _, tc := range []struct {
		name  string
		value []byte
		mask  []byte
	}{
		{name: "value overflow", value: []byte{3, 0xff}, mask: []byte{1, 0xff}},
		{name: "mask overflow", value: []byte{1, 0xff}, mask: []byte{3, 0xff}},
		{name: "both overflow", value: []byte{3, 0xff}, mask: []byte{3, 0xff}},
		{name: "overflow with zero mask", value: []byte{3, 0xff}, mask: []byte{0}},
		{name: "zero with overflow mask", value: []byte{0}, mask: []byte{3, 0xff}},
		{name: "excess significant bytes", value: []byte{1, 0, 0}, mask: []byte{1, 0xff}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := codec.TernaryApply(tc.value, tc.mask, 9)
			require.ErrorIs(t, err, errs.ErrInvalidBitWidth)
		})
	}
	_, err := codec.LPMMask([]byte{1, 0}, 8, 8)
	require.ErrorIs(t, err, errs.ErrInvalidBitWidth)
	_, err = codec.TernaryApply([]byte{0}, []byte{1, 0}, 8)
	require.ErrorIs(t, err, errs.ErrInvalidBitWidth)
}

func TestMasks_InvalidArguments(t *testing.T) {
	for _, width := range []int{-16, -1, 0} {
		var lpmErr, maskErr, applyErr error
		require.NotPanics(t, func() {
			_, lpmErr = codec.LPMMask(nil, 0, width)
			_, maskErr = codec.TernaryMask(0, width)
			_, applyErr = codec.TernaryApply(nil, nil, width)
		}, "width=%d", width)
		require.Error(t, lpmErr)
		require.Error(t, maskErr)
		require.Error(t, applyErr)
	}
	for _, prefix := range []int{-1, 10} {
		_, err := codec.LPMMask([]byte{1}, prefix, 9)
		require.Error(t, err)
		_, err = codec.TernaryMask(prefix, 9)
		require.Error(t, err)
	}
}

func TestTernaryApply_LeadingZeros(t *testing.T) {
	for _, tc := range []struct {
		value []byte
		mask  []byte
		want  []byte
	}{
		{value: []byte{0, 0, 1, 0xff}, mask: []byte{0, 0xff}, want: []byte{0xff}},
		{value: []byte{0xff}, mask: []byte{0, 0, 1, 0xfe}, want: []byte{0xfe}},
		{value: []byte{0, 0, 0}, mask: []byte{1, 0xff}, want: []byte{0}},
		{value: []byte{1, 0xff}, mask: []byte{0, 0, 0}, want: []byte{0}},
		{value: nil, mask: []byte{1, 0xff}, want: []byte{0}},
	} {
		got, err := codec.TernaryApply(tc.value, tc.mask, 9)
		require.NoError(t, err)
		require.Equal(t, tc.want, got)
	}
}

func TestMasks_InputOwnership(t *testing.T) {
	value := []byte{0, 1, 0xff}
	mask := []byte{0, 1, 0xfe}
	valueBefore := bytes.Clone(value)
	maskBefore := bytes.Clone(mask)
	lpm, err := codec.LPMMask(value, 8, 9)
	require.NoError(t, err)
	ternary, err := codec.TernaryApply(value, mask, 9)
	require.NoError(t, err)
	alias, err := codec.TernaryApply(value, value, 9)
	require.NoError(t, err)
	lpm[0], ternary[0], alias[0] = 0, 0, 0
	require.Equal(t, valueBefore, value)
	require.Equal(t, maskBefore, mask)
}

func canonicalNumber(n *big.Int) []byte {
	if n.Sign() == 0 {
		return []byte{0}
	}
	return n.Bytes()
}

func TestMasks_AllFieldWidths(t *testing.T) {
	for width := 1; width <= 129; width++ {
		limit := new(big.Int).Lsh(big.NewInt(1), uint(width))
		maximum := new(big.Int).Sub(limit, big.NewInt(1))
		for prefix := 0; prefix <= width; prefix++ {
			wantMask := new(big.Int).Lsh(big.NewInt(1), uint(prefix))
			wantMask.Sub(wantMask, big.NewInt(1)).Lsh(wantMask, uint(width-prefix))
			mask, err := codec.TernaryMask(prefix, width)
			require.NoError(t, err)
			require.Equal(t, canonicalNumber(wantMask), mask, "prefix=%d width=%d", prefix, width)
			for _, value := range []*big.Int{big.NewInt(0), big.NewInt(1), maximum} {
				want := new(big.Int).Rsh(new(big.Int).Set(value), uint(width-prefix))
				want.Lsh(want, uint(width-prefix))
				input := append([]byte{0, 0}, value.Bytes()...)
				lpm, err := codec.LPMMask(input, prefix, width)
				require.NoError(t, err)
				require.Equal(t, canonicalNumber(want), lpm, "prefix=%d width=%d value=%s", prefix, width, value)
				ternary, err := codec.TernaryApply(input, mask, width)
				require.NoError(t, err)
				require.Equal(t, lpm, ternary)
			}
		}
	}
}

func FuzzMasks_Numeric(f *testing.F) {
	f.Add([]byte{1, 0xff}, []byte{1, 0xff}, uint8(9), uint8(9))
	f.Add([]byte{0, 0, 1, 0xff}, []byte{0xff}, uint8(9), uint8(8))
	f.Add([]byte{3, 0xff}, []byte{1, 0xff}, uint8(9), uint8(0))
	f.Add([]byte{1, 0xff}, []byte{3, 0xff}, uint8(9), uint8(1))
	f.Add([]byte{}, []byte{}, uint8(1), uint8(0))
	f.Add(bytes.Repeat([]byte{0xff}, 16), []byte{0xff}, uint8(128), uint8(65))
	f.Fuzz(func(t *testing.T, value, mask []byte, fieldWidth, prefixSeed uint8) {
		width := max(1, int(fieldWidth))
		prefix := int(prefixSeed) % (width + 1)
		v, m := new(big.Int).SetBytes(value), new(big.Int).SetBytes(mask)
		lpm, err := codec.LPMMask(value, prefix, width)
		if v.BitLen() > width {
			require.ErrorIs(t, err, errs.ErrInvalidBitWidth)
		} else {
			require.NoError(t, err)
			want := new(big.Int).Rsh(new(big.Int).Set(v), uint(width-prefix))
			want.Lsh(want, uint(width-prefix))
			require.Equal(t, canonicalNumber(want), lpm)
		}
		ternary, err := codec.TernaryApply(value, mask, width)
		if v.BitLen() > width || m.BitLen() > width {
			require.ErrorIs(t, err, errs.ErrInvalidBitWidth)
		} else {
			require.NoError(t, err)
			require.Equal(t, canonicalNumber(new(big.Int).And(v, m)), ternary)
		}
		generated, err := codec.TernaryMask(prefix, width)
		require.NoError(t, err)
		wantMask := new(big.Int).Lsh(big.NewInt(1), uint(prefix))
		wantMask.Sub(wantMask, big.NewInt(1)).Lsh(wantMask, uint(width-prefix))
		require.Equal(t, canonicalNumber(wantMask), generated)
	})
}
