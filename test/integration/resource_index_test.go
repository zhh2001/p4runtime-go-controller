//go:build integration

package integration

import (
	"context"
	"math"
	"os"
	"sync/atomic"
	"testing"
	"time"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/zhh2001/p4runtime-go-controller/v2/client"
	"github.com/zhh2001/p4runtime-go-controller/v2/counter"
	errs "github.com/zhh2001/p4runtime-go-controller/v2/errors"
	"github.com/zhh2001/p4runtime-go-controller/v2/meter"
	"github.com/zhh2001/p4runtime-go-controller/v2/pipeline"
	"github.com/zhh2001/p4runtime-go-controller/v2/register"
)

func TestBMv2_ResourceIndexes(t *testing.T) {
	infoPath, configPath := os.Getenv("P4RT_RESOURCE_P4INFO"), os.Getenv("P4RT_RESOURCE_DEVICE_CONFIG")
	if infoPath == "" && configPath == "" {
		t.Skip("P4RT_RESOURCE_P4INFO and P4RT_RESOURCE_DEVICE_CONFIG unset")
	}
	require.NotEmpty(t, infoPath)
	require.NotEmpty(t, configPath)
	info, err := os.ReadFile(infoPath)
	require.NoError(t, err)
	config, err := os.ReadFile(configPath)
	require.NoError(t, err)
	p, err := pipeline.LoadText(info, config)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	requests := make(chan *p4v1.ReadRequest, 32)
	var writes atomic.Int64
	readInterceptor := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		stream, err := streamer(ctx, desc, cc, method, opts...)
		if err != nil {
			return nil, err
		}
		if method == "/p4.v1.P4Runtime/Read" {
			return &readRoleStream{ClientStream: stream, requests: requests}, nil
		}
		return stream, nil
	}
	writeInterceptor := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if method == "/p4.v1.P4Runtime/Write" {
			writes.Add(1)
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
	c, err := client.Dial(ctx, targetAddr(), client.WithDeviceID(1), client.WithElectionID(client.ElectionID{Low: 1}), client.WithInsecure(),
		client.WithDialOptions(grpc.WithStreamInterceptor(readInterceptor), grpc.WithUnaryInterceptor(writeInterceptor)))
	require.NoError(t, err)
	defer c.Close()
	_, err = c.SetPipeline(ctx, p, client.SetPipelineOptions{})
	require.NoError(t, err)
	cr, err := counter.NewReader(c, p)
	require.NoError(t, err)
	mr, err := meter.NewReader(c, p)
	require.NoError(t, err)
	rr, err := register.NewReader(c, p)
	require.NoError(t, err)
	cfg := meter.Config{CIR: 10, CBurst: 20, PIR: 30, PBurst: 40}
	for _, resource := range []struct {
		name  string
		read  func(int64) error
		write func(int64) error
	}{
		{"counter", func(index int64) error { _, err := cr.Read(ctx, "stats", index); return err }, func(index int64) error { return cr.Write(ctx, "stats", index, 11, 22) }},
		{"meter", func(index int64) error { _, err := mr.Read(ctx, "rate", index); return err }, func(index int64) error { return mr.Write(ctx, "rate", index, cfg) }},
		{"register", func(index int64) error { _, err := rr.Read(ctx, "state", index); return err }, func(index int64) error { return rr.Write(ctx, "state", index, []byte{1}) }},
	} {
		t.Run(resource.name+" invalid indexes", func(t *testing.T) {
			for _, index := range []int64{math.MinInt64, -2, 4, math.MaxInt64} {
				require.ErrorContains(t, resource.read(index), "index")
				require.ErrorContains(t, resource.write(index), "index")
			}
			require.ErrorContains(t, resource.write(-1), "index")
			require.Empty(t, requests, "invalid read attempted an RPC")
			require.Zero(t, writes.Load(), "invalid write attempted an RPC")
		})
	}
	t.Run("counter boundaries", func(t *testing.T) {
		for _, index := range []int64{0, 3} {
			require.NoError(t, cr.Write(ctx, "stats", index, 11+index, 22+index))
			samples, err := cr.Read(ctx, "stats", index)
			require.NoError(t, err)
			require.Len(t, samples, 1)
			require.Equal(t, index, samples[0].Index)
			require.Equal(t, 11+index, samples[0].Packets)
			require.Equal(t, 22+index, samples[0].Bytes)
		}
		samples, err := cr.Read(ctx, "stats", -1)
		require.NoError(t, err)
		require.Len(t, samples, 4)
		indexes := make([]int64, 0, len(samples))
		for _, sample := range samples {
			indexes = append(indexes, sample.Index)
		}
		require.ElementsMatch(t, []int64{0, 1, 2, 3}, indexes)
	})
	t.Run("meter boundaries", func(t *testing.T) {
		want := &p4v1.MeterConfig{Cir: cfg.CIR, Cburst: cfg.CBurst, Pir: cfg.PIR, Pburst: cfg.PBurst}
		for _, index := range []int64{0, 3} {
			require.NoError(t, mr.Write(ctx, "rate", index, cfg))
			entries, err := mr.Read(ctx, "rate", index)
			require.NoError(t, err)
			require.Len(t, entries, 1)
			require.NotNil(t, entries[0].Index)
			require.Equal(t, index, entries[0].Index.Index)
			require.True(t, proto.Equal(want, entries[0].Config))
		}
		entries, err := mr.Read(ctx, "rate", -1)
		require.NoError(t, err)
		require.Len(t, entries, 4)
		indexes := make([]int64, 0, len(entries))
		for _, entry := range entries {
			require.NotNil(t, entry.Index)
			indexes = append(indexes, entry.Index.Index)
		}
		require.ElementsMatch(t, []int64{0, 1, 2, 3}, indexes)
	})
	t.Run("register boundaries", func(t *testing.T) {
		_, err := rr.Read(ctx, "state", 0)
		if status.Code(err) == codes.Unimplemented {
			require.ErrorIs(t, rr.Write(ctx, "state", 0, []byte{1}), errs.ErrTargetUnsupported)
			t.Skip("target does not implement RegisterEntry RPCs, valid register indexes are covered by SDK protocol tests")
		}
		require.NoError(t, err)
		for _, index := range []int64{0, 3} {
			require.NoError(t, rr.Write(ctx, "state", index, []byte{1}))
			entries, err := rr.Read(ctx, "state", index)
			require.NoError(t, err)
			require.Len(t, entries, 1)
			require.NotNil(t, entries[0].Index)
			require.Equal(t, index, entries[0].Index.Index)
			require.Equal(t, []byte{1}, entries[0].GetData().GetBitstring())
		}
		entries, err := rr.Read(ctx, "state", -1)
		require.NoError(t, err)
		require.Len(t, entries, 4)
	})
}
