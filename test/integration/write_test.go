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
)

// This test must run separately, before any pipeline is installed.
func TestBMv2_WriteWithoutPipeline(t *testing.T) {
	if os.Getenv("P4RT_TEST_FRESH_TARGET") != "1" {
		t.Skip("P4RT_TEST_FRESH_TARGET unset; requires a fresh target without a pipeline")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, targetAddr(), client.WithDeviceID(1),
		client.WithElectionID(client.ElectionID{Low: 1}), client.WithInsecure())
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.BecomePrimary(ctx))
	err = c.WriteTableEntry(ctx, client.UpdateInsert, &p4v1.TableEntry{TableId: 1})
	require.ErrorIs(t, err, errs.ErrPipelineNotSet)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	var writeErr *errs.WriteError
	require.ErrorAs(t, err, &writeErr)
	require.Nil(t, writeErr.Updates)
}

func TestBMv2_WriteWithStaleElection(t *testing.T) {
	if deviceConfigPath() == "" {
		t.Skip("P4RT_DEVICE_CONFIG unset; skipping live write authorization")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Keep the stream primary and submit a different election ID on the
	// unary RPC, so the target must reject it before processing updates.
	interceptor := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if request, ok := req.(*p4v1.WriteRequest); ok {
			copyRequest := proto.Clone(request).(*p4v1.WriteRequest)
			copyRequest.ElectionId = &p4v1.Uint128{Low: 1}
			req = copyRequest
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
	c, err := client.Dial(ctx, targetAddr(), client.WithDeviceID(1),
		client.WithElectionID(client.ElectionID{Low: 100}), client.WithInsecure(), client.WithUnaryInterceptor(interceptor))
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.BecomePrimary(ctx))
	err = c.WriteTableEntry(ctx, client.UpdateInsert, &p4v1.TableEntry{TableId: 1})
	require.ErrorIs(t, err, errs.ErrNotPrimary)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	var writeErr *errs.WriteError
	require.ErrorAs(t, err, &writeErr)
	require.Nil(t, writeErr.Updates)
}
