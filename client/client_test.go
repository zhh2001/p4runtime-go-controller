package client_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"github.com/zhh2001/p4runtime-go-controller/client"
	errs "github.com/zhh2001/p4runtime-go-controller/errors"
	"github.com/zhh2001/p4runtime-go-controller/internal/testutil"
)

// dialViaHarness wires the Client through the bufconn dialer of h.
func dialViaHarness(ctx context.Context, h *testutil.ServerHarness, opts ...client.Option) (*client.Client, error) {
	base := []client.Option{
		client.WithDeviceID(1),
		client.WithElectionID(client.ElectionID{Low: 1}),
		client.WithInsecure(),
		client.WithArbitrationTimeout(1500 * time.Millisecond),
		client.WithDialOptions(grpc.WithContextDialer(h.Dialer())),
	}
	all := append(base, opts...)
	return client.Dial(ctx, "passthrough:bufnet", all...)
}

func TestDial_RequiresDeviceID(t *testing.T) {
	_, err := client.Dial(context.Background(), "ignored",
		client.WithElectionID(client.ElectionID{Low: 1}),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "device ID required")
}

func TestDial_RequiresNonZeroElectionID(t *testing.T) {
	_, err := client.Dial(context.Background(), "ignored",
		client.WithDeviceID(1),
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, errs.ErrElectionIDZero)
}

func TestDial_BecomesPrimary(t *testing.T) {
	h := testutil.StartServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	c, err := dialViaHarness(ctx, h)
	require.NoError(t, err)
	defer c.Close()

	require.NoError(t, c.BecomePrimary(ctx))
	assert.Equal(t, client.StatePrimary, c.State())
	assert.True(t, c.IsPrimary())
	assert.EqualValues(t, 1, c.DeviceID())
	assert.Equal(t, client.ElectionID{Low: 1}, c.ElectionID())
	assert.NotNil(t, c.Conn())
	assert.NotNil(t, c.RPC())
}

func TestDial_BackupWhenPrimaryPinned(t *testing.T) {
	h := testutil.StartServer(t)
	h.Mu.Lock()
	h.PrimaryElectionHigh = 0
	h.PrimaryElectionLow = 99 // pin someone else as primary
	h.Mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	c, err := dialViaHarness(ctx, h)
	require.NoError(t, err)
	defer c.Close()

	// Give the supervisor a moment to deliver the event.
	require.Eventually(t, func() bool {
		return c.State() == client.StateBackup
	}, time.Second, 10*time.Millisecond)
	assert.False(t, c.IsPrimary())
}

func TestDial_ArbitrationRejectionMovesToBackup(t *testing.T) {
	h := testutil.StartServer(t)
	h.Mu.Lock()
	h.ArbitrationRejection = &rpcstatus.Status{Code: int32(codes.PermissionDenied), Message: "denied"}
	h.Mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	c, err := dialViaHarness(ctx, h)
	require.NoError(t, err)
	defer c.Close()

	require.Eventually(t, func() bool {
		return c.State() == client.StateBackup
	}, time.Second, 10*time.Millisecond)
}

func TestClient_CloseIsIdempotent(t *testing.T) {
	h := testutil.StartServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	c, err := dialViaHarness(ctx, h)
	require.NoError(t, err)
	assert.NoError(t, c.Close())
	assert.NoError(t, c.Close()) // second call is a no-op
}

func TestBecomePrimary_RespectsContext(t *testing.T) {
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

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer waitCancel()
	err = c.BecomePrimary(waitCtx)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
}

func TestBecomePrimary_ReturnsAfterClose(t *testing.T) {
	h := testutil.StartServer(t)
	h.Mu.Lock()
	h.PrimaryElectionHigh = 0
	h.PrimaryElectionLow = 99
	h.Mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := dialViaHarness(ctx, h)
	require.NoError(t, err)

	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = c.Close()
	}()

	err = c.BecomePrimary(context.Background())
	assert.ErrorIs(t, err, errs.ErrStreamClosed)
}

func TestStateString(t *testing.T) {
	cases := map[client.State]string{
		client.StateDisconnected: "disconnected",
		client.StateConnecting:   "connecting",
		client.StateBackup:       "backup",
		client.StatePrimary:      "primary",
	}
	for s, want := range cases {
		assert.Equal(t, want, s.String())
	}
	assert.Contains(t, client.State(99).String(), "unknown")
}
