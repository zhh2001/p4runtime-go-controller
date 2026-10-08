//go:build integration && linux

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/packetio"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
)

func TestBMv2_PacketOutMetadata(t *testing.T) {
	one, two := openEthernetPorts(t)
	info, err := os.ReadFile(p4infoPath())
	require.NoError(t, err)
	config, err := os.ReadFile(deviceConfigPath())
	require.NoError(t, err)
	p, err := pipeline.LoadText(info, config)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, targetAddr(), client.WithDeviceID(1), client.WithElectionID(client.ElectionID{Low: 1}), client.WithInsecure())
	require.NoError(t, err)
	defer c.Close()
	_, err = c.SetPipeline(ctx, p, client.SetPipelineOptions{})
	require.NoError(t, err)
	sub, err := packetio.NewSubscriber(c, p)
	require.NoError(t, err)
	for i, tc := range []struct {
		name     string
		metadata map[string][]byte
		want     string
	}{
		{name: "missing port", metadata: map[string][]byte{"_pad": {0}}, want: "egress_port"},
		{name: "missing padding", metadata: map[string][]byte{"egress_port": {1}}, want: "_pad"},
		{name: "unknown field", metadata: map[string][]byte{"egress_port": {1}, "_pad": {0}, "unknown": {0}}, want: "unknown"},
		{name: "port overflow", metadata: map[string][]byte{"egress_port": {2, 0}, "_pad": {0}}, want: "egress_port"},
		{name: "padding overflow", metadata: map[string][]byte{"egress_port": {1}, "_pad": {128}}, want: "_pad"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frame := l2Frame(byte(80 + i))
			require.ErrorContains(t, sub.Send(ctx, &packetio.PacketOut{Payload: frame, Metadata: tc.metadata}), tc.want)
			require.False(t, one.receives(t, frame, 100*time.Millisecond))
			require.False(t, two.receives(t, frame, 100*time.Millisecond))
		})
	}
	for port, iface := range map[byte]ethernetPort{1: one, 2: two} {
		frame := l2Frame(90 + port)
		metadata := map[string][]byte{"egress_port": {0, 0, port}, "_pad": nil}
		if port == 2 {
			metadata["_pad"] = []byte{0, 0}
		}
		require.NoError(t, sub.Send(ctx, &packetio.PacketOut{Payload: frame, Metadata: metadata}))
		require.True(t, iface.receives(t, frame, 3*time.Second), "encoded PacketOut did not reach port %d", port)
	}
	require.NoError(t, c.CloseGracefully(ctx))
}
