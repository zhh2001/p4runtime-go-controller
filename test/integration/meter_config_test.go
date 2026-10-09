//go:build integration

package integration

import (
	"context"
	"math"
	"os"
	"testing"
	"time"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.com/zhh2001/p4runtime-go-controller/v2/client"
	errs "github.com/zhh2001/p4runtime-go-controller/v2/errors"
	"github.com/zhh2001/p4runtime-go-controller/v2/meter"
	"github.com/zhh2001/p4runtime-go-controller/v2/pipeline"
)

func meterPipeline(t *testing.T) *pipeline.Pipeline {
	t.Helper()
	infoPath, configPath := os.Getenv("P4RT_METER_P4INFO"), os.Getenv("P4RT_METER_DEVICE_CONFIG")
	if infoPath == "" && configPath == "" {
		t.Skip("P4RT_METER_P4INFO and P4RT_METER_DEVICE_CONFIG unset")
	}
	require.NotEmpty(t, infoPath)
	require.NotEmpty(t, configPath)
	info, err := os.ReadFile(infoPath)
	require.NoError(t, err)
	config, err := os.ReadFile(configPath)
	require.NoError(t, err)
	p, err := pipeline.LoadText(info, config)
	require.NoError(t, err)
	definition, ok := p.Meter("rate")
	require.True(t, ok)
	require.Equal(t, p4configv1.MeterSpec_PACKETS, definition.Unit)
	require.Equal(t, p4configv1.MeterSpec_TWO_RATE_THREE_COLOR, definition.Raw().GetSpec().GetType())
	return p
}

func TestBMv2_MeterConfigs(t *testing.T) {
	p := meterPipeline(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	requests := make(chan *p4v1.WriteRequest, 32)
	interceptor := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if method == "/p4.v1.P4Runtime/Write" {
			requests <- proto.Clone(req.(*p4v1.WriteRequest)).(*p4v1.WriteRequest)
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
	c, err := client.Dial(ctx, targetAddr(), client.WithDeviceID(1), client.WithElectionID(client.ElectionID{Low: 1}), client.WithInsecure(), client.WithDialOptions(grpc.WithUnaryInterceptor(interceptor)))
	require.NoError(t, err)
	defer c.Close()
	_, err = c.SetPipeline(ctx, p, client.SetPipelineOptions{})
	require.NoError(t, err)
	r, err := meter.NewReader(c, p)
	require.NoError(t, err)
	for _, cfg := range []meter.Config{{CIR: -1}, {CBurst: -1}, {PIR: -1}, {PBurst: -1}, {EBurst: -1}, {CIR: 20, PIR: 10}, {EBurst: 1}} {
		require.Error(t, r.Write(ctx, "rate", 0, cfg))
	}
	for _, index := range []int64{-2, -1, 4} {
		require.Error(t, r.Reset(ctx, "rate", index))
	}
	require.Empty(t, requests, "invalid meter request attempted an RPC")
	definition, _ := p.Meter("rate")
	request := func(index int64, config *p4v1.MeterConfig) {
		require.Len(t, requests, 1)
		req := <-requests
		require.Len(t, req.Updates, 1)
		require.Equal(t, p4v1.Update_MODIFY, req.Updates[0].Type)
		entry := req.Updates[0].Entity.GetMeterEntry()
		require.Equal(t, definition.ID, entry.GetMeterId())
		require.NotNil(t, entry.GetIndex())
		require.Equal(t, index, entry.Index.Index)
		require.True(t, proto.Equal(config, entry.Config))
		require.Nil(t, entry.CounterData)
	}
	for _, index := range []int64{0, 3} {
		for _, cfg := range []meter.Config{{}, {CIR: 10, CBurst: 20, PIR: 30, PBurst: 40}, {CBurst: 2, PBurst: 1}} {
			require.NoError(t, r.Write(ctx, "rate", index, cfg))
			want := &p4v1.MeterConfig{Cir: cfg.CIR, Cburst: cfg.CBurst, Pir: cfg.PIR, Pburst: cfg.PBurst}
			request(index, want)
			entries, err := r.Read(ctx, "rate", index)
			require.NoError(t, err)
			require.Len(t, entries, 1)
			require.NotNil(t, entries[0].Config)
			require.True(t, proto.Equal(want, entries[0].Config), "meter readback: %s", entries[0])
		}
		require.NoError(t, r.Reset(ctx, "rate", index))
		request(index, nil)
		entries, err := r.Read(ctx, "rate", index)
		require.NoError(t, err)
		require.Len(t, entries, 1)
		require.Nil(t, entries[0].Config, "reset meter must have no config")
	}
	wide := meter.Config{CBurst: int64(math.MaxUint32) + 1, PBurst: int64(math.MaxUint32) + 1}
	err = r.Write(ctx, "rate", 0, wide)
	request(0, &p4v1.MeterConfig{Cburst: wide.CBurst, Pburst: wide.PBurst})
	if err != nil {
		require.ErrorIs(t, err, errs.ErrTargetUnsupported)
		t.Log("target rejects bursts above uint32; SDK preserves the full int64 value for target validation")
	} else {
		entries, err := r.Read(ctx, "rate", 0)
		require.NoError(t, err)
		require.Len(t, entries, 1)
		require.Equal(t, wide.CBurst, entries[0].GetConfig().GetCburst())
		require.Equal(t, wide.PBurst, entries[0].GetConfig().GetPburst())
	}
}
