package client_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/internal/testutil"
)

func TestStream_AllDispatchers(t *testing.T) {
	h := testutil.StartServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := dialViaHarness(ctx, h)
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.BecomePrimary(ctx))

	var pktCount, digestCount, idleCount, anyCount int64

	offPkt := c.OnPacketIn(func(_ context.Context, _ *p4v1.PacketIn) {
		atomic.AddInt64(&pktCount, 1)
	})
	offDigest := c.OnDigestList(func(_ context.Context, _ *p4v1.DigestList) {
		atomic.AddInt64(&digestCount, 1)
	})
	offIdle := c.OnIdleTimeout(func(_ context.Context, _ *p4v1.IdleTimeoutNotification) {
		atomic.AddInt64(&idleCount, 1)
	})
	offAny := c.OnStreamMessage(func(_ context.Context, _ *p4v1.StreamMessageResponse) {
		atomic.AddInt64(&anyCount, 1)
	})
	defer offPkt()
	defer offDigest()
	defer offIdle()
	defer offAny()

	// Wait for the stream to be ready, then push one of each.
	require.Eventually(t, func() bool {
		return h.PushStreamMessage(&p4v1.StreamMessageResponse{
			Update: &p4v1.StreamMessageResponse_Packet{Packet: &p4v1.PacketIn{Payload: []byte{0x00}}},
		}) == nil
	}, time.Second, 20*time.Millisecond)
	require.NoError(t, h.PushStreamMessage(&p4v1.StreamMessageResponse{
		Update: &p4v1.StreamMessageResponse_Digest{Digest: &p4v1.DigestList{DigestId: 1}},
	}))
	require.NoError(t, h.PushStreamMessage(&p4v1.StreamMessageResponse{
		Update: &p4v1.StreamMessageResponse_IdleTimeoutNotification{IdleTimeoutNotification: &p4v1.IdleTimeoutNotification{}},
	}))

	require.Eventually(t, func() bool {
		return atomic.LoadInt64(&pktCount) == 1 &&
			atomic.LoadInt64(&digestCount) == 1 &&
			atomic.LoadInt64(&idleCount) == 1 &&
			atomic.LoadInt64(&anyCount) == 3
	}, time.Second, 20*time.Millisecond)

	// Cancel handlers; further events must not fire.
	offPkt()
	offDigest()
	offIdle()
	offAny()
	require.NoError(t, h.PushStreamMessage(&p4v1.StreamMessageResponse{
		Update: &p4v1.StreamMessageResponse_Packet{Packet: &p4v1.PacketIn{Payload: []byte{0x01}}},
	}))
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int64(1), atomic.LoadInt64(&pktCount))
	assert.Equal(t, int64(3), atomic.LoadInt64(&anyCount))
}

func TestStream_SendAckAndNilGuards(t *testing.T) {
	h := testutil.StartServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := dialViaHarness(ctx, h)
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.BecomePrimary(ctx))

	require.NoError(t, c.SendDigestAck(ctx, &p4v1.DigestListAck{DigestId: 7, ListId: 11}))
	require.Eventually(t, func() bool {
		h.Mu.Lock()
		defer h.Mu.Unlock()
		return len(h.ReceivedDigestAcks) == 1
	}, time.Second, 20*time.Millisecond)

	assert.Error(t, c.SendDigestAck(ctx, nil))
	assert.Error(t, c.SendPacketOut(ctx, nil))
}

func TestStream_HandlerCanCloseClient(t *testing.T) {
	h := testutil.StartServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := dialViaHarness(ctx, h)
	require.NoError(t, err)
	defer c.Close()

	closed := make(chan error, 1)
	c.OnPacketIn(func(_ context.Context, _ *p4v1.PacketIn) {
		closed <- c.Close()
	})
	require.Eventually(t, func() bool {
		return h.PushStreamMessage(&p4v1.StreamMessageResponse{
			Update: &p4v1.StreamMessageResponse_Packet{Packet: &p4v1.PacketIn{}},
		}) == nil
	}, time.Second, 10*time.Millisecond)
	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("Close blocked inside the packet handler")
	}
	for range c.Events() {
	}
}

func TestStream_HandlersCanReplaceSubscriptions(t *testing.T) {
	cases := []struct {
		name      string
		message   *p4v1.StreamMessageResponse
		subscribe func(*client.Client, func()) func()
	}{
		{
			name:    "packet",
			message: &p4v1.StreamMessageResponse{Update: &p4v1.StreamMessageResponse_Packet{Packet: &p4v1.PacketIn{}}},
			subscribe: func(c *client.Client, h func()) func() {
				return c.OnPacketIn(func(context.Context, *p4v1.PacketIn) { h() })
			},
		},
		{
			name:    "digest",
			message: &p4v1.StreamMessageResponse{Update: &p4v1.StreamMessageResponse_Digest{Digest: &p4v1.DigestList{}}},
			subscribe: func(c *client.Client, h func()) func() {
				return c.OnDigestList(func(context.Context, *p4v1.DigestList) { h() })
			},
		},
		{
			name:    "idle",
			message: &p4v1.StreamMessageResponse{Update: &p4v1.StreamMessageResponse_IdleTimeoutNotification{IdleTimeoutNotification: &p4v1.IdleTimeoutNotification{}}},
			subscribe: func(c *client.Client, h func()) func() {
				return c.OnIdleTimeout(func(context.Context, *p4v1.IdleTimeoutNotification) { h() })
			},
		},
		{
			name:    "stream",
			message: &p4v1.StreamMessageResponse{Update: &p4v1.StreamMessageResponse_Error{Error: &p4v1.StreamError{}}},
			subscribe: func(c *client.Client, h func()) func() {
				return c.OnStreamMessage(func(context.Context, *p4v1.StreamMessageResponse) { h() })
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := testutil.StartServer(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			c, err := dialViaHarness(ctx, h)
			require.NoError(t, err)
			defer c.Close()
			require.NoError(t, c.BecomePrimary(ctx))

			var originalCount, replacementCount atomic.Int64
			registered, replaced, received := make(chan struct{}), make(chan struct{}, 1), make(chan struct{}, 1)
			var off, offReplacement func()
			off = tc.subscribe(c, func() {
				<-registered
				originalCount.Add(1)
				off()
				off()
				offReplacement = tc.subscribe(c, func() {
					replacementCount.Add(1)
					received <- struct{}{}
				})
				replaced <- struct{}{}
			})
			close(registered)
			require.Eventually(t, func() bool { return h.PushStreamMessage(tc.message) == nil }, time.Second, 10*time.Millisecond)
			select {
			case <-replaced:
			case <-ctx.Done():
				t.Fatal("handler could not replace its subscription")
			}
			require.Zero(t, replacementCount.Load())
			require.NoError(t, h.PushStreamMessage(tc.message))
			select {
			case <-received:
			case <-ctx.Done():
				t.Fatal("stream stopped receiving after the subscription changed")
			}
			require.Equal(t, int64(1), originalCount.Load())
			require.Equal(t, int64(1), replacementCount.Load())
			offReplacement()
		})
	}
}
