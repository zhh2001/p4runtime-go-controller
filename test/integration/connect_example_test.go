//go:build integration && linux

package integration

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
)

func TestBMv2_ConnectExample(t *testing.T) {
	binary := os.Getenv("P4RT_CONNECT_EXAMPLE_BIN")
	if binary == "" {
		t.Skip("P4RT_CONNECT_EXAMPLE_BIN unset; skipping live connect example")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	want := "no pipeline installed"
	if os.Getenv("P4RT_TEST_FRESH_TARGET") != "1" {
		if deviceConfigPath() == "" {
			t.Skip("P4RT_DEVICE_CONFIG unset; skipping installed pipeline example")
		}
		info, err := os.ReadFile(p4infoPath())
		require.NoError(t, err)
		config, err := os.ReadFile(deviceConfigPath())
		require.NoError(t, err)
		p, err := pipeline.LoadText(info, config)
		require.NoError(t, err)
		c, err := client.Dial(ctx, targetAddr(), client.WithDeviceID(1), client.WithElectionID(client.ElectionID{Low: 1}), client.WithInsecure())
		require.NoError(t, err)
		defer c.Close()
		_, err = c.SetPipeline(ctx, p, client.SetPipelineOptions{})
		require.NoError(t, err)
		require.NoError(t, c.Close())
		want = fmt.Sprintf("pipeline installed: %d tables, %d actions", len(p.Tables()), len(p.Info().GetActions()))
	}
	command := exec.CommandContext(ctx, binary, "--addr", targetAddr(), "--device-id", "1", "--election", "100")
	stdout, err := command.StdoutPipe()
	require.NoError(t, err)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	require.NoError(t, command.Start())
	defer func() {
		if command.ProcessState == nil {
			command.Process.Signal(syscall.SIGINT)
			command.Wait()
		}
	}()
	lines := make(chan string, 8)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
	}()
	for _, expected := range []string{"connected: device_id=1", want} {
		select {
		case line, ok := <-lines:
			if !ok {
				err := command.Wait()
				t.Fatalf("connect example stopped before %q: %v: %s", expected, err, stderr.String())
			}
			require.Contains(t, line, expected)
		case <-ctx.Done():
			t.Fatalf("connect example did not print %q: %v", expected, ctx.Err())
		}
	}
	require.NoError(t, command.Process.Signal(syscall.SIGINT))
	require.NoError(t, command.Wait(), "connect example: %s", stderr.String())
	require.Contains(t, stderr.String(), "shutting down")
}
