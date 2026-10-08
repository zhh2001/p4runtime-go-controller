package packetio_test

import (
	"context"
	"testing"
	"time"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.com/zhh2001/p4runtime-go-controller/client"
	errs "github.com/zhh2001/p4runtime-go-controller/errors"
	"github.com/zhh2001/p4runtime-go-controller/internal/testutil"
	"github.com/zhh2001/p4runtime-go-controller/packetio"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
)

func metadataClient(t *testing.T) (context.Context, *client.Client, *testutil.ServerHarness) {
	t.Helper()
	h := testutil.StartServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	c, err := client.Dial(ctx, "passthrough:bufnet", client.WithDeviceID(1),
		client.WithElectionID(client.ElectionID{Low: 1}), client.WithInsecure(),
		client.WithDialOptions(grpc.WithContextDialer(h.Dialer())))
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, c.BecomePrimary(ctx))
	return ctx, c, h
}

func metadataPipeline(t *testing.T, fields ...*p4configv1.ControllerPacketMetadata_Metadata) *pipeline.Pipeline {
	t.Helper()
	p, err := pipeline.New(&p4configv1.P4Info{ControllerPacketMetadata: []*p4configv1.ControllerPacketMetadata{{
		Preamble: &p4configv1.Preamble{Id: 2, Name: "packet_out"}, Metadata: fields,
	}}}, nil)
	require.NoError(t, err)
	return p
}

func TestPacketOutMetadataValidation(t *testing.T) {
	port := &p4configv1.ControllerPacketMetadata_Metadata{Id: 1, Name: "egress_port", Bitwidth: 9}
	padding := &p4configv1.ControllerPacketMetadata_Metadata{Id: 2, Name: "_pad", Bitwidth: 7}
	for _, tc := range []struct {
		name   string
		fields []*p4configv1.ControllerPacketMetadata_Metadata
		out    *packetio.PacketOut
		want   string
		width  bool
	}{
		{name: "nil packet", fields: []*p4configv1.ControllerPacketMetadata_Metadata{port}, want: "nil packet"},
		{name: "nil metadata", fields: []*p4configv1.ControllerPacketMetadata_Metadata{port}, out: &packetio.PacketOut{}, want: "egress_port"},
		{name: "empty metadata", fields: []*p4configv1.ControllerPacketMetadata_Metadata{port}, out: &packetio.PacketOut{Metadata: map[string][]byte{}}, want: "egress_port"},
		{name: "missing port", fields: []*p4configv1.ControllerPacketMetadata_Metadata{port, padding}, out: &packetio.PacketOut{Metadata: map[string][]byte{"_pad": {0}}}, want: "egress_port"},
		{name: "missing padding", fields: []*p4configv1.ControllerPacketMetadata_Metadata{port, padding}, out: &packetio.PacketOut{Metadata: map[string][]byte{"egress_port": {1}}}, want: "_pad"},
		{name: "unknown field", fields: []*p4configv1.ControllerPacketMetadata_Metadata{port}, out: &packetio.PacketOut{Metadata: map[string][]byte{"egress_port": {1}, "unknown": {1}}}, want: "unknown"},
		{name: "port overflow", fields: []*p4configv1.ControllerPacketMetadata_Metadata{port}, out: &packetio.PacketOut{Metadata: map[string][]byte{"egress_port": {2, 0}}}, want: "egress_port", width: true},
		{name: "padding overflow", fields: []*p4configv1.ControllerPacketMetadata_Metadata{port, padding}, out: &packetio.PacketOut{Metadata: map[string][]byte{"egress_port": {1}, "_pad": {128}}}, want: "_pad", width: true},
		{name: "invalid width", fields: []*p4configv1.ControllerPacketMetadata_Metadata{{Id: 1, Name: "egress_port"}}, out: &packetio.PacketOut{Metadata: map[string][]byte{"egress_port": {1}}}, want: "bitwidth"},
		{name: "empty definition", out: &packetio.PacketOut{Metadata: map[string][]byte{"egress_port": {1}}}, want: "egress_port"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, c, h := metadataClient(t)
			sub, err := packetio.NewSubscriber(c, metadataPipeline(t, tc.fields...))
			require.NoError(t, err)
			err = sub.Send(ctx, tc.out)
			require.ErrorContains(t, err, tc.want)
			if tc.width {
				require.ErrorIs(t, err, errs.ErrInvalidBitWidth)
			}
			require.NoError(t, c.CloseGracefully(ctx))
			h.Mu.Lock()
			defer h.Mu.Unlock()
			require.Empty(t, h.ReceivedPacketOuts, "invalid metadata reached the stream")
		})
	}
}

func TestPacketOutWithoutDefinition(t *testing.T) {
	for _, tc := range []struct {
		name     string
		metadata map[string][]byte
	}{
		{name: "nil metadata"},
		{name: "empty metadata", metadata: map[string][]byte{}},
		{name: "supplied metadata", metadata: map[string][]byte{"egress_port": {1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, c, h := metadataClient(t)
			p, err := pipeline.New(&p4configv1.P4Info{}, nil)
			require.NoError(t, err)
			sub, err := packetio.NewSubscriber(c, p)
			require.NoError(t, err)
			out := &packetio.PacketOut{Payload: []byte{1, 2}, Metadata: tc.metadata}
			if len(tc.metadata) != 0 {
				require.Error(t, sub.Send(ctx, out))
			} else {
				require.NoError(t, sub.Send(ctx, out))
			}
			require.NoError(t, c.CloseGracefully(ctx))
			h.Mu.Lock()
			defer h.Mu.Unlock()
			if len(tc.metadata) != 0 {
				require.Empty(t, h.ReceivedPacketOuts)
			} else {
				require.Len(t, h.ReceivedPacketOuts, 1)
				require.True(t, proto.Equal(&p4v1.PacketOut{Payload: out.Payload}, h.ReceivedPacketOuts[0]))
			}
		})
	}
}

func TestPacketOutMetadataDefinitionOrder(t *testing.T) {
	ctx, c, h := metadataClient(t)
	p := metadataPipeline(t,
		&p4configv1.ControllerPacketMetadata_Metadata{Id: 9, Name: "wide", Bitwidth: 65},
		&p4configv1.ControllerPacketMetadata_Metadata{Id: 3, Name: "_pad", Bitwidth: 7},
		&p4configv1.ControllerPacketMetadata_Metadata{Id: 17, Name: "egress_port", Bitwidth: 9},
	)
	sub, err := packetio.NewSubscriber(c, p)
	require.NoError(t, err)
	out := &packetio.PacketOut{Payload: []byte{1, 2}, Metadata: map[string][]byte{
		"egress_port": {0, 1, 255}, "_pad": nil, "wide": {0, 1, 0, 0, 0, 0, 0, 0, 0, 0},
	}}
	for range 20 {
		require.NoError(t, sub.Send(ctx, out))
	}
	require.NoError(t, c.CloseGracefully(ctx))
	h.Mu.Lock()
	defer h.Mu.Unlock()
	require.Len(t, h.ReceivedPacketOuts, 20)
	want := &p4v1.PacketOut{Payload: out.Payload, Metadata: []*p4v1.PacketMetadata{
		{MetadataId: 9, Value: []byte{1, 0, 0, 0, 0, 0, 0, 0, 0}},
		{MetadataId: 3, Value: []byte{0}},
		{MetadataId: 17, Value: []byte{1, 255}},
	}}
	for _, actual := range h.ReceivedPacketOuts {
		require.True(t, proto.Equal(want, actual), "unexpected metadata: %v", actual.Metadata)
	}
	require.Equal(t, []byte{0, 1, 255}, out.Metadata["egress_port"])
	require.Nil(t, out.Metadata["_pad"])
}
