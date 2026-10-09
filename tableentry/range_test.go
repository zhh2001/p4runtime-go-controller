package tableentry_test

import (
	"testing"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/require"

	errs "github.com/zhh2001/p4runtime-go-controller/v2/errors"
	"github.com/zhh2001/p4runtime-go-controller/v2/tableentry"
)

func TestBuilder_RangeNormalizedEndpoints(t *testing.T) {
	for _, tc := range []struct {
		name     string
		low      []byte
		high     []byte
		wantLow  []byte
		wantHigh []byte
	}{
		{name: "padded low", low: []byte{0, 0, 1}, high: []byte{2}, wantLow: []byte{1}, wantHigh: []byte{2}},
		{name: "padded high", low: []byte{1}, high: []byte{0, 0, 2}, wantLow: []byte{1}, wantHigh: []byte{2}},
		{name: "equal padded endpoints", low: []byte{0, 0, 1}, high: []byte{0, 0, 0, 1}, wantLow: []byte{1}, wantHigh: []byte{1}},
		{name: "byte boundary", low: []byte{0, 0, 0xff}, high: []byte{0, 1, 0}, wantLow: []byte{0xff}, wantHigh: []byte{1, 0}},
		{name: "zero endpoints", low: []byte{0, 0, 0}, high: nil, wantLow: []byte{0}, wantHigh: []byte{0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			builder := tableentry.NewBuilder(fixturePipeline(t), "ingress.t_tcam").
				Match("hdr.tcp.port", tableentry.Range(tc.low, tc.high)).
				Action("forward", tableentry.Param("port", []byte{1})).Priority(10)
			var entry *p4v1.TableEntry
			var err error
			require.NotPanics(t, func() { entry, err = builder.Build() })
			require.NoError(t, err)
			entry = roundTripEntry(t, entry)
			require.Len(t, entry.Match, 1)
			match := entry.Match[0].GetRange()
			require.NotNil(t, match)
			require.Equal(t, tc.wantLow, match.Low)
			require.Equal(t, tc.wantHigh, match.High)
		})
	}
}

func TestBuilder_RangeInvalidEndpoints(t *testing.T) {
	for _, tc := range []struct {
		name string
		low  []byte
		high []byte
	}{
		{name: "reversed", low: []byte{0, 0, 2}, high: []byte{0, 0, 1}},
		{name: "low overflow", low: []byte{1, 0, 0}, high: []byte{0xff, 0xff}},
		{name: "high overflow", low: []byte{1}, high: []byte{1, 0, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var entry *p4v1.TableEntry
			var err error
			require.NotPanics(t, func() {
				entry, err = tableentry.NewBuilder(fixturePipeline(t), "ingress.t_tcam").
					Match("hdr.tcp.port", tableentry.Range(tc.low, tc.high)).
					Action("forward", tableentry.Param("port", []byte{1})).Priority(10).Build()
			})
			require.Nil(t, entry)
			if tc.name == "reversed" {
				require.ErrorContains(t, err, "low > high")
			} else {
				require.ErrorIs(t, err, errs.ErrInvalidBitWidth)
			}
		})
	}
}
