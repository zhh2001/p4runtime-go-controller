package codec_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	errs "github.com/zhh2001/p4runtime-go-controller/errors"
	"github.com/zhh2001/p4runtime-go-controller/internal/codec"
)

func TestEncodeUint(t *testing.T) {
	cases := []struct {
		v             uint64
		width         int
		want          []byte
		wantErr       bool
		containsIsErr error
	}{
		{v: 0, width: 8, want: []byte{}},
		{v: 0, width: 32, want: []byte{}},
		{v: 1, width: 8, want: []byte{0x01}},
		{v: 255, width: 8, want: []byte{0xff}},
		{v: 256, width: 16, want: []byte{0x01, 0x00}},
		{v: 0xdeadbeef, width: 32, want: []byte{0xde, 0xad, 0xbe, 0xef}},
		{v: 0xffffffff, width: 32, want: []byte{0xff, 0xff, 0xff, 0xff}},
		{v: 0x1ff, width: 9, want: []byte{0x01, 0xff}},
		{v: 1<<20 - 1, width: 20, want: []byte{0x0f, 0xff, 0xff}},
		// full 64-bit
		{v: ^uint64(0), width: 64, want: bytes.Repeat([]byte{0xff}, 8)},
		// errors
		{v: 256, width: 8, wantErr: true, containsIsErr: errs.ErrInvalidBitWidth},
		{v: 1, width: 0, wantErr: true},
		{v: 1, width: 65, wantErr: true},
	}
	for _, tc := range cases {
		got, err := codec.EncodeUint(tc.v, tc.width)
		if tc.wantErr {
			require.Error(t, err, "v=%d w=%d", tc.v, tc.width)
			if tc.containsIsErr != nil {
				assert.True(t, errors.Is(err, tc.containsIsErr))
			}
			continue
		}
		require.NoError(t, err, "v=%d w=%d", tc.v, tc.width)
		assert.Equal(t, tc.want, got, "v=%d w=%d", tc.v, tc.width)
	}
}

func TestMustEncodeUint(t *testing.T) {
	assert.Equal(t, []byte{0x01}, codec.MustEncodeUint(1, 8))
	assert.Panics(t, func() { codec.MustEncodeUint(256, 8) })
}

func TestEncodeBytes(t *testing.T) {
	cases := []struct {
		name    string
		in      []byte
		width   int
		want    []byte
		wantErr bool
	}{
		{name: "empty", in: []byte{}, width: 8, want: []byte{}},
		{name: "zeros only", in: []byte{0x00, 0x00}, width: 16, want: []byte{}},
		{name: "strip leading", in: []byte{0x00, 0x00, 0xab}, width: 24, want: []byte{0xab}},
		{name: "full 16", in: []byte{0xab, 0xcd}, width: 16, want: []byte{0xab, 0xcd}},
		{name: "too wide", in: []byte{0x01, 0x00}, width: 8, wantErr: true},
		{name: "bits set outside width", in: []byte{0x03, 0xff}, width: 9, wantErr: true},
		{name: "aligned 9-bit ok", in: []byte{0x01, 0xff}, width: 9, want: []byte{0x01, 0xff}},
		{name: "bad width", in: []byte{0x01}, width: 0, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := codec.EncodeBytes(tc.in, tc.width)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDecodeUint(t *testing.T) {
	cases := []struct {
		in   []byte
		want uint64
		err  bool
	}{
		{in: []byte{}, want: 0},
		{in: []byte{0x01}, want: 1},
		{in: []byte{0x01, 0x00}, want: 256},
		{in: []byte{0xde, 0xad, 0xbe, 0xef}, want: 0xdeadbeef},
		{in: bytes.Repeat([]byte{0xff}, 8), want: ^uint64(0)},
		{in: bytes.Repeat([]byte{0xff}, 9), err: true},
	}
	for _, tc := range cases {
		got, err := codec.DecodeUint(tc.in)
		if tc.err {
			assert.Error(t, err)
			continue
		}
		require.NoError(t, err)
		assert.Equal(t, tc.want, got)
	}
}

func TestMAC(t *testing.T) {
	b, err := codec.MAC("00:11:22:33:44:55")
	require.NoError(t, err)
	assert.Equal(t, []byte{0x11, 0x22, 0x33, 0x44, 0x55}, b) // leading 00 stripped

	b, err = codec.MAC("ff-ff-ff-ff-ff-ff")
	require.NoError(t, err)
	assert.Equal(t, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, b)

	b, err = codec.MAC("aabbccddeeff")
	require.NoError(t, err)
	assert.Equal(t, []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}, b)

	_, err = codec.MAC("not-a-mac")
	assert.Error(t, err)

	_, err = codec.MAC("gg:hh:ii:jj:kk:ll")
	assert.Error(t, err)
}

func TestMustMAC(t *testing.T) {
	assert.Equal(t, []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}, codec.MustMAC("01:02:03:04:05:06"))
	assert.Panics(t, func() { codec.MustMAC("bogus") })
}

func TestIPv4(t *testing.T) {
	b, err := codec.IPv4("10.0.0.1")
	require.NoError(t, err)
	assert.Equal(t, []byte{0x0a, 0x00, 0x00, 0x01}, b)

	b, err = codec.IPv4("0.0.0.0")
	require.NoError(t, err)
	assert.Equal(t, []byte{}, b)

	_, err = codec.IPv4("::1")
	assert.Error(t, err)

	_, err = codec.IPv4("bogus")
	assert.Error(t, err)
}

func TestMustIPv4(t *testing.T) {
	assert.NotEmpty(t, codec.MustIPv4("1.2.3.4"))
	assert.Panics(t, func() { codec.MustIPv4("nope") })
}

func TestIPv6(t *testing.T) {
	b, err := codec.IPv6("2001:db8::1")
	require.NoError(t, err)
	assert.Equal(t, byte(0x20), b[0])
	assert.Equal(t, byte(0x01), b[len(b)-1])

	_, err = codec.IPv6("10.0.0.1")
	assert.Error(t, err)

	_, err = codec.IPv6("::ffff:10.0.0.1") // v4-mapped
	assert.Error(t, err)
}

func TestMustIPv6(t *testing.T) {
	assert.NotEmpty(t, codec.MustIPv6("::1"))
	assert.Panics(t, func() { codec.MustIPv6("garbage") })
}

func TestLPMMask(t *testing.T) {
	// 10.0.0.0/8
	v, _ := codec.IPv4("10.1.2.3")
	b, err := codec.LPMMask(v, 8, 32)
	require.NoError(t, err)
	assert.Equal(t, []byte{0x0a, 0x00, 0x00, 0x00}, b)

	// 10.1.0.0/16
	b, err = codec.LPMMask(v, 16, 32)
	require.NoError(t, err)
	assert.Equal(t, []byte{0x0a, 0x01, 0x00, 0x00}, b)

	// 10.1.2.0/23  → 0a 01 02 00 (canonical strips leading zeros only)
	v, _ = codec.IPv4("10.1.3.0")
	b, err = codec.LPMMask(v, 23, 32)
	require.NoError(t, err)
	assert.Equal(t, []byte{0x0a, 0x01, 0x02, 0x00}, b)

	// prefix 0 → empty
	b, err = codec.LPMMask(v, 0, 32)
	require.NoError(t, err)
	assert.Equal(t, []byte{}, b)

	// errors
	_, err = codec.LPMMask(v, -1, 32)
	assert.Error(t, err)
	_, err = codec.LPMMask(v, 33, 32)
	assert.Error(t, err)
	_, err = codec.LPMMask(v, 8, 0)
	assert.Error(t, err)
	_, err = codec.LPMMask(bytes.Repeat([]byte{0xff}, 5), 8, 32)
	assert.Error(t, err)
}

func TestTernaryMask(t *testing.T) {
	b, err := codec.TernaryMask(24, 32)
	require.NoError(t, err)
	assert.Equal(t, []byte{0xff, 0xff, 0xff, 0x00}, b)

	b, err = codec.TernaryMask(20, 32)
	require.NoError(t, err)
	assert.Equal(t, []byte{0xff, 0xff, 0xf0, 0x00}, b)

	b, err = codec.TernaryMask(0, 32)
	require.NoError(t, err)
	assert.Equal(t, []byte{}, b)

	_, err = codec.TernaryMask(33, 32)
	assert.Error(t, err)
}

func TestTernaryApply(t *testing.T) {
	v, _ := codec.IPv4("10.1.2.3")
	m := []byte{0xff, 0xff, 0xff, 0x00}
	got, err := codec.TernaryApply(v, m, 32)
	require.NoError(t, err)
	assert.Equal(t, []byte{0x0a, 0x01, 0x02, 0x00}, got)

	_, err = codec.TernaryApply(bytes.Repeat([]byte{0xff}, 6), m, 32)
	assert.Error(t, err)
}

func TestValidateRange(t *testing.T) {
	require.NoError(t, codec.ValidateRange([]byte{0x01}, []byte{0x02}, 8))
	require.NoError(t, codec.ValidateRange([]byte{0x01}, []byte{0x01}, 8))
	require.Error(t, codec.ValidateRange([]byte{0x02}, []byte{0x01}, 8))
	require.Error(t, codec.ValidateRange([]byte{0xff, 0xff}, []byte{0x01}, 8))
}

func TestParseHex(t *testing.T) {
	cases := []struct {
		in   string
		want []byte
		err  bool
	}{
		{in: "0x0a", want: []byte{0x0a}},
		{in: "0a:0b:0c", want: []byte{0x0a, 0x0b, 0x0c}},
		{in: "aBcDeF", want: []byte{0xab, 0xcd, 0xef}},
		{in: "0", want: []byte{}},
		{in: "0x", want: []byte{}},
		{in: "gg", err: true},
	}
	for _, tc := range cases {
		got, err := codec.ParseHex(tc.in)
		if tc.err {
			assert.Error(t, err)
			continue
		}
		require.NoError(t, err)
		assert.Equal(t, tc.want, got, "in=%q", tc.in)
	}
}

func TestFormatHex(t *testing.T) {
	assert.Equal(t, "0", codec.FormatHex([]byte{}))
	assert.Equal(t, "0a:0b", codec.FormatHex([]byte{0x0a, 0x0b}))
	assert.Equal(t, "ff", codec.FormatHex([]byte{0xff}))
}
