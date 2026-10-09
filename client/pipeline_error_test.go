package client

import (
	"context"
	"errors"
	"fmt"
	"testing"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	errs "github.com/zhh2001/p4runtime-go-controller/errors"
)

type getPipelineFailure struct {
	p4v1.P4RuntimeClient
	err error
}

func (s getPipelineFailure) GetForwardingPipelineConfig(context.Context, *p4v1.GetForwardingPipelineConfigRequest, ...grpc.CallOption) (*p4v1.GetForwardingPipelineConfigResponse, error) {
	return nil, s.err
}

func TestGetPipeline_NonStatusError(t *testing.T) {
	for _, original := range []error{errors.New("pipeline not set"), context.Canceled, context.DeadlineExceeded} {
		c := &Client{rpc: getPipelineFailure{err: original}}
		p, err := c.GetPipeline(context.Background())
		assert.Nil(t, p)
		assert.ErrorIs(t, err, original)
		assert.NotErrorIs(t, err, errs.ErrPipelineNotSet)
		_, ok := status.FromError(err)
		assert.False(t, ok)
	}
}

func TestGetPipeline_WrappedStatus(t *testing.T) {
	st, err := status.New(codes.FailedPrecondition, "No forwarding pipeline config set for this device").WithDetails(
		&p4v1.Error{Space: "target", Code: 7, Message: "configuration absent"},
	)
	require.NoError(t, err)
	original := st.Err()
	interceptorError := fmt.Errorf("interceptor: %w", original)
	c := &Client{rpc: getPipelineFailure{err: interceptorError}}
	p, err := c.GetPipeline(context.Background())
	assert.Nil(t, p)
	assert.ErrorIs(t, err, errs.ErrPipelineNotSet)
	assert.ErrorIs(t, err, interceptorError)
	assert.ErrorIs(t, err, original)
	assert.True(t, proto.Equal(st.Proto(), status.Convert(err).Proto()))
}

func FuzzGetPipelineError(f *testing.F) {
	f.Add(uint8(codes.FailedPrecondition), "No forwarding pipeline config set for this device")
	f.Add(uint8(codes.PermissionDenied), "pipeline not set")
	f.Add(uint8(codes.FailedPrecondition), "pipeline cookie differs")
	f.Fuzz(func(t *testing.T, code uint8, message string) {
		if code == 0 {
			return
		}
		st := status.New(codes.Code(code), message)
		original := st.Err()
		c := &Client{rpc: getPipelineFailure{err: original}}
		p, err := c.GetPipeline(context.Background())
		assert.Nil(t, p)
		require.Error(t, err)
		assert.ErrorIs(t, err, original)
		assert.True(t, proto.Equal(st.Proto(), status.Convert(err).Proto()))
		wrapped := fmt.Errorf("operation: %w", err)
		assert.Equal(t, errors.Is(err, errs.ErrPipelineNotSet), errors.Is(wrapped, errs.ErrPipelineNotSet))
		if errors.Is(err, errs.ErrPipelineNotSet) {
			assert.Equal(t, codes.FailedPrecondition, st.Code())
		}
	})
}
