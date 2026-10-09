package client

import (
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	errs "github.com/zhh2001/p4runtime-go-controller/errors"
)

// getPipelineError keeps the RPC status separate from its operation context.
type getPipelineError struct {
	cause     error
	rpcStatus *status.Status
}

func (e *getPipelineError) Error() string {
	return fmt.Sprintf("GetForwardingPipelineConfig: %v", e.cause)
}

func (e *getPipelineError) Unwrap() error { return e.cause }

func (e *getPipelineError) GRPCStatus() *status.Status { return e.rpcStatus }

func (e *getPipelineError) Is(target error) bool {
	if target != errs.ErrPipelineNotSet || e.rpcStatus.Code() != codes.FailedPrecondition {
		return false
	}
	message := strings.TrimSuffix(strings.ToLower(strings.Join(strings.Fields(e.rpcStatus.Message()), " ")), ".")
	for _, missing := range []string{
		"no forwarding pipeline config set", "no pipeline config set",
		"pipeline not set", "pipeline is not set", "pipeline not configured",
	} {
		if message == missing || message == missing+" for this device" || message == missing+" on this device" {
			return true
		}
	}
	return false
}
