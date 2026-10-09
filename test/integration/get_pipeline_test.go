//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/zhh2001/p4runtime-go-controller/v2/client"
	errs "github.com/zhh2001/p4runtime-go-controller/v2/errors"
)

func TestBMv2_GetPipelineWithoutPipeline(t *testing.T) {
	if os.Getenv("P4RT_TEST_FRESH_TARGET") != "1" {
		t.Skip("P4RT_TEST_FRESH_TARGET unset; requires a fresh target without a pipeline")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, targetAddr(), client.WithDeviceID(1), client.WithElectionID(client.ElectionID{Low: 1}), client.WithInsecure())
	require.NoError(t, err)
	defer c.Close()
	_, original := c.RPC().GetForwardingPipelineConfig(ctx, &p4v1.GetForwardingPipelineConfigRequest{
		DeviceId: 1, ResponseType: p4v1.GetForwardingPipelineConfigRequest_ALL,
	})
	require.Equal(t, codes.FailedPrecondition, status.Code(original))
	p, err := c.GetPipeline(ctx)
	require.Nil(t, p)
	require.ErrorIs(t, err, errs.ErrPipelineNotSet)
	require.ErrorIs(t, err, original)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.True(t, proto.Equal(status.Convert(original).Proto(), status.Convert(err).Proto()))
}
