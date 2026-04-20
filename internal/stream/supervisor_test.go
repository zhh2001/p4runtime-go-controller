package stream

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
)

func TestStateString(t *testing.T) {
	cases := map[State]string{
		StateDisconnected: "disconnected",
		StateConnecting:   "connecting",
		StateBackup:       "backup",
		StatePrimary:      "primary",
	}
	for s, want := range cases {
		assert.Equal(t, want, s.String())
	}
	assert.Contains(t, State(42).String(), "unknown")
}

func TestNextBackoff(t *testing.T) {
	assert.Equal(t, 2*time.Second, nextBackoff(1*time.Second, 30*time.Second))
	assert.Equal(t, 30*time.Second, nextBackoff(20*time.Second, 30*time.Second))
	// saturation
	assert.Equal(t, time.Duration(30*time.Second), nextBackoff(1<<62, 30*time.Second))
}

func TestJitter(t *testing.T) {
	d := 1 * time.Second
	low, high := time.Duration(float64(d)*0.8), time.Duration(float64(d)*1.2)
	for i := 0; i < 50; i++ {
		got := jitter(d)
		assert.GreaterOrEqual(t, got, low)
		assert.LessOrEqual(t, got, high)
	}
	assert.Equal(t, time.Duration(0), jitter(0))
}

func TestStatusOK(t *testing.T) {
	assert.True(t, statusOK(nil))
	assert.True(t, statusOK(&rpcstatus.Status{Code: int32(codes.OK)}))
	assert.False(t, statusOK(&rpcstatus.Status{Code: int32(codes.AlreadyExists)}))
}
