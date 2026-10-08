package tableentry_test

import (
	"testing"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	errs "github.com/zhh2001/p4runtime-go-controller/errors"
	"github.com/zhh2001/p4runtime-go-controller/internal/codec"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
	"github.com/zhh2001/p4runtime-go-controller/tableentry"
)

func maskPipeline(t *testing.T) *pipeline.Pipeline {
	t.Helper()
	info := proto.Clone(fixturePipeline(t).Info()).(*p4configv1.P4Info)
	info.Tables[1].MatchFields[0].Bitwidth = 9
	info.Tables[2].MatchFields[0].Bitwidth = 9
	p, err := pipeline.New(info, nil)
	require.NoError(t, err)
	return p
}

func TestBuilder_LPMFieldWidth(t *testing.T) {
	for _, tc := range []struct {
		prefix int32
		want   []byte
	}{
		{prefix: 1, want: []byte{1, 0}},
		{prefix: 8, want: []byte{1, 0xfe}},
		{prefix: 9, want: []byte{1, 0xff}},
	} {
		entry, err := tableentry.NewBuilder(maskPipeline(t), "ingress.t_lpm").
			Match("hdr.ipv4.dst", tableentry.LPM([]byte{0, 0, 1, 0xff}, tc.prefix)).
			Action("forward", tableentry.Param("port", []byte{1})).
			Build()
		require.NoError(t, err)
		entry = roundTripEntry(t, entry)
		require.Len(t, entry.Match, 1)
		lpm := entry.Match[0].GetLpm()
		require.NotNil(t, lpm)
		require.Equal(t, tc.prefix, lpm.PrefixLen)
		require.Equal(t, tc.want, lpm.Value)
	}
}

func TestBuilder_TernaryFieldWidth(t *testing.T) {
	for _, tc := range []struct {
		name string
		mask []byte
		want []byte
	}{
		{name: "full field", mask: []byte{1, 0xff}, want: []byte{1, 0xff}},
		{name: "partial field", mask: []byte{1, 0xfe}, want: []byte{1, 0xfe}},
		{name: "short mask", mask: []byte{0xff}, want: []byte{0xff}},
		{name: "padded mask", mask: []byte{0, 0, 0xff}, want: []byte{0xff}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry, err := tableentry.NewBuilder(maskPipeline(t), "ingress.t_tcam").
				Match("hdr.ipv4.dst", tableentry.Ternary([]byte{0, 0, 1, 0xff}, tc.mask)).
				Action("forward", tableentry.Param("port", []byte{1})).
				Priority(10).
				Build()
			require.NoError(t, err)
			entry = roundTripEntry(t, entry)
			require.Len(t, entry.Match, 1)
			ternary := entry.Match[0].GetTernary()
			require.NotNil(t, ternary)
			require.Equal(t, tc.want, ternary.Value)
			require.Equal(t, tc.want, ternary.Mask)
		})
	}
}

func TestBuilder_MaskWidthValidation(t *testing.T) {
	_, err := tableentry.NewBuilder(maskPipeline(t), "ingress.t_lpm").
		Match("hdr.ipv4.dst", tableentry.LPM([]byte{3, 0xff}, 9)).
		Action("forward", tableentry.Param("port", []byte{1})).Build()
	require.ErrorIs(t, err, errs.ErrInvalidBitWidth)
	for _, tc := range []struct {
		value []byte
		mask  []byte
	}{
		{value: []byte{3, 0xff}, mask: []byte{1, 0xff}},
		{value: []byte{1, 0xff}, mask: []byte{3, 0xff}},
	} {
		_, err := tableentry.NewBuilder(maskPipeline(t), "ingress.t_tcam").
			Match("hdr.ipv4.dst", tableentry.Ternary(tc.value, tc.mask)).
			Action("forward", tableentry.Param("port", []byte{1})).
			Priority(10).Build()
		require.ErrorIs(t, err, errs.ErrInvalidBitWidth)
	}
}

func TestBuilder_MaskHelpersKeepWildcards(t *testing.T) {
	value, err := codec.LPMMask([]byte{1, 0xff}, 0, 9)
	require.NoError(t, err)
	entry, err := tableentry.NewBuilder(maskPipeline(t), "ingress.t_lpm").
		Match("hdr.ipv4.dst", tableentry.LPM(value, 0)).
		Action("forward", tableentry.Param("port", []byte{1})).Build()
	require.NoError(t, err)
	require.Empty(t, roundTripEntry(t, entry).Match)

	mask, err := codec.TernaryMask(0, 9)
	require.NoError(t, err)
	entry, err = tableentry.NewBuilder(maskPipeline(t), "ingress.t_tcam").
		Match("hdr.ipv4.dst", tableentry.Ternary([]byte{1, 0xff}, mask)).
		Action("forward", tableentry.Param("port", []byte{1})).
		Priority(10).Build()
	require.NoError(t, err)
	require.Empty(t, roundTripEntry(t, entry).Match)
}
