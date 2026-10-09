//go:build integration && linux

package integration

import (
	"bufio"
	"context"
	"encoding/hex"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBMv2_L2Examples(t *testing.T) {
	l2, packets, counters := os.Getenv("P4RT_L2_EXAMPLE_BIN"), os.Getenv("P4RT_PACKET_EXAMPLE_BIN"), os.Getenv("P4RT_COUNTER_EXAMPLE_BIN")
	if l2 == "" && packets == "" && counters == "" {
		t.Skip("example binary paths unset; skipping live example tests")
	}
	require.NotEmpty(t, l2)
	require.NotEmpty(t, packets)
	require.NotEmpty(t, counters)
	one, two := openEthernetPorts(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, l2, "--addr", targetAddr(), "--p4info", p4infoPath(), "--config", deviceConfigPath()).CombinedOutput()
	require.NoError(t, err, "L2 example: %s", output)
	require.Contains(t, string(output), "wrote 1 entry")
	frame := l2Frame(3)
	two.send(t, frame)
	require.True(t, one.receives(t, frame, 3*time.Second), "the entry written by example 02 did not forward")
	output, err = exec.CommandContext(ctx, counters, "--addr", targetAddr(), "--p4info", p4infoPath(), "--index", "1").CombinedOutput()
	require.NoError(t, err, "counter example: %s", output)
	require.Contains(t, string(output), "MyIngress.pkt_counter[1]: packets=1 bytes=60")
	command := exec.CommandContext(ctx, packets, "--addr", targetAddr(), "--p4info", p4infoPath(), "--send-port", "2")
	stderr, err := command.StderrPipe()
	require.NoError(t, err)
	require.NoError(t, command.Start())
	t.Cleanup(func() {
		if command.ProcessState == nil {
			command.Process.Signal(syscall.SIGINT)
			command.Wait()
		}
	})
	lines := make(chan string, 32)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
	}()
	waitLine := func(want string) string {
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		for {
			select {
			case line, ok := <-lines:
				require.True(t, ok, "packet example stopped before %q", want)
				if strings.Contains(line, want) {
					return line
				}
			case <-timer.C:
				t.Fatalf("packet example did not print %q", want)
				return ""
			}
		}
	}
	waitLine("sent demo packet")
	demo := l2Frame(0)
	copy(demo[14:], "hello world")
	require.True(t, two.receives(t, demo, 3*time.Second), "example 03's PacketOut did not arrive on port 2")
	require.False(t, one.receives(t, demo, 200*time.Millisecond), "example 03's PacketOut followed the L2 entry instead of its metadata")
	unknown := l2Frame(4)
	unknown[5] = 0x56
	one.send(t, unknown)
	line := waitLine("packet-in port=[1] payload=60 bytes")
	require.True(t, strings.HasSuffix(line, hex.EncodeToString(unknown[:16])))
	require.NoError(t, command.Process.Signal(syscall.SIGINT))
	require.NoError(t, command.Wait(), "packet example should exit cleanly on SIGINT")
}
