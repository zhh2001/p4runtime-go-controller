//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/zhh2001/p4runtime-go-controller/client"
	errs "github.com/zhh2001/p4runtime-go-controller/errors"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
	"github.com/zhh2001/p4runtime-go-controller/register"
)

func TestBMv2_RegisterValues(t *testing.T) {
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
	definition, ok := p.Register("state")
	require.True(t, ok)
	require.EqualValues(t, 9, definition.Raw().GetTypeSpec().GetBitstring().GetBit().GetBitwidth())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	requests := make(chan *p4v1.WriteRequest, 16)
	interceptor := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if method == "/p4.v1.P4Runtime/Write" {
			requests <- proto.Clone(req.(*p4v1.WriteRequest)).(*p4v1.WriteRequest)
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
	c, err := client.Dial(ctx, targetAddr(), client.WithDeviceID(1), client.WithElectionID(client.ElectionID{Low: 1}), client.WithInsecure(),
		client.WithDialOptions(grpc.WithUnaryInterceptor(interceptor)))
	require.NoError(t, err)
	defer c.Close()
	_, err = c.SetPipeline(ctx, p, client.SetPipelineOptions{})
	require.NoError(t, err)
	r, err := register.NewReader(c, p)
	require.NoError(t, err)
	data := func(value ...byte) *p4v1.P4Data {
		return &p4v1.P4Data{Data: &p4v1.P4Data_Bitstring{Bitstring: value}}
	}
	t.Run("local validation", func(t *testing.T) {
		require.ErrorIs(t, r.Write(ctx, "state", 0, []byte{2, 0}), errs.ErrInvalidBitWidth)
		require.ErrorIs(t, r.WriteData(ctx, "state", 0, data(2, 0)), errs.ErrInvalidBitWidth)
		require.ErrorContains(t, r.WriteData(ctx, "state", 0, &p4v1.P4Data{Data: &p4v1.P4Data_Bool{Bool: false}}), "P4Data.bitstring")
		require.Error(t, r.WriteData(ctx, "state", 0, nil))
		require.Empty(t, requests, "invalid value attempted an RPC")
	})
	_, readErr := r.Read(ctx, "state", 0)
	unsupported := status.Code(readErr) == codes.Unimplemented
	if !unsupported {
		require.NoError(t, readErr)
	}
	for _, tc := range []struct {
		name string
		call func() error
		want []byte
	}{
		{"empty integer", func() error { return r.Write(ctx, "state", 0, nil) }, []byte{0}},
		{"padded integer", func() error { return r.Write(ctx, "state", 0, []byte{0, 0, 1}) }, []byte{1}},
		{"typed maximum", func() error { return r.WriteData(ctx, "state", 0, data(0, 1, 255)) }, []byte{1, 255}},
		{"typed zero", func() error { return r.WriteData(ctx, "state", 0, data()) }, []byte{0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if unsupported {
				require.ErrorIs(t, err, errs.ErrTargetUnsupported)
			} else {
				require.NoError(t, err)
			}
			require.Len(t, requests, 1)
			req := <-requests
			require.Len(t, req.Updates, 1)
			require.Equal(t, p4v1.Update_MODIFY, req.Updates[0].Type)
			entry := req.Updates[0].Entity.GetRegisterEntry()
			require.Equal(t, definition.ID, entry.GetRegisterId())
			require.NotNil(t, entry.GetIndex())
			require.Zero(t, entry.Index.Index)
			require.True(t, proto.Equal(data(tc.want...), entry.GetData()))
			if !unsupported {
				entries, err := r.Read(ctx, "state", 0)
				require.NoError(t, err)
				require.Len(t, entries, 1)
				require.Equal(t, tc.want, entries[0].GetData().GetBitstring())
			}
		})
	}
	if unsupported {
		t.Log("target does not implement RegisterEntry RPCs; local validation, request encoding and unsupported responses checked; value readback requires a supporting target")
	}
}
