package client_test

import (
	"context"
	"testing"
	"time"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/zhh2001/p4runtime-go-controller/client"
	errs "github.com/zhh2001/p4runtime-go-controller/errors"
	"github.com/zhh2001/p4runtime-go-controller/internal/testutil"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
)

func samplePipeline(t *testing.T) *pipeline.Pipeline {
	t.Helper()
	info := &p4configv1.P4Info{
		PkgInfo: &p4configv1.PkgInfo{Arch: "v1model"},
		Tables: []*p4configv1.Table{{
			Preamble: &p4configv1.Preamble{Id: 1, Name: "t"},
			Size:     1,
		}},
	}
	p, err := pipeline.New(info, []byte{0xaa})
	require.NoError(t, err)
	return p
}

func TestSetPipeline_HappyPath(t *testing.T) {
	h := testutil.StartServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := dialViaHarness(ctx, h)
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.BecomePrimary(ctx))

	res, err := c.SetPipeline(ctx, samplePipeline(t), client.SetPipelineOptions{})
	require.NoError(t, err)
	assert.Equal(t, client.PipelineVerifyAndCommit, res.Action)
	assert.Equal(t, []client.SetPipelineAction{client.PipelineVerifyAndCommit}, res.Attempted)
}

func TestSetPipeline_FallbackChain(t *testing.T) {
	h := testutil.StartServer(t)
	h.Mu.Lock()
	h.SetPipelineErrByAction = map[p4v1.SetForwardingPipelineConfigRequest_Action]error{
		p4v1.SetForwardingPipelineConfigRequest_VERIFY_AND_COMMIT:    status.Error(codes.Unimplemented, "verify not supported"),
		p4v1.SetForwardingPipelineConfigRequest_RECONCILE_AND_COMMIT: status.Error(codes.InvalidArgument, "action not supported on this target"),
	}
	h.Mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := dialViaHarness(ctx, h)
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.BecomePrimary(ctx))

	res, err := c.SetPipeline(ctx, samplePipeline(t), client.SetPipelineOptions{})
	require.NoError(t, err)
	assert.Equal(t, client.PipelineCommit, res.Action)
	assert.Equal(t, []client.SetPipelineAction{
		client.PipelineVerifyAndCommit,
		client.PipelineReconcileAndCommit,
		client.PipelineCommit,
	}, res.Attempted)
}

func TestSetPipeline_NoFallback(t *testing.T) {
	h := testutil.StartServer(t)
	h.Mu.Lock()
	h.SetPipelineErrByAction = map[p4v1.SetForwardingPipelineConfigRequest_Action]error{
		p4v1.SetForwardingPipelineConfigRequest_VERIFY_AND_COMMIT: status.Error(codes.Unimplemented, "nope"),
	}
	h.Mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := dialViaHarness(ctx, h)
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.BecomePrimary(ctx))

	res, err := c.SetPipeline(ctx, samplePipeline(t), client.SetPipelineOptions{
		Action:     client.PipelineVerifyAndCommit,
		NoFallback: true,
	})
	require.Error(t, err)
	assert.Len(t, res.Attempted, 1)
	assert.Contains(t, err.Error(), "Unimplemented")
}

func TestSetPipeline_NonFallbackErrorBubbles(t *testing.T) {
	h := testutil.StartServer(t)
	h.Mu.Lock()
	h.SetPipelineErrByAction = map[p4v1.SetForwardingPipelineConfigRequest_Action]error{
		p4v1.SetForwardingPipelineConfigRequest_VERIFY_AND_COMMIT: status.Error(codes.PermissionDenied, "denied"),
	}
	h.Mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := dialViaHarness(ctx, h)
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.BecomePrimary(ctx))

	_, err = c.SetPipeline(ctx, samplePipeline(t), client.SetPipelineOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PermissionDenied")
}

func TestSetPipeline_RejectsWhenNotPrimary(t *testing.T) {
	h := testutil.StartServer(t)
	h.Mu.Lock()
	h.PrimaryElectionHigh = 0
	h.PrimaryElectionLow = 99
	h.Mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := dialViaHarness(ctx, h)
	require.NoError(t, err)
	defer c.Close()

	require.Eventually(t, func() bool { return c.State() == client.StateBackup }, time.Second, 10*time.Millisecond)

	_, err = c.SetPipeline(ctx, samplePipeline(t), client.SetPipelineOptions{})
	assert.ErrorIs(t, err, errs.ErrNotPrimary)
}

func TestSetPipeline_NilPipeline(t *testing.T) {
	h := testutil.StartServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := dialViaHarness(ctx, h)
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.BecomePrimary(ctx))

	_, err = c.SetPipeline(ctx, nil, client.SetPipelineOptions{})
	assert.Error(t, err)
}

func TestGetPipeline_NoConfig(t *testing.T) {
	h := testutil.StartServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := dialViaHarness(ctx, h)
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.BecomePrimary(ctx))

	_, err = c.GetPipeline(ctx)
	assert.ErrorIs(t, err, errs.ErrPipelineNotSet)
}

func TestGetPipeline_WithConfig(t *testing.T) {
	h := testutil.StartServer(t)
	info := &p4configv1.P4Info{
		PkgInfo: &p4configv1.PkgInfo{Arch: "v1model"},
		Tables: []*p4configv1.Table{{
			Preamble: &p4configv1.Preamble{Id: 5, Name: "fetch.t"},
		}},
	}
	h.Mu.Lock()
	h.GetPipelineResp = &p4v1.GetForwardingPipelineConfigResponse{
		Config: &p4v1.ForwardingPipelineConfig{P4Info: info, P4DeviceConfig: []byte{0x01}},
	}
	h.Mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := dialViaHarness(ctx, h)
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.BecomePrimary(ctx))

	p, err := c.GetPipeline(ctx)
	require.NoError(t, err)
	_, ok := p.Table("fetch.t")
	assert.True(t, ok)
	assert.Equal(t, []byte{0x01}, p.DeviceConfig())
}
