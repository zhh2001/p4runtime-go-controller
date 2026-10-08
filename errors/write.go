package errors

import (
	"fmt"
	"strings"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// WriteError preserves a failed Write RPC and its per-update results.
// Use errors.As to inspect Updates and errors.Is to match any failed update.
type WriteError struct {
	// Cause is the original RPC error.
	Cause error
	// Updates contains one p4.Error per request update, in request order.
	// Successful updates have canonical_code OK. A nil slice means the
	// response did not contain a valid, complete set of per-update results.
	Updates []*p4v1.Error
}

// Error includes the RPC status and the index and message of each failed update.
func (e *WriteError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "write: %v", e.Cause)
	for i, update := range e.Updates {
		if update.GetCanonicalCode() != int32(codes.OK) {
			fmt.Fprintf(&b, ", update %d: %s: %s", i, codes.Code(update.GetCanonicalCode()), update.GetMessage())
		}
	}
	return b.String()
}

// Unwrap returns the original RPC error.
func (e *WriteError) Unwrap() error { return e.Cause }

// GRPCStatus returns the original status, including its message and details.
func (e *WriteError) GRPCStatus() *status.Status { return status.Convert(e.Cause) }

// Is matches a known sentinel for the RPC or any failed update. A match does
// not imply that every update failed. Inspect Updates before retrying a batch.
func (e *WriteError) Is(target error) bool {
	st := e.GRPCStatus()
	if sentinel := writeSentinel(st.Code(), st.Message(), true); sentinel != nil && sentinel == target {
		return true
	}
	for _, update := range e.Updates {
		if sentinel := writeSentinel(codes.Code(update.GetCanonicalCode()), update.GetMessage(), false); sentinel != nil && sentinel == target {
			return true
		}
	}
	return false
}

func writeSentinel(code codes.Code, message string, rpc bool) error {
	switch code {
	case codes.AlreadyExists:
		return ErrEntryExists
	case codes.NotFound:
		return ErrEntryNotFound
	case codes.Unimplemented:
		return ErrTargetUnsupported
	case codes.PermissionDenied, codes.FailedPrecondition:
		if !rpc {
			return nil
		}
		message = strings.ToLower(message)
		if strings.Contains(message, "not primary") || strings.Contains(message, "not the primary") || strings.Contains(message, "no longer primary") {
			return ErrNotPrimary
		}
		if code == codes.FailedPrecondition && (strings.Contains(message, "no forwarding pipeline config set") ||
			strings.Contains(message, "no pipeline config set") || strings.Contains(message, "pipeline not set") ||
			strings.Contains(message, "pipeline is not set") || strings.Contains(message, "pipeline not configured")) {
			return ErrPipelineNotSet
		}
	}
	return nil
}
