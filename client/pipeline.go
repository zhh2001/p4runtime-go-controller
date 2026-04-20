package client

import (
	"context"
	"errors"
	"fmt"
	"strings"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	errs "github.com/zhh2001/p4runtime-go-controller/errors"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
)

// SetPipelineAction selects the SetForwardingPipelineConfig action that the
// caller wants to attempt first. If the target does not support it, Client
// falls back through RECONCILE_AND_COMMIT and finally COMMIT. Callers who
// need a specific action without fallback can set NoFallback=true in
// SetPipelineOptions.
type SetPipelineAction = p4v1.SetForwardingPipelineConfigRequest_Action

// Re-exported action constants for caller convenience.
const (
	PipelineVerify             SetPipelineAction = p4v1.SetForwardingPipelineConfigRequest_VERIFY
	PipelineVerifyAndSave      SetPipelineAction = p4v1.SetForwardingPipelineConfigRequest_VERIFY_AND_SAVE
	PipelineVerifyAndCommit    SetPipelineAction = p4v1.SetForwardingPipelineConfigRequest_VERIFY_AND_COMMIT
	PipelineCommit             SetPipelineAction = p4v1.SetForwardingPipelineConfigRequest_COMMIT
	PipelineReconcileAndCommit SetPipelineAction = p4v1.SetForwardingPipelineConfigRequest_RECONCILE_AND_COMMIT
)

// SetPipelineOptions tunes Client.SetPipeline.
type SetPipelineOptions struct {
	// Action is the first action to attempt. Defaults to VERIFY_AND_COMMIT.
	Action SetPipelineAction
	// NoFallback disables the automatic fallback chain.
	NoFallback bool
}

// SetPipelineResult reports which action actually succeeded and, when
// applicable, the fallback chain that was walked.
type SetPipelineResult struct {
	// Action is the action that the target accepted.
	Action SetPipelineAction
	// Attempted records every action tried in order, including the
	// one that finally succeeded.
	Attempted []SetPipelineAction
}

// SetPipeline pushes a pipeline.Pipeline onto the target. It honors the
// election ID and device ID configured on the Client.
//
// The fallback chain mirrors the recommendation in
// `docs/troubleshooting.md`: start with VERIFY_AND_COMMIT (strictest), fall
// back to RECONCILE_AND_COMMIT (permissive of in-flight state), then COMMIT
// (non-verifying). A target that returns UNIMPLEMENTED triggers the
// fallback; targets that return INVALID_ARGUMENT with the substring
// "action" + "not supported" are also treated as fallback triggers. All
// other errors bubble up unchanged.
func (c *Client) SetPipeline(ctx context.Context, p *pipeline.Pipeline, opts SetPipelineOptions) (SetPipelineResult, error) {
	if p == nil {
		return SetPipelineResult{}, fmt.Errorf("client.SetPipeline: %w", errors.New("nil pipeline"))
	}
	if !c.IsPrimary() {
		return SetPipelineResult{}, errs.ErrNotPrimary
	}

	actions := []SetPipelineAction{PipelineVerifyAndCommit, PipelineReconcileAndCommit, PipelineCommit}
	if opts.Action != 0 {
		actions = reorderActions(opts.Action, opts.NoFallback)
	}

	cfg := &p4v1.ForwardingPipelineConfig{
		P4Info:         p.Info(),
		P4DeviceConfig: p.DeviceConfig(),
	}

	var result SetPipelineResult
	var lastErr error
	for _, act := range actions {
		result.Attempted = append(result.Attempted, act)
		req := &p4v1.SetForwardingPipelineConfigRequest{
			DeviceId: c.opts.deviceID,
			ElectionId: &p4v1.Uint128{
				High: c.opts.electionID.High,
				Low:  c.opts.electionID.Low,
			},
			Role:   c.opts.role,
			Action: act,
			Config: cfg,
		}
		_, err := c.rpc.SetForwardingPipelineConfig(ctx, req)
		if err == nil {
			result.Action = act
			return result, nil
		}
		lastErr = err
		if opts.NoFallback || !isFallbackError(err) {
			return result, fmt.Errorf("SetForwardingPipelineConfig(%s): %w", actionName(act), err)
		}
		c.opts.logger.InfoContext(ctx, "p4runtime: SetForwardingPipelineConfig fallback",
			"failed_action", actionName(act),
			"error", err.Error())
	}
	return result, fmt.Errorf("SetForwardingPipelineConfig exhausted actions: %w", lastErr)
}

// GetPipeline fetches the forwarding pipeline currently active on the
// target. A nil pipeline is returned when the target has no pipeline
// installed (ErrPipelineNotSet).
func (c *Client) GetPipeline(ctx context.Context) (*pipeline.Pipeline, error) {
	req := &p4v1.GetForwardingPipelineConfigRequest{
		DeviceId:     c.opts.deviceID,
		ResponseType: p4v1.GetForwardingPipelineConfigRequest_ALL,
	}
	resp, err := c.rpc.GetForwardingPipelineConfig(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("GetForwardingPipelineConfig: %w", err)
	}
	cfg := resp.GetConfig()
	if cfg == nil || cfg.GetP4Info() == nil {
		return nil, errs.ErrPipelineNotSet
	}
	return pipeline.New(cfg.GetP4Info(), cfg.GetP4DeviceConfig())
}

func reorderActions(preferred SetPipelineAction, noFallback bool) []SetPipelineAction {
	if noFallback {
		return []SetPipelineAction{preferred}
	}
	chain := []SetPipelineAction{PipelineVerifyAndCommit, PipelineReconcileAndCommit, PipelineCommit}
	out := []SetPipelineAction{preferred}
	for _, a := range chain {
		if a == preferred {
			continue
		}
		out = append(out, a)
	}
	return out
}

func isFallbackError(err error) bool {
	st, ok := status.FromError(err)
	if !ok {
		return false
	}
	switch st.Code() {
	case codes.Unimplemented:
		return true
	case codes.InvalidArgument:
		msg := strings.ToLower(st.Message())
		return strings.Contains(msg, "not supported") ||
			strings.Contains(msg, "unsupported action")
	}
	return false
}

func actionName(a SetPipelineAction) string {
	if s, ok := p4v1.SetForwardingPipelineConfigRequest_Action_name[int32(a)]; ok {
		return s
	}
	return fmt.Sprintf("unknown(%d)", int32(a))
}
