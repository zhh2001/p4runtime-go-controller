// Package errors exposes the sentinel errors returned by the
// p4runtime-go-controller SDK.
//
// Callers should use errors.Is or errors.As (from the standard library) to
// test for specific failure modes. The concrete error values returned by the
// SDK wrap the underlying gRPC status where applicable, so
// google.golang.org/grpc/status.FromError continues to work.
package errors

import "errors"

// Sentinel errors used across the SDK. Handlers should match with errors.Is.
var (
	// ErrNotPrimary is returned when an operation requires primary mastership
	// and the client has not (yet) been elected primary for the target
	// device.
	ErrNotPrimary = errors.New("not primary controller for device")

	// ErrPipelineNotSet is returned when an operation requires an active
	// forwarding pipeline and none has been configured on the target.
	ErrPipelineNotSet = errors.New("pipeline not set on target")

	// ErrEntryExists is returned by Insert when the table entry already
	// exists. Callers that need idempotent behavior should convert to
	// Modify.
	ErrEntryExists = errors.New("table entry already exists")

	// ErrEntryNotFound is returned by Modify or Delete when the target
	// entry does not exist.
	ErrEntryNotFound = errors.New("table entry not found")

	// ErrUnsupportedMatchKind is returned when the P4 pipeline does not
	// declare the match kind being used (for example, attempting a LPM
	// match on an EXACT-only field).
	ErrUnsupportedMatchKind = errors.New("match kind not supported by pipeline")

	// ErrTargetUnsupported is returned when the target does not implement
	// a feature the caller requested (for example, VERIFY_AND_COMMIT
	// pipeline installation).
	ErrTargetUnsupported = errors.New("feature not supported by target")

	// ErrStreamClosed is returned when the StreamChannel was closed either
	// by the target or by Client.Close.
	ErrStreamClosed = errors.New("stream channel closed")

	// ErrArbitrationFailed is returned when the initial arbitration
	// exchange does not complete within the supervisor's deadline or when
	// the target rejects the election ID.
	ErrArbitrationFailed = errors.New("arbitration failed")

	// ErrElectionIDZero is returned when the caller supplies a zero
	// election ID (high=0, low=0). The P4Runtime spec reserves this value
	// for "no election participation".
	ErrElectionIDZero = errors.New("election ID must be non-zero")

	// ErrInvalidBitWidth is returned by codec helpers when the supplied
	// value does not fit into the declared bit width.
	ErrInvalidBitWidth = errors.New("value exceeds declared bit width")

	// ErrInvalidMatchField is returned when a builder is asked to match a
	// field that is not declared on the table.
	ErrInvalidMatchField = errors.New("match field not declared on table")

	// ErrInvalidActionParam is returned when a builder is asked to set an
	// action parameter that is not declared on the action.
	ErrInvalidActionParam = errors.New("action parameter not declared on action")
)
