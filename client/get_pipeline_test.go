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
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/zhh2001/p4runtime-go-controller/client"
	errs "github.com/zhh2001/p4runtime-go-controller/errors"
	"github.com/zhh2001/p4runtime-go-controller/internal/testutil"
)

func TestGetPipeline_TargetErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		code    codes.Code
		message string
		missing bool
	}{
		{"BMv2", codes.FailedPrecondition, "No forwarding pipeline config set for this device", true},
		{"no forwarding config", codes.FailedPrecondition, "no forwarding pipeline config set", true},
		{"no config", codes.FailedPrecondition, "no pipeline config set", true},
		{"not set", codes.FailedPrecondition, "pipeline not set", true},
		{"is not set", codes.FailedPrecondition, "pipeline is not set on this device", true},
		{"not configured", codes.FailedPrecondition, "pipeline not configured for this device", true},
		{"case and spacing", codes.FailedPrecondition, "  NO Forwarding Pipeline CONFIG set  for this device.  ", true},
		{"pipeline mismatch", codes.FailedPrecondition, "pipeline cookie differs", false},
		{"not primary", codes.FailedPrecondition, "not primary controller", false},
		{"pipeline dependency", codes.FailedPrecondition, "pipeline not set because another dependency failed", false},
		{"quoted message", codes.FailedPrecondition, "upstream reported: no forwarding pipeline config set", false},
		{"permission denied", codes.PermissionDenied, "no forwarding pipeline config set for this device", false},
		{"unknown device", codes.NotFound, "device does not exist", false},
		{"not found", codes.NotFound, "no forwarding pipeline config set", false},
		{"unsupported RPC", codes.Unimplemented, "GetForwardingPipelineConfig unsupported", false},
		{"deadline", codes.DeadlineExceeded, "no forwarding pipeline config set", false},
		{"canceled", codes.Canceled, "request canceled", false},
		{"internal", codes.Internal, "no forwarding pipeline config set", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			detail := wrapperspb.String("target diagnostics")
			detail.ProtoReflect().SetUnknown([]byte{0x10, 0x01})
			st, err := status.New(tc.code, tc.message).WithDetails(detail)
			require.NoError(t, err)
			original := st.Err()
			h := testutil.StartServer(t)
			h.Mu.Lock()
			h.OverrideGetPipelineErr = original
			h.PrimaryElectionLow = 99
			h.Mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			c, err := dialViaHarness(ctx, h, client.WithDeviceID(17))
			require.NoError(t, err)
			defer c.Close()
			require.Equal(t, client.StateBackup, c.State())
			p, err := c.GetPipeline(ctx)
			require.Error(t, err)
			assert.Nil(t, p)
			if tc.missing {
				assert.ErrorIs(t, err, errs.ErrPipelineNotSet)
			} else {
				assert.NotErrorIs(t, err, errs.ErrPipelineNotSet)
			}
			assert.ErrorIs(t, err, original)
			assert.Equal(t, tc.code, status.Code(err))
			got, ok := status.FromError(err)
			require.True(t, ok)
			assert.True(t, proto.Equal(st.Proto(), got.Proto()), "gRPC status changed: %v", got)
			assert.Contains(t, err.Error(), "GetForwardingPipelineConfig")
			h.Mu.Lock()
			defer h.Mu.Unlock()
			require.Len(t, h.GetPipelineRequests, 1)
			assert.EqualValues(t, 17, h.GetPipelineRequests[0].GetDeviceId())
			assert.Equal(t, p4v1.GetForwardingPipelineConfigRequest_ALL, h.GetPipelineRequests[0].GetResponseType())
			assert.Empty(t, h.SetPipelineRequests, "GetPipeline must not install a pipeline")
		})
	}
}

func TestGetPipeline_ResponseForms(t *testing.T) {
	info := samplePipeline(t).Info()
	for _, tc := range []struct {
		name   string
		config *p4v1.ForwardingPipelineConfig
		ok     bool
	}{
		{name: "unset config"},
		{name: "empty config", config: &p4v1.ForwardingPipelineConfig{}},
		{name: "no P4Info", config: &p4v1.ForwardingPipelineConfig{P4DeviceConfig: []byte{1}}},
		{name: "cookie only", config: &p4v1.ForwardingPipelineConfig{Cookie: &p4v1.ForwardingPipelineConfig_Cookie{Cookie: 3}}},
		{name: "P4Info only", config: &p4v1.ForwardingPipelineConfig{P4Info: info}, ok: true},
		{name: "complete", config: &p4v1.ForwardingPipelineConfig{P4Info: info, P4DeviceConfig: []byte{1}}, ok: true},
		{name: "empty P4Info", config: &p4v1.ForwardingPipelineConfig{P4Info: &p4configv1.P4Info{}}, ok: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := testutil.StartServer(t)
			h.Mu.Lock()
			h.GetPipelineResp = &p4v1.GetForwardingPipelineConfigResponse{Config: tc.config}
			h.Mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			c, err := dialViaHarness(ctx, h)
			require.NoError(t, err)
			defer c.Close()
			p, err := c.GetPipeline(ctx)
			if !tc.ok {
				assert.ErrorIs(t, err, errs.ErrPipelineNotSet)
				assert.Nil(t, p)
			} else {
				require.NoError(t, err)
				require.NotNil(t, p)
				assert.True(t, proto.Equal(tc.config.GetP4Info(), p.Info()))
				assert.Equal(t, tc.config.GetP4DeviceConfig(), p.DeviceConfig())
			}
		})
	}
}
