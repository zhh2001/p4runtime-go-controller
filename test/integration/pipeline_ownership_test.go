//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/codec"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
	"github.com/zhh2001/p4runtime-go-controller/tableentry"
)

func TestBMv2_PipelineOwnership(t *testing.T) {
	if deviceConfigPath() == "" {
		t.Skip("P4RT_DEVICE_CONFIG unset")
	}
	text, err := os.ReadFile(p4infoPath())
	require.NoError(t, err)
	info := &p4configv1.P4Info{}
	require.NoError(t, prototext.Unmarshal(text, info))
	config, err := os.ReadFile(deviceConfigPath())
	require.NoError(t, err)
	wantInfo, wantConfig := proto.Clone(info), append([]byte(nil), config...)
	p, err := pipeline.New(info, config)
	require.NoError(t, err)
	builder := tableentry.NewBuilder(p, "MyIngress.t_l2")
	table, ok := p.Table("MyIngress.t_l2")
	require.True(t, ok)
	tableID := table.ID
	table.ID = 0
	table.MatchFields[0].Bitwidth = 1
	table.ActionRefs[0].ID = 0
	proto.Reset(table.Raw())
	p.Tables()[0].ID = 0
	action, ok := p.Action("MyIngress.forward")
	require.True(t, ok)
	action.Params[0].Bitwidth = 1
	proto.Reset(action.Raw())
	counter, ok := p.Counter("MyIngress.pkt_counter")
	require.True(t, ok)
	counter.Size = 1
	proto.Reset(counter.Raw())
	metadata, ok := p.PacketMetadata("packet_out")
	require.True(t, ok)
	metadata.Metadata[0].Bitwidth = 1
	proto.Reset(metadata.Raw())
	proto.Reset(info)
	config[0] ^= 0xff
	proto.Reset(p.Info())
	p.DeviceConfig()[0] ^= 0xff
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, targetAddr(), client.WithDeviceID(1), client.WithElectionID(client.ElectionID{Low: 1}), client.WithInsecure())
	require.NoError(t, err)
	defer c.Close()
	_, err = c.SetPipeline(ctx, p, client.SetPipelineOptions{})
	require.NoError(t, err)
	active, err := c.GetPipeline(ctx)
	require.NoError(t, err)
	require.True(t, proto.Equal(wantInfo, active.Info()))
	require.Equal(t, wantConfig, active.DeviceConfig())
	entry, err := builder.Match("hdr.eth.dst", tableentry.Exact(codec.MustMAC("00:11:22:33:44:55"))).Action("MyIngress.forward", tableentry.Param("port", []byte{2})).Build()
	require.NoError(t, err)
	require.Equal(t, tableID, entry.TableId)
	require.NoError(t, c.WriteTableEntry(ctx, client.UpdateInsert, entry))
	entries, err := c.ReadTableEntries(ctx, tableID)
	require.NoError(t, err)
	var found bool
	for _, stored := range entries {
		if len(stored.Match) == 1 && proto.Equal(entry.Match[0], stored.Match[0]) {
			found = true
			require.True(t, proto.Equal(entry.Action, stored.Action))
		}
	}
	require.True(t, found)
	require.NoError(t, c.WriteTableEntry(ctx, client.UpdateDelete, &p4v1.TableEntry{TableId: tableID, Match: entry.Match}))
}
