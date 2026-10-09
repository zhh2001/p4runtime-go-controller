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
// caller wants to perform. VERIFY_AND_COMMIT can fall back to
// RECONCILE_AND_COMMIT when unsupported. Other actions are attempted once.
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
	// Action defaults to VERIFY_AND_COMMIT. COMMIT requires a nil pipeline.
	Action SetPipelineAction
	// NoFallback disables VERIFY_AND_COMMIT's fallback to RECONCILE_AND_COMMIT,
	// including when Action is left at its default value.
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

// SetPipeline performs a forwarding pipeline action on the target. It honors the
// election ID and device ID configured on the Client.
//
// COMMIT requires a nil pipeline and commits the target's previously saved
// config. All other actions require a non-nil pipeline. VERIFY and
// VERIFY_AND_SAVE do not install the config, and RECONCILE_AND_COMMIT never
// falls back to an action that clears existing forwarding state.
//
// Unless NoFallback is set, an unsupported VERIFY_AND_COMMIT can fall back
// to RECONCILE_AND_COMMIT. Only UNIMPLEMENTED or an INVALID_ARGUMENT message
// explicitly identifying an unsupported RPC action permits fallback.
func (c *Client) SetPipeline(ctx context.Context, p *pipeline.Pipeline, opts SetPipelineOptions) (SetPipelineResult, error) {
	action := opts.Action
	if action == 0 {
		action = PipelineVerifyAndCommit
	}
	switch action {
	case PipelineCommit:
		if p != nil {
			return SetPipelineResult{}, fmt.Errorf("client.SetPipeline: COMMIT requires a nil pipeline")
		}
	case PipelineVerify, PipelineVerifyAndSave, PipelineVerifyAndCommit, PipelineReconcileAndCommit:
		if p == nil {
			return SetPipelineResult{}, fmt.Errorf("client.SetPipeline: nil pipeline")
		}
	default:
		return SetPipelineResult{}, fmt.Errorf("client.SetPipeline: invalid action %d", action)
	}
	if !c.IsPrimary() {
		return SetPipelineResult{}, errs.ErrNotPrimary
	}

	actions := []SetPipelineAction{action}
	if action == PipelineVerifyAndCommit && !opts.NoFallback {
		actions = append(actions, PipelineReconcileAndCommit)
	}

	var cfg *p4v1.ForwardingPipelineConfig
	if p != nil {
		cfg = &p4v1.ForwardingPipelineConfig{
			P4Info:         p.Info(),
			P4DeviceConfig: p.DeviceConfig(),
		}
	}

	var result SetPipelineResult
	var lastErr error
	for i, act := range actions {
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
		unsupported := isFallbackError(err, act)
		if unsupported {
			err = fmt.Errorf("%w: %w", errs.ErrTargetUnsupported, err)
		}
		lastErr = fmt.Errorf("SetForwardingPipelineConfig(%s): %w", actionName(act), err)
		if !unsupported || i == len(actions)-1 {
			break
		}
		c.opts.logger.InfoContext(ctx, "p4runtime: SetForwardingPipelineConfig fallback",
			"failed_action", actionName(act),
			"error", err.Error())
	}
	return result, lastErr
}

// GetPipeline fetches the forwarding pipeline currently active on the
// target. A nil pipeline is returned when the target has no pipeline
// installed (ErrPipelineNotSet). An explicit target error retains its
// original gRPC status, including message and details.
func (c *Client) GetPipeline(ctx context.Context) (*pipeline.Pipeline, error) {
	req := &p4v1.GetForwardingPipelineConfigRequest{
		DeviceId:     c.opts.deviceID,
		ResponseType: p4v1.GetForwardingPipelineConfigRequest_ALL,
	}
	resp, err := c.rpc.GetForwardingPipelineConfig(ctx, req)
	if err != nil {
		var rpcError interface{ GRPCStatus() *status.Status }
		if errors.As(err, &rpcError) {
			if st := rpcError.GRPCStatus(); st != nil {
				return nil, &getPipelineError{cause: err, rpcStatus: st}
			}
		}
		return nil, fmt.Errorf("GetForwardingPipelineConfig: %w", err)
	}
	cfg := resp.GetConfig()
	if cfg == nil || cfg.GetP4Info() == nil {
		return nil, errs.ErrPipelineNotSet
	}
	return pipeline.New(cfg.GetP4Info(), cfg.GetP4DeviceConfig())
}

func isFallbackError(err error, action SetPipelineAction) bool {
	st, ok := status.FromError(err)
	if !ok {
		return false
	}
	switch st.Code() {
	case codes.Unimplemented:
		return true
	case codes.InvalidArgument:
		msg := strings.ToLower(st.Message())
		msg = strings.NewReplacer(":", " ", "'", "", "\"", "").Replace(msg)
		msg = strings.Trim(strings.Join(strings.Fields(msg), " "), " .")
		for _, suffix := range []string{" on this target", " on the target", " by this target", " by the target"} {
			msg = strings.TrimSuffix(msg, suffix)
		}
		name := strings.ToLower(actionName(action))
		for _, subject := range []string{"action", name, "action " + name, "setforwardingpipelineconfig action", "setforwardingpipelineconfig action " + name} {
			if msg == "unsupported "+subject || msg == subject+" not supported" || msg == subject+" is not supported" ||
				msg == subject+" unsupported" || msg == subject+" is unsupported" {
				return true
			}
		}
	}
	return false
}

func actionName(a SetPipelineAction) string {
	if s, ok := p4v1.SetForwardingPipelineConfigRequest_Action_name[int32(a)]; ok {
		return s
	}
	return fmt.Sprintf("unknown(%d)", int32(a))
}
