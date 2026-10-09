//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.com/zhh2001/p4runtime-go-controller/v2/client"
	"github.com/zhh2001/p4runtime-go-controller/v2/counter"
	"github.com/zhh2001/p4runtime-go-controller/v2/pipeline"
)

var counterUnits = []struct {
	name string
	unit p4configv1.CounterSpec_Unit
}{
	{"packet_stats", p4configv1.CounterSpec_PACKETS},
	{"byte_stats", p4configv1.CounterSpec_BYTES},
	{"stats", p4configv1.CounterSpec_BOTH},
}

func counterValuePipeline(t *testing.T) *pipeline.Pipeline {
	t.Helper()
	infoPath, configPath := os.Getenv("P4RT_COUNTER_P4INFO"), os.Getenv("P4RT_COUNTER_DEVICE_CONFIG")
	if infoPath == "" && configPath == "" {
		t.Skip("P4RT_COUNTER_P4INFO and P4RT_COUNTER_DEVICE_CONFIG unset")
	}
	require.NotEmpty(t, infoPath)
	require.NotEmpty(t, configPath)
	info, err := os.ReadFile(infoPath)
	require.NoError(t, err)
	config, err := os.ReadFile(configPath)
	require.NoError(t, err)
	p, err := pipeline.LoadText(info, config)
	require.NoError(t, err)
	for _, tc := range counterUnits {
		definition, ok := p.Counter(tc.name)
		require.True(t, ok)
		require.Equal(t, tc.unit, definition.Unit)
		require.EqualValues(t, 4, definition.Size)
	}
	return p
}

func counterValueReader(t *testing.T, ctx context.Context, p *pipeline.Pipeline, options ...client.Option) (*counter.Reader, *client.Client) {
	t.Helper()
	options = append([]client.Option{client.WithDeviceID(1), client.WithElectionID(client.ElectionID{Low: 1}), client.WithInsecure()}, options...)
	c, err := client.Dial(ctx, targetAddr(), options...)
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })
	_, err = c.SetPipeline(ctx, p, client.SetPipelineOptions{})
	require.NoError(t, err)
	r, err := counter.NewReader(c, p)
	require.NoError(t, err)
	return r, c
}

func TestBMv2_CounterValues(t *testing.T) {
	p := counterValuePipeline(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	requests := make(chan *p4v1.WriteRequest, 32)
	interceptor := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if method == "/p4.v1.P4Runtime/Write" {
			requests <- proto.Clone(req.(*p4v1.WriteRequest)).(*p4v1.WriteRequest)
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
	r, _ := counterValueReader(t, ctx, p, client.WithDialOptions(grpc.WithUnaryInterceptor(interceptor)))
	for _, tc := range counterUnits {
		t.Run(tc.unit.String(), func(t *testing.T) {
			definition, _ := p.Counter(tc.name)
			for _, index := range []int64{-2, -1, 4} {
				require.Error(t, r.Write(ctx, tc.name, index, 0, 0))
			}
			require.Empty(t, requests, "invalid index attempted an RPC")
			for _, values := range [][2]int64{
				{0, 0}, {1, 0}, {0, 60}, {12, 720},
				{int64(math.MaxUint32) + 1, int64(math.MaxUint32) + 2},
				{math.MaxInt64, math.MaxInt64 - 1}, {-1, -2},
				{math.MinInt64, math.MinInt64 + 1}, {0, 0},
			} {
				// Index 3 is never counted by this fixture's data plane.
				require.NoError(t, r.Write(ctx, tc.name, 3, values[0], values[1]))
				require.Len(t, requests, 1)
				req := <-requests
				require.Len(t, req.Updates, 1)
				require.Equal(t, p4v1.Update_MODIFY, req.Updates[0].Type)
				entry := req.Updates[0].Entity.GetCounterEntry()
				require.Equal(t, definition.ID, entry.GetCounterId())
				require.NotNil(t, entry.GetIndex())
				require.EqualValues(t, 3, entry.Index.Index)
				require.NotNil(t, entry.Data)
				require.Equal(t, values[0], entry.Data.PacketCount)
				require.Equal(t, values[1], entry.Data.ByteCount)
				samples, err := r.Read(ctx, tc.name, 3)
				require.NoError(t, err)
				require.Len(t, samples, 1)
				want := &counter.Data{Name: definition.Name, ID: definition.ID, Index: 3}
				if tc.unit != p4configv1.CounterSpec_BYTES {
					want.Packets = values[0]
				}
				if tc.unit != p4configv1.CounterSpec_PACKETS {
					want.Bytes = values[1]
				}
				require.Equal(t, want, samples[0])
			}
			samples, err := r.Read(ctx, tc.name, -1)
			require.NoError(t, err)
			require.Len(t, samples, 4)
			indexes := make([]int64, len(samples))
			for i, sample := range samples {
				require.Equal(t, definition.Name, sample.Name)
				require.Equal(t, definition.ID, sample.ID)
				indexes[i] = sample.Index
			}
			require.ElementsMatch(t, []int64{0, 1, 2, 3}, indexes)
		})
	}
}

func TestBMv2_CounterCLI(t *testing.T) {
	bin := os.Getenv("P4RT_CLI_BIN")
	if bin == "" {
		t.Skip("P4RT_CLI_BIN unset")
	}
	p := counterValuePipeline(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	settings := filepath.Join(t.TempDir(), "cli.yaml")
	require.NoError(t, os.WriteFile(settings, []byte("{}\n"), 0o600))
	definition, _ := p.Counter("stats")
	for _, values := range [][2]int64{{0, 0}, {math.MaxInt64, math.MaxInt64 - 1}, {-1, -2}, {math.MinInt64, math.MinInt64 + 1}} {
		r, c := counterValueReader(t, ctx, p)
		require.NoError(t, r.Write(ctx, "stats", 3, values[0], values[1]))
		// Each CLI process needs the primary session for its own connection.
		require.NoError(t, c.Close())
		for _, format := range []string{"table", "json", "yaml"} {
			command := exec.CommandContext(ctx, bin, "counter", "read", "--addr", targetAddr(), "--device-id", "1", "--election-id", "1", "--role", "", "--insecure=true", "--config-file", settings, "--output", format, "--p4info", os.Getenv("P4RT_COUNTER_P4INFO"), "--counter", "stats", "--index", "3")
			for _, env := range os.Environ() {
				if !strings.HasPrefix(env, "P4CTL_") {
					command.Env = append(command.Env, env)
				}
			}
			var stderr bytes.Buffer
			command.Stderr = &stderr
			output, err := command.Output()
			require.NoError(t, err, "%s: %s", format, stderr.String())
			if format == "table" {
				require.Equal(t, fmt.Sprintf("%s[3]: packets=%d bytes=%d\n", definition.Name, values[0], values[1]), string(output))
				continue
			}
			if format == "yaml" {
				var document any
				require.NoError(t, yaml.Unmarshal(output, &document))
				output, err = json.Marshal(document)
				require.NoError(t, err)
			}
			var rows []map[string]any
			require.NoError(t, json.Unmarshal(output, &rows))
			require.Equal(t, []map[string]any{{"name": definition.Name, "id": float64(definition.ID), "index": "3", "packets": fmt.Sprint(values[0]), "bytes": fmt.Sprint(values[1])}}, rows)
		}
	}
}
