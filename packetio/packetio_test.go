package packetio_test

import (
	"context"
	"sync"
	"testing"
	"time"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/internal/testutil"
	"github.com/zhh2001/p4runtime-go-controller/packetio"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
)

func fixturePipeline(t *testing.T) *pipeline.Pipeline {
	t.Helper()
	info := &p4configv1.P4Info{
		ControllerPacketMetadata: []*p4configv1.ControllerPacketMetadata{
			{
				Preamble: &p4configv1.Preamble{Id: 1, Name: "packet_in"},
				Metadata: []*p4configv1.ControllerPacketMetadata_Metadata{
					{Id: 1, Name: "ingress_port", Bitwidth: 9},
				},
			},
			{
				Preamble: &p4configv1.Preamble{Id: 2, Name: "packet_out"},
				Metadata: []*p4configv1.ControllerPacketMetadata_Metadata{
					{Id: 1, Name: "egress_port", Bitwidth: 9},
				},
			},
		},
	}
	p, err := pipeline.New(info, nil)
	require.NoError(t, err)
	return p
}

func TestPacketIn_Decode(t *testing.T) {
	h := testutil.StartServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, "passthrough:bufnet",
		client.WithDeviceID(1),
		client.WithElectionID(client.ElectionID{Low: 1}),
		client.WithInsecure(),
		client.WithArbitrationTimeout(1500*time.Millisecond),
		client.WithDialOptions(grpc.WithContextDialer(h.Dialer())),
	)
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.BecomePrimary(ctx))

	sub, err := packetio.NewSubscriber(c, fixturePipeline(t))
	require.NoError(t, err)

	var (
		mu  sync.Mutex
		got *packetio.PacketIn
	)
	done := make(chan struct{})
	sub.OnPacket(func(_ context.Context, pkt *packetio.PacketIn) {
		mu.Lock()
		defer mu.Unlock()
		if got == nil {
			got = pkt
			close(done)
		}
	})

	require.Eventually(t, func() bool {
		return h.PushStreamMessage(&p4v1.StreamMessageResponse{
			Update: &p4v1.StreamMessageResponse_Packet{
				Packet: &p4v1.PacketIn{
					Payload: []byte{0xde, 0xad, 0xbe, 0xef},
					Metadata: []*p4v1.PacketMetadata{
						{MetadataId: 1, Value: []byte{0x01}},
					},
				},
			},
		}) == nil
	}, time.Second, 20*time.Millisecond)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for PacketIn")
	}

	require.NotNil(t, got)
	assert.Equal(t, []byte{0xde, 0xad, 0xbe, 0xef}, got.Payload)
	assert.Equal(t, []byte{0x01}, got.Metadata["ingress_port"])
}

func TestPacketOut_Encode(t *testing.T) {
	h := testutil.StartServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, "passthrough:bufnet",
		client.WithDeviceID(1),
		client.WithElectionID(client.ElectionID{Low: 1}),
		client.WithInsecure(),
		client.WithArbitrationTimeout(1500*time.Millisecond),
		client.WithDialOptions(grpc.WithContextDialer(h.Dialer())),
	)
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.BecomePrimary(ctx))

	sub, err := packetio.NewSubscriber(c, fixturePipeline(t))
	require.NoError(t, err)

	err = sub.Send(ctx, &packetio.PacketOut{
		Payload:  []byte{0x01, 0x02},
		Metadata: map[string][]byte{"egress_port": {0x01}},
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		h.Mu.Lock()
		defer h.Mu.Unlock()
		return len(h.ReceivedPacketOuts) == 1
	}, time.Second, 10*time.Millisecond)

	h.Mu.Lock()
	pkt := h.ReceivedPacketOuts[0]
	h.Mu.Unlock()
	assert.Equal(t, []byte{0x01, 0x02}, pkt.Payload)
	require.Len(t, pkt.Metadata, 1)
	assert.EqualValues(t, 1, pkt.Metadata[0].MetadataId)
	assert.Equal(t, []byte{0x01}, pkt.Metadata[0].Value)
}

func TestPacketOut_UnknownMetadata(t *testing.T) {
	h := testutil.StartServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, "passthrough:bufnet",
		client.WithDeviceID(1),
		client.WithElectionID(client.ElectionID{Low: 1}),
		client.WithInsecure(),
		client.WithArbitrationTimeout(1500*time.Millisecond),
		client.WithDialOptions(grpc.WithContextDialer(h.Dialer())),
	)
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.BecomePrimary(ctx))

	sub, err := packetio.NewSubscriber(c, fixturePipeline(t))
	require.NoError(t, err)

	err = sub.Send(ctx, &packetio.PacketOut{
		Metadata: map[string][]byte{"nope": {0x01}},
	})
	assert.Error(t, err)
}
