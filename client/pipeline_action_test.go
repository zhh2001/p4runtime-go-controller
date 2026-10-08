package client

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestPipelineFallbackError(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		action SetPipelineAction
		want   bool
	}{
		{"unimplemented", status.Error(codes.Unimplemented, "unsupported"), PipelineVerifyAndCommit, true},
		{"generic action", status.Error(codes.InvalidArgument, "action not supported on this target"), PipelineVerifyAndCommit, true},
		{"unsupported action", status.Error(codes.InvalidArgument, "unsupported action"), PipelineVerifyAndCommit, true},
		{"named action", status.Error(codes.InvalidArgument, "Action 'VERIFY_AND_COMMIT' is not supported."), PipelineVerifyAndCommit, true},
		{"qualified action", status.Error(codes.InvalidArgument, "SetForwardingPipelineConfig action VERIFY_AND_COMMIT is unsupported by this target"), PipelineVerifyAndCommit, true},
		{"case and spacing", status.Error(codes.InvalidArgument, "  VERIFY_AND_COMMIT  NOT SUPPORTED  "), PipelineVerifyAndCommit, true},
		{"RPC action", status.Error(codes.InvalidArgument, "SetForwardingPipelineConfig action not supported"), PipelineVerifyAndCommit, true},
		{"different action", status.Error(codes.InvalidArgument, "RECONCILE_AND_COMMIT not supported"), PipelineVerifyAndCommit, false},
		{"parser feature", status.Error(codes.InvalidArgument, "parser feature not supported"), PipelineVerifyAndCommit, false},
		{"parser action", status.Error(codes.InvalidArgument, "parser action not supported"), PipelineVerifyAndCommit, false},
		{"action parameter", status.Error(codes.InvalidArgument, "unsupported action parameter"), PipelineVerifyAndCommit, false},
		{"action with parser failure", status.Error(codes.InvalidArgument, "VERIFY_AND_COMMIT failed: parser feature not supported"), PipelineVerifyAndCommit, false},
		{"reconciliation failure", status.Error(codes.InvalidArgument, "forwarding state cannot be preserved"), PipelineReconcileAndCommit, false},
		{"permission denied", status.Error(codes.PermissionDenied, "action not supported"), PipelineVerifyAndCommit, false},
		{"deadline", status.Error(codes.DeadlineExceeded, "action not supported"), PipelineVerifyAndCommit, false},
		{"non status", errors.New("action not supported"), PipelineVerifyAndCommit, false},
		{"wrapped status", fmt.Errorf("outer: %w", status.Error(codes.Unimplemented, "unsupported")), PipelineVerifyAndCommit, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isFallbackError(tc.err, tc.action))
		})
	}
}
