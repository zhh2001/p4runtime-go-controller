//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/zhh2001/p4runtime-go-controller/v2/client"
	"github.com/zhh2001/p4runtime-go-controller/v2/packetio"
	"github.com/zhh2001/p4runtime-go-controller/v2/pipeline"
)

func TestBMv2_PacketCPUPort(t *testing.T) {
	if deviceConfigPath() == "" {
		t.Skip("P4RT_DEVICE_CONFIG unset; skipping live CPU port test")
	}
	info, err := os.ReadFile(p4infoPath())
	require.NoError(t, err)
	config, err := os.ReadFile(deviceConfigPath())
	require.NoError(t, err)
	p, err := pipeline.LoadText(info, config)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, targetAddr(), client.WithDeviceID(1), client.WithElectionID(client.ElectionID{Low: 1}), client.WithInsecure())
	require.NoError(t, err)
	defer c.Close()
	_, err = c.SetPipeline(ctx, p, client.SetPipelineOptions{})
	require.NoError(t, err)
	sub, err := packetio.NewSubscriber(c, p)
	require.NoError(t, err)
	packets := make(chan *packetio.PacketIn, 1)
	stop := sub.OnPacket(func(_ context.Context, pkt *packetio.PacketIn) {
		select {
		case packets <- pkt:
		default:
		}
	})
	defer stop()
	frame := make([]byte, 60)
	copy(frame, []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x00, 0x66, 0x77, 0x88, 0x99, 0xaa, 0x88, 0xb5})
	copy(frame[14:], "CPU port loopback")
	require.NoError(t, sub.Send(ctx, &packetio.PacketOut{Payload: frame, Metadata: map[string][]byte{"egress_port": {255}, "_pad": {0}}}))
	select {
	case pkt := <-packets:
		require.Equal(t, frame, pkt.Payload)
		require.Equal(t, []byte{255}, pkt.Metadata["ingress_port"])
		require.Equal(t, []byte{0}, pkt.Metadata["_pad"])
	case <-ctx.Done():
		t.Fatal("CPU port 255 did not return PacketOut as PacketIn")
	}
	require.NoError(t, c.CloseGracefully(ctx))
}
