//go:build integration && linux

package integration

import (
	"context"
	"encoding/hex"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/zhh2001/p4runtime-go-controller/v2/client"
	"github.com/zhh2001/p4runtime-go-controller/v2/pipeline"
)

func TestBMv2_PacketCLI(t *testing.T) {
	binary := os.Getenv("P4RT_CLI_BIN")
	if binary == "" {
		t.Skip("P4RT_CLI_BIN unset; skipping live PacketOut CLI")
	}
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
	require.NoError(t, c.Close())
	for port, iface := range map[int]ethernetPort{1: one, 2: two} {
		for i := range 10 {
			frame := l2Frame(byte(30 + port*10 + i))
			output, err := exec.CommandContext(ctx, binary, "packet", "send", "--addr", targetAddr(), "--device-id", "1", "--election-id", "1", "--role", "", "--insecure=true", "--p4info", p4infoPath(), "--port", strconv.Itoa(port), "--hex", hex.EncodeToString(frame)).CombinedOutput()
			require.NoError(t, err, "PacketOut CLI port %d, attempt %d: %s", port, i+1, output)
			require.True(t, iface.receives(t, frame, 3*time.Second), "CLI exited without sending its Ethernet frame, port %d, attempt %d", port, i+1)
		}
	}
	frame := l2Frame(70)
	output, err := exec.CommandContext(ctx, binary, "packet", "send", "--addr", targetAddr(), "--device-id", "1", "--election-id", "1", "--role", "", "--insecure=true", "--p4info", p4infoPath(), "--port", "512", "--hex", hex.EncodeToString(frame)).CombinedOutput()
	require.Error(t, err)
	require.Contains(t, string(output), "--port")
	require.NotContains(t, string(output), "panic:")
	require.False(t, one.receives(t, frame, 100*time.Millisecond))
	require.False(t, two.receives(t, frame, 100*time.Millisecond))
}
