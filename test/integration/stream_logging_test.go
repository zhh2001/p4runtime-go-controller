//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/zhh2001/p4runtime-go-controller/client"
)

type streamLogOutput struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *streamLogOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}

type streamLogRecord struct {
	Message    string `json:"msg"`
	Level      string `json:"level"`
	DeviceID   uint64 `json:"device_id"`
	ElectionID uint64 `json:"election_id_low"`
	Controller string `json:"controller"`
	State      string `json:"state"`
	Stage      string `json:"stage"`
}

func (b *streamLogOutput) records(t *testing.T) []streamLogRecord {
	t.Helper()
	b.mu.Lock()
	data := bytes.Clone(b.buffer.Bytes())
	b.mu.Unlock()
	decoder := json.NewDecoder(bytes.NewReader(data))
	var records []streamLogRecord
	for decoder.More() {
		var record streamLogRecord
		require.NoError(t, decoder.Decode(&record))
		records = append(records, record)
	}
	return records
}

func TestBMv2_StreamLogging(t *testing.T) {
	if deviceConfigPath() == "" {
		t.Skip("P4RT_DEVICE_CONFIG unset")
	}
	var output streamLogOutput
	logger := slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})).With("controller", "reconnect-test")
	firstStream := make(chan context.CancelFunc, 1)
	var opened sync.Once
	interceptor := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		streamCtx, cancel := context.WithCancel(ctx)
		stream, err := streamer(streamCtx, desc, cc, method, opts...)
		if err != nil {
			cancel()
			return nil, err
		}
		opened.Do(func() { firstStream <- cancel })
		return stream, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, targetAddr(), client.WithDeviceID(1),
		client.WithElectionID(client.ElectionID{Low: 1}), client.WithInsecure(),
		client.WithLogger(logger), client.WithLogger(nil), client.WithStreamInterceptor(interceptor))
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.BecomePrimary(ctx))
	select {
	case cancelStream := <-firstStream:
		cancelStream() // Interrupt only the RPC, leaving the SDK session active.
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.Eventually(t, func() bool {
		primary := 0
		for _, record := range output.records(t) {
			if record.Message == "p4runtime: arbitration status" && record.State == "primary" {
				primary++
			}
		}
		return primary >= 2 && c.IsPrimary()
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, c.Close())
	var attempts, primaries, failures, stopped int
	for _, record := range output.records(t) {
		require.Equal(t, uint64(1), record.DeviceID)
		require.Equal(t, uint64(1), record.ElectionID)
		require.Equal(t, "reconnect-test", record.Controller)
		switch record.Message {
		case "p4runtime: opening StreamChannel":
			attempts++
			require.Equal(t, "INFO", record.Level)
		case "p4runtime: arbitration status":
			if record.State == "primary" {
				primaries++
			}
		case "p4runtime: StreamChannel failed":
			failures++
			require.Equal(t, "recv", record.Stage)
			require.Equal(t, "WARN", record.Level)
		case "p4runtime: stream supervisor stopped":
			stopped++
			require.Equal(t, "DEBUG", record.Level)
		}
	}
	require.Equal(t, 2, attempts)
	require.Equal(t, 2, primaries)
	require.Equal(t, 1, failures)
	require.Equal(t, 1, stopped)
}
