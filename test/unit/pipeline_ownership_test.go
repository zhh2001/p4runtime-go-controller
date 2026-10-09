package unit_test

import (
	"context"
	"testing"
	"time"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.com/zhh2001/p4runtime-go-controller/v2/client"
	"github.com/zhh2001/p4runtime-go-controller/v2/counter"
	"github.com/zhh2001/p4runtime-go-controller/v2/digest"
	"github.com/zhh2001/p4runtime-go-controller/v2/internal/testutil"
	"github.com/zhh2001/p4runtime-go-controller/v2/meter"
	"github.com/zhh2001/p4runtime-go-controller/v2/packetio"
	"github.com/zhh2001/p4runtime-go-controller/v2/pipeline"
	"github.com/zhh2001/p4runtime-go-controller/v2/register"
	"github.com/zhh2001/p4runtime-go-controller/v2/tableentry"
)

func TestPipelineOwnership_SDKRequests(t *testing.T) {
	bits := &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_Bitstring{Bitstring: &p4configv1.P4BitstringLikeTypeSpec{TypeSpec: &p4configv1.P4BitstringLikeTypeSpec_Bit{Bit: &p4configv1.P4BitTypeSpec{Bitwidth: 9}}}}}
	info := &p4configv1.P4Info{
		Tables: []*p4configv1.Table{{Preamble: &p4configv1.Preamble{Id: 0x02000001, Name: "ingress.table", Alias: "table"}, Size: 4,
			MatchFields: []*p4configv1.MatchField{{Id: 1, Name: "key", Bitwidth: 9, Match: &p4configv1.MatchField_MatchType_{MatchType: p4configv1.MatchField_EXACT}}},
			ActionRefs:  []*p4configv1.ActionRef{{Id: 0x01000001}},
		}},
		Actions:                  []*p4configv1.Action{{Preamble: &p4configv1.Preamble{Id: 0x01000001, Name: "ingress.forward", Alias: "forward"}, Params: []*p4configv1.Action_Param{{Id: 1, Name: "port", Bitwidth: 9}}}},
		Counters:                 []*p4configv1.Counter{{Preamble: &p4configv1.Preamble{Id: 0x12000001, Name: "stats"}, Size: 4, Spec: &p4configv1.CounterSpec{Unit: p4configv1.CounterSpec_BOTH}}},
		Meters:                   []*p4configv1.Meter{{Preamble: &p4configv1.Preamble{Id: 0x14000001, Name: "rate"}, Size: 4, Spec: &p4configv1.MeterSpec{Unit: p4configv1.MeterSpec_PACKETS}}},
		Registers:                []*p4configv1.Register{{Preamble: &p4configv1.Preamble{Id: 0x16000001, Name: "state"}, Size: 4, TypeSpec: &p4configv1.P4DataTypeSpec{TypeSpec: &p4configv1.P4DataTypeSpec_NewType{NewType: &p4configv1.P4NamedType{Name: "Word"}}}}},
		TypeInfo:                 &p4configv1.P4TypeInfo{NewTypes: map[string]*p4configv1.P4NewTypeSpec{"Word": {Representation: &p4configv1.P4NewTypeSpec_OriginalType{OriginalType: bits}}}},
		Digests:                  []*p4configv1.Digest{{Preamble: &p4configv1.Preamble{Id: 0x17000001, Name: "events"}, TypeSpec: bits}},
		ControllerPacketMetadata: []*p4configv1.ControllerPacketMetadata{{Preamble: &p4configv1.Preamble{Id: 0x04000001, Name: "packet_out"}, Metadata: []*p4configv1.ControllerPacketMetadata_Metadata{{Id: 1, Name: "egress_port", Bitwidth: 9}}}},
	}
	want := proto.Clone(info)
	p, err := pipeline.New(info, []byte{1, 2, 3})
	require.NoError(t, err)
	h := testutil.StartServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, "passthrough:bufnet", client.WithDeviceID(1), client.WithElectionID(client.ElectionID{Low: 1}), client.WithInsecure(), client.WithDialOptions(grpc.WithContextDialer(h.Dialer())))
	require.NoError(t, err)
	defer c.Close()
	builder := tableentry.NewBuilder(p, "table")
	packets, err := packetio.NewSubscriber(c, p)
	require.NoError(t, err)
	// Mutate both constructor inputs and definitions retrieved separately
	// from the objects retained by Builder and Subscriber.
	info.Actions[0].Params[0].Bitwidth = 1
	bits.GetBitstring().GetBit().Bitwidth = 1
	proto.Reset(info)
	proto.Reset(p.Info())
	table, _ := p.Table("table")
	table.MatchFields[0].Bitwidth = 1
	table.ActionRefs[0].ID = 0
	action, _ := p.Action("forward")
	action.Params[0].Bitwidth = 1
	stats, _ := p.Counter("stats")
	stats.ID, stats.Size = 0, 1
	rate, _ := p.Meter("rate")
	rate.Raw().Spec.Type = p4configv1.MeterSpec_SINGLE_RATE_TWO_COLOR
	state, _ := p.Register("state")
	state.Raw().TypeSpec.GetNewType().Name = "missing"
	events, _ := p.Digest("events")
	events.ID = 0
	metadata, _ := p.PacketMetadata("packet_out")
	metadata.Metadata[0].Bitwidth = 1
	p.Info().TypeInfo.NewTypes["Word"].GetOriginalType().GetBitstring().GetBit().Bitwidth = 1
	_, err = c.SetPipeline(ctx, p, client.SetPipelineOptions{})
	require.NoError(t, err)
	entry, err := builder.Match("key", tableentry.Exact([]byte{1, 255})).Action("forward", tableentry.Param("port", []byte{1, 255})).Build()
	require.NoError(t, err)
	require.NoError(t, c.WriteTableEntry(ctx, client.UpdateInsert, entry))
	counts, err := counter.NewReader(c, p)
	require.NoError(t, err)
	require.NoError(t, counts.Write(ctx, "stats", 3, 12, 720))
	rates, err := meter.NewReader(c, p)
	require.NoError(t, err)
	require.NoError(t, rates.Write(ctx, "rate", 3, meter.Config{CIR: 10, PIR: 20, CBurst: 3, PBurst: 4}))
	values, err := register.NewReader(c, p)
	require.NoError(t, err)
	require.NoError(t, values.Write(ctx, "state", 3, []byte{1, 255}))
	subscriber, err := digest.NewSubscriber(c, p)
	require.NoError(t, err)
	off, err := subscriber.Subscribe("events", func(context.Context, *p4v1.DigestList) {})
	require.NoError(t, err)
	defer off()
	require.NoError(t, subscriber.Ack(ctx, &p4v1.DigestList{DigestId: 0x17000001, ListId: 7}))
	require.NoError(t, packets.Send(ctx, &packetio.PacketOut{Payload: []byte{1}, Metadata: map[string][]byte{"egress_port": {1, 255}}}))
	h.Mu.Lock()
	request := h.SetPipelineReq
	writes := append([]*p4v1.WriteRequest(nil), h.WriteRequests...)
	h.Mu.Unlock()
	require.True(t, proto.Equal(want, request.Config.P4Info))
	require.Equal(t, []byte{1, 2, 3}, request.Config.P4DeviceConfig)
	require.Len(t, writes, 4)
	stored := writes[0].Updates[0].Entity.GetTableEntry()
	require.EqualValues(t, 0x02000001, stored.TableId)
	require.Equal(t, []byte{1, 255}, stored.Match[0].GetExact().Value)
	require.EqualValues(t, 0x01000001, stored.Action.GetAction().ActionId)
	require.Equal(t, []byte{1, 255}, stored.Action.GetAction().Params[0].Value)
	require.EqualValues(t, 0x12000001, writes[1].Updates[0].Entity.GetCounterEntry().CounterId)
	require.EqualValues(t, 3, writes[1].Updates[0].Entity.GetCounterEntry().Index.Index)
	config := writes[2].Updates[0].Entity.GetMeterEntry().Config
	require.Equal(t, &p4v1.MeterConfig{Cir: 10, Pir: 20, Cburst: 3, Pburst: 4}, config)
	require.Equal(t, []byte{1, 255}, writes[3].Updates[0].Entity.GetRegisterEntry().Data.GetBitstring())
	require.Eventually(t, func() bool {
		h.Mu.Lock()
		defer h.Mu.Unlock()
		return len(h.ReceivedPacketOuts) == 1 && len(h.ReceivedDigestAcks) == 1
	}, time.Second, 5*time.Millisecond)
	h.Mu.Lock()
	packet, ack := h.ReceivedPacketOuts[0], h.ReceivedDigestAcks[0]
	h.Mu.Unlock()
	require.Equal(t, []byte{1, 255}, packet.Metadata[0].Value)
	require.EqualValues(t, 0x17000001, ack.DigestId)
	require.EqualValues(t, 7, ack.ListId)
}
