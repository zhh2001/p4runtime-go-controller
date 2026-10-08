package codec_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/zhh2001/p4runtime-go-controller/codec"
	errs "github.com/zhh2001/p4runtime-go-controller/errors"
)

func TestZeroEncodings(t *testing.T) {
	for _, tc := range []struct {
		name   string
		encode func() ([]byte, error)
	}{
		{name: "uint", encode: func() ([]byte, error) { return codec.EncodeUint(0, 9) }},
		{name: "nil", encode: func() ([]byte, error) { return codec.EncodeBytes(nil, 9) }},
		{name: "empty", encode: func() ([]byte, error) { return codec.EncodeBytes([]byte{}, 9) }},
		{name: "wide bytes", encode: func() ([]byte, error) { return codec.EncodeBytes(make([]byte, 16), 128) }},
		{name: "MAC", encode: func() ([]byte, error) { return codec.MAC("00:00:00:00:00:00") }},
		{name: "IPv4", encode: func() ([]byte, error) { return codec.IPv4("0.0.0.0") }},
		{name: "IPv6", encode: func() ([]byte, error) { return codec.IPv6("::") }},
		{name: "hex", encode: func() ([]byte, error) { return codec.ParseHex("0x00") }},
		{name: "LPM", encode: func() ([]byte, error) { return codec.LPMMask([]byte{1}, 8, 32) }},
		{name: "LPM zero prefix", encode: func() ([]byte, error) { return codec.LPMMask([]byte{1}, 0, 32) }},
		{name: "ternary mask", encode: func() ([]byte, error) { return codec.TernaryMask(0, 32) }},
		{name: "ternary value", encode: func() ([]byte, error) { return codec.TernaryApply([]byte{1}, []byte{0xf0}, 8) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := tc.encode()
			require.NoError(t, err)
			require.Equal(t, []byte{0x00}, encoded)
			value, err := codec.DecodeUint(encoded)
			require.NoError(t, err)
			require.Zero(t, value)
			require.Equal(t, "00", codec.FormatHex(encoded))
		})
	}
}

func TestMustZeroEncodings(t *testing.T) {
	for name, encoded := range map[string][]byte{
		"uint": codec.MustEncodeUint(0, 64),
		"MAC":  codec.MustMAC("00:00:00:00:00:00"),
		"IPv4": codec.MustIPv4("0.0.0.0"),
		"IPv6": codec.MustIPv6("::"),
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, []byte{0x00}, encoded)
		})
	}
}

func TestMasks_FieldWidth(t *testing.T) {
	mask, err := codec.TernaryMask(8, 9)
	require.NoError(t, err)
	require.Equal(t, []byte{1, 0xfe}, mask)
	value, err := codec.LPMMask([]byte{0, 1, 0xff}, 8, 9)
	require.NoError(t, err)
	require.Equal(t, []byte{1, 0xfe}, value)
	value, err = codec.TernaryApply([]byte{0, 1, 0xff}, mask, 9)
	require.NoError(t, err)
	require.Equal(t, []byte{1, 0xfe}, value)
	_, err = codec.LPMMask([]byte{3, 0xff}, 9, 9)
	require.ErrorIs(t, err, errs.ErrInvalidBitWidth)
	_, err = codec.TernaryApply([]byte{1, 0xff}, []byte{3, 0xff}, 9)
	require.ErrorIs(t, err, errs.ErrInvalidBitWidth)
}
