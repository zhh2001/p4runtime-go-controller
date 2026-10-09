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
	"google.golang.org/protobuf/proto"

	"github.com/zhh2001/p4runtime-go-controller/v2/client"
	errs "github.com/zhh2001/p4runtime-go-controller/v2/errors"
	"github.com/zhh2001/p4runtime-go-controller/v2/internal/testutil"
	"github.com/zhh2001/p4runtime-go-controller/v2/pipeline"
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
		p4v1.SetForwardingPipelineConfigRequest_VERIFY_AND_COMMIT: status.Error(codes.Unimplemented, "verify not supported"),
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
	assert.Equal(t, client.PipelineReconcileAndCommit, res.Action)
	assert.Equal(t, []client.SetPipelineAction{
		client.PipelineVerifyAndCommit,
		client.PipelineReconcileAndCommit,
	}, res.Attempted)
}

func TestSetPipeline_NoFallback(t *testing.T) {
	for _, action := range []client.SetPipelineAction{0, client.PipelineVerifyAndCommit} {
		t.Run(action.String(), func(t *testing.T) {
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
				Action:     action,
				NoFallback: true,
			})
			require.Error(t, err)
			assert.Len(t, res.Attempted, 1)
			assert.Contains(t, err.Error(), "Unimplemented")
			assert.ErrorIs(t, err, errs.ErrTargetUnsupported)
			assert.Equal(t, codes.Unimplemented, status.Code(err))
		})
	}
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
	_, err = c.SetPipeline(ctx, nil, client.SetPipelineOptions{Action: client.PipelineCommit})
	assert.ErrorIs(t, err, errs.ErrNotPrimary)
	h.Mu.Lock()
	defer h.Mu.Unlock()
	assert.Empty(t, h.SetPipelineRequests)
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

func TestSetPipeline_CommitSavedConfig(t *testing.T) {
	h := testutil.StartServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := dialViaHarness(ctx, h)
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.BecomePrimary(ctx))

	res, err := c.SetPipeline(ctx, nil, client.SetPipelineOptions{Action: client.PipelineCommit})
	require.NoError(t, err)
	assert.Equal(t, client.PipelineCommit, res.Action)
	assert.Equal(t, []client.SetPipelineAction{client.PipelineCommit}, res.Attempted)
	h.Mu.Lock()
	defer h.Mu.Unlock()
	require.NotNil(t, h.SetPipelineReq)
	assert.Nil(t, h.SetPipelineReq.Config)
}

func TestSetPipeline_SaveThenCommitRequests(t *testing.T) {
	h := testutil.StartServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := dialViaHarness(ctx, h)
	require.NoError(t, err)
	defer c.Close()
	p := samplePipeline(t)
	res, err := c.SetPipeline(ctx, p, client.SetPipelineOptions{Action: client.PipelineVerifyAndSave})
	require.NoError(t, err)
	assert.Equal(t, client.PipelineVerifyAndSave, res.Action)
	assert.Equal(t, []client.SetPipelineAction{client.PipelineVerifyAndSave}, res.Attempted)
	res, err = c.SetPipeline(ctx, nil, client.SetPipelineOptions{Action: client.PipelineCommit})
	require.NoError(t, err)
	assert.Equal(t, client.PipelineCommit, res.Action)
	assert.Equal(t, []client.SetPipelineAction{client.PipelineCommit}, res.Attempted)
	h.Mu.Lock()
	defer h.Mu.Unlock()
	require.Len(t, h.SetPipelineRequests, 2)
	assert.True(t, proto.Equal(p.Info(), h.SetPipelineRequests[0].GetConfig().GetP4Info()))
	assert.Equal(t, p.DeviceConfig(), h.SetPipelineRequests[0].GetConfig().GetP4DeviceConfig())
	assert.Nil(t, h.SetPipelineRequests[1].Config)
	for _, req := range h.SetPipelineRequests {
		assert.Equal(t, c.DeviceID(), req.GetDeviceId())
		assert.Equal(t, c.ElectionID().High, req.GetElectionId().GetHigh())
		assert.Equal(t, c.ElectionID().Low, req.GetElectionId().GetLow())
	}
}

func TestSetPipeline_ExplicitActionsDoNotFallback(t *testing.T) {
	for _, action := range []client.SetPipelineAction{client.PipelineVerify, client.PipelineVerifyAndSave, client.PipelineCommit, client.PipelineReconcileAndCommit} {
		t.Run(action.String(), func(t *testing.T) {
			h := testutil.StartServer(t)
			h.Mu.Lock()
			h.SetPipelineErrByAction = map[client.SetPipelineAction]error{action: status.Error(codes.Unimplemented, "action unsupported")}
			h.Mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			c, err := dialViaHarness(ctx, h)
			require.NoError(t, err)
			defer c.Close()
			p := samplePipeline(t)
			if action == client.PipelineCommit {
				p = nil
			}
			res, err := c.SetPipeline(ctx, p, client.SetPipelineOptions{Action: action})
			require.ErrorIs(t, err, errs.ErrTargetUnsupported)
			assert.Equal(t, codes.Unimplemented, status.Code(err))
			assert.Equal(t, []client.SetPipelineAction{action}, res.Attempted)
			h.Mu.Lock()
			assert.Len(t, h.SetPipelineRequests, 1)
			h.Mu.Unlock()
		})
	}
}

func TestSetPipeline_ExhaustsOnlyInstallActions(t *testing.T) {
	h := testutil.StartServer(t)
	h.Mu.Lock()
	h.SetPipelineErrByAction = map[client.SetPipelineAction]error{
		client.PipelineVerifyAndCommit:    status.Error(codes.Unimplemented, "action unsupported"),
		client.PipelineReconcileAndCommit: status.Error(codes.InvalidArgument, "action not supported on this target"),
	}
	h.Mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := dialViaHarness(ctx, h)
	require.NoError(t, err)
	defer c.Close()
	p := samplePipeline(t)
	res, err := c.SetPipeline(ctx, p, client.SetPipelineOptions{})
	require.ErrorIs(t, err, errs.ErrTargetUnsupported)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Zero(t, res.Action)
	assert.Equal(t, []client.SetPipelineAction{client.PipelineVerifyAndCommit, client.PipelineReconcileAndCommit}, res.Attempted)
	h.Mu.Lock()
	defer h.Mu.Unlock()
	require.Len(t, h.SetPipelineRequests, 2)
	for _, req := range h.SetPipelineRequests {
		assert.True(t, proto.Equal(p.Info(), req.GetConfig().GetP4Info()))
		assert.Equal(t, p.DeviceConfig(), req.GetConfig().GetP4DeviceConfig())
	}
}

func TestSetPipeline_FallbackPreservesFinalFailure(t *testing.T) {
	h := testutil.StartServer(t)
	finalErr := status.Error(codes.InvalidArgument, "forwarding state cannot be preserved")
	h.Mu.Lock()
	h.SetPipelineErrByAction = map[client.SetPipelineAction]error{
		client.PipelineVerifyAndCommit:    status.Error(codes.Unimplemented, "action unsupported"),
		client.PipelineReconcileAndCommit: finalErr,
	}
	h.Mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := dialViaHarness(ctx, h)
	require.NoError(t, err)
	defer c.Close()
	res, err := c.SetPipeline(ctx, samplePipeline(t), client.SetPipelineOptions{})
	require.ErrorIs(t, err, finalErr)
	assert.NotErrorIs(t, err, errs.ErrTargetUnsupported)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Contains(t, err.Error(), "RECONCILE_AND_COMMIT")
	assert.Equal(t, []client.SetPipelineAction{client.PipelineVerifyAndCommit, client.PipelineReconcileAndCommit}, res.Attempted)
	h.Mu.Lock()
	defer h.Mu.Unlock()
	assert.Len(t, h.SetPipelineRequests, 2)
}

func TestSetPipeline_CommitMissingSavedConfig(t *testing.T) {
	h := testutil.StartServer(t)
	original := status.Error(codes.NotFound, "no saved config")
	h.Mu.Lock()
	h.SetPipelineErrByAction = map[client.SetPipelineAction]error{client.PipelineCommit: original}
	h.Mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := dialViaHarness(ctx, h)
	require.NoError(t, err)
	defer c.Close()
	res, err := c.SetPipeline(ctx, nil, client.SetPipelineOptions{Action: client.PipelineCommit})
	require.ErrorIs(t, err, original)
	assert.Equal(t, codes.NotFound, status.Code(err))
	assert.Equal(t, []client.SetPipelineAction{client.PipelineCommit}, res.Attempted)
	h.Mu.Lock()
	defer h.Mu.Unlock()
	assert.Len(t, h.SetPipelineRequests, 1)
	assert.Nil(t, h.SetPipelineReq.Config)
}

func TestSetPipeline_InvalidActionOrConfigDoesNotSend(t *testing.T) {
	h := testutil.StartServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := dialViaHarness(ctx, h)
	require.NoError(t, err)
	defer c.Close()
	p := samplePipeline(t)
	for _, tc := range []struct {
		name   string
		action client.SetPipelineAction
		p      *pipeline.Pipeline
	}{
		{"COMMIT with config", client.PipelineCommit, p},
		{"unknown action", 99, p},
		{"negative action", -1, p},
		{"default without config", 0, nil},
		{"VERIFY without config", client.PipelineVerify, nil},
		{"SAVE without config", client.PipelineVerifyAndSave, nil},
		{"install without config", client.PipelineVerifyAndCommit, nil},
		{"reconcile without config", client.PipelineReconcileAndCommit, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := c.SetPipeline(ctx, tc.p, client.SetPipelineOptions{Action: tc.action})
			require.Error(t, err)
			assert.Empty(t, res.Attempted)
		})
	}
	h.Mu.Lock()
	defer h.Mu.Unlock()
	assert.Empty(t, h.SetPipelineRequests)
}

func TestSetPipeline_UnrelatedUnsupportedFeatureStops(t *testing.T) {
	h := testutil.StartServer(t)
	h.Mu.Lock()
	h.SetPipelineErrByAction = map[client.SetPipelineAction]error{
		client.PipelineVerifyAndCommit: status.Error(codes.InvalidArgument, "parser feature not supported"),
	}
	h.Mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := dialViaHarness(ctx, h)
	require.NoError(t, err)
	defer c.Close()
	res, err := c.SetPipeline(ctx, samplePipeline(t), client.SetPipelineOptions{})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Equal(t, []client.SetPipelineAction{client.PipelineVerifyAndCommit}, res.Attempted)
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
