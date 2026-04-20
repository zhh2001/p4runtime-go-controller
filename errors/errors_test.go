package errors_test

import (
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"

	errs "github.com/zhh2001/p4runtime-go-controller/errors"
)

func TestSentinelsAreDistinct(t *testing.T) {
	all := []error{
		errs.ErrNotPrimary,
		errs.ErrPipelineNotSet,
		errs.ErrEntryExists,
		errs.ErrEntryNotFound,
		errs.ErrUnsupportedMatchKind,
		errs.ErrTargetUnsupported,
		errs.ErrStreamClosed,
		errs.ErrArbitrationFailed,
		errs.ErrElectionIDZero,
		errs.ErrInvalidBitWidth,
		errs.ErrInvalidMatchField,
		errs.ErrInvalidActionParam,
	}
	for i := range all {
		for j := range all {
			if i == j {
				continue
			}
			assert.Falsef(t, stderrors.Is(all[i], all[j]),
				"sentinel %d must not be Is() sentinel %d", i, j)
		}
	}
}

func TestSentinelsWrapProperly(t *testing.T) {
	wrapped := stderrors.Join(errs.ErrNotPrimary, stderrors.New("context"))
	assert.True(t, stderrors.Is(wrapped, errs.ErrNotPrimary))
	assert.False(t, stderrors.Is(wrapped, errs.ErrPipelineNotSet))
}
