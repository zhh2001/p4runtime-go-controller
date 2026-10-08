//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/codec"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
	"github.com/zhh2001/p4runtime-go-controller/tableentry"
)

func TestBMv2_FieldWidthMasks(t *testing.T) {
	infoPath, configPath := os.Getenv("P4RT_MASK_P4INFO"), os.Getenv("P4RT_MASK_DEVICE_CONFIG")
	if infoPath == "" && configPath == "" {
		t.Skip("P4RT_MASK_P4INFO and P4RT_MASK_DEVICE_CONFIG unset; skipping live mask tests")
	}
	require.NotEmpty(t, infoPath)
	require.NotEmpty(t, configPath)
	info, err := os.ReadFile(infoPath)
	require.NoError(t, err)
	config, err := os.ReadFile(configPath)
	require.NoError(t, err)
	p, err := pipeline.LoadText(info, config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, targetAddr(), client.WithDeviceID(1),
		client.WithElectionID(client.ElectionID{Low: 1}), client.WithInsecure())
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.BecomePrimary(ctx))
	_, err = c.SetPipeline(ctx, p, client.SetPipelineOptions{})
	require.NoError(t, err)

	for _, tc := range []struct {
		name     string
		table    string
		prefix   int32
		mask     []byte
		low      []byte
		high     []byte
		want     []byte
		wantHigh []byte
	}{
		{name: "lpm first bit", table: "MyIngress.t_lpm", prefix: 1, want: []byte{1, 0}},
		{name: "lpm partial prefix", table: "MyIngress.t_lpm", prefix: 8, want: []byte{1, 0xfe}},
		{name: "lpm full prefix", table: "MyIngress.t_lpm", prefix: 9, want: []byte{1, 0xff}},
		{name: "ternary first bit", table: "MyIngress.t_ternary", prefix: 1, want: []byte{1, 0}},
		{name: "ternary partial prefix", table: "MyIngress.t_ternary", prefix: 8, want: []byte{1, 0xfe}},
		{name: "ternary full prefix", table: "MyIngress.t_ternary", prefix: 9, want: []byte{1, 0xff}},
		{name: "ternary short mask", table: "MyIngress.t_ternary", mask: []byte{0xff}, want: []byte{0xff}},
		{name: "ternary padded mask", table: "MyIngress.t_ternary", mask: []byte{0, 0, 0xff}, want: []byte{0xff}},
		{name: "range padded low", table: "MyIngress.t_range", low: []byte{0, 0, 1}, high: []byte{2}, want: []byte{1}, wantHigh: []byte{2}},
		{name: "range padded high", table: "MyIngress.t_range", low: []byte{1}, high: []byte{0, 0, 2}, want: []byte{1}, wantHigh: []byte{2}},
		{name: "range equal endpoints", table: "MyIngress.t_range", low: []byte{0, 0, 1}, high: []byte{0, 0, 0, 1}, want: []byte{1}, wantHigh: []byte{1}},
		{name: "range byte boundary", table: "MyIngress.t_range", low: []byte{0, 0, 0xff}, high: []byte{0, 1, 0}, want: []byte{0xff}, wantHigh: []byte{1, 0}},
		{name: "range maximum", table: "MyIngress.t_range", low: []byte{0, 0, 1, 0xfe}, high: []byte{0, 0, 1, 0xff}, want: []byte{1, 0xfe}, wantHigh: []byte{1, 0xff}},
		{name: "range zero endpoints", table: "MyIngress.t_range", low: []byte{0, 0, 0}, want: []byte{0}, wantHigh: []byte{0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			table, ok := p.Table(tc.table)
			require.True(t, ok)
			require.Len(t, table.MatchFields, 1)
			require.EqualValues(t, 9, table.MatchFields[0].Bitwidth)
			b := tableentry.NewBuilder(p, tc.table).
				Action("MyIngress.forward", tableentry.Param("port", codec.MustEncodeUint(1, 9)))
			value := []byte{0, 0, 1, 0xff}
			switch tc.table {
			case "MyIngress.t_lpm":
				b.Match(table.MatchFields[0].Name, tableentry.LPM(value, tc.prefix))
			case "MyIngress.t_ternary":
				mask := tc.mask
				if mask == nil {
					mask, err = codec.TernaryMask(int(tc.prefix), 9)
					require.NoError(t, err)
				}
				b.Match(table.MatchFields[0].Name, tableentry.Ternary(value, mask)).Priority(10)
			case "MyIngress.t_range":
				b.Match(table.MatchFields[0].Name, tableentry.Range(tc.low, tc.high)).Priority(10)
			}
			entry, err := b.Build()
			require.NoError(t, err)
			require.Len(t, entry.Match, 1)
			switch tc.table {
			case "MyIngress.t_lpm":
				require.Equal(t, tc.want, entry.Match[0].GetLpm().Value)
			case "MyIngress.t_ternary":
				require.Equal(t, tc.want, entry.Match[0].GetTernary().Value)
				require.Equal(t, tc.want, entry.Match[0].GetTernary().Mask)
			case "MyIngress.t_range":
				require.Equal(t, tc.want, entry.Match[0].GetRange().Low)
				require.Equal(t, tc.wantHigh, entry.Match[0].GetRange().High)
			}
			err = c.WriteTableEntry(ctx, client.UpdateInsert, entry)
			require.NoError(t, err, "target error details: %v", status.Convert(err).Details())
			entries, err := c.ReadTableEntries(ctx, entry.TableId)
			require.NoError(t, err)
			var stored *p4v1.TableEntry
			for _, candidate := range entries {
				if len(candidate.Match) == 1 && proto.Equal(candidate.Match[0], entry.Match[0]) {
					stored = candidate
					break
				}
			}
			require.NotNil(t, stored, "entry was not returned with the same match value")
			require.Equal(t, entry.Priority, stored.Priority)
			require.True(t, proto.Equal(entry.Action, stored.Action))
			key := &p4v1.TableEntry{TableId: entry.TableId, Match: entry.Match, Priority: entry.Priority}
			require.NoError(t, c.WriteTableEntry(ctx, client.UpdateDelete, key))
			entries, err = c.ReadTableEntries(ctx, entry.TableId)
			require.NoError(t, err)
			for _, candidate := range entries {
				if len(candidate.Match) == 1 {
					require.False(t, proto.Equal(candidate.Match[0], entry.Match[0]), "entry remains after deletion")
				}
			}
		})
	}
}
