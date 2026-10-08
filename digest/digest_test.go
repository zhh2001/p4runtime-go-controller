package digest_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/digest"
	errs "github.com/zhh2001/p4runtime-go-controller/errors"
	"github.com/zhh2001/p4runtime-go-controller/internal/testutil"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
)

func digestPipeline(t *testing.T) *pipeline.Pipeline {
	t.Helper()
	info := &p4configv1.P4Info{
		Digests: []*p4configv1.Digest{
			{Preamble: &p4configv1.Preamble{Id: 0x100, Name: "ingress.mac_learn", Alias: "mac_learn"}},
			{Preamble: &p4configv1.Preamble{Id: 0x101, Name: "ingress.port_event", Alias: "port_event"}},
		},
	}
	p, err := pipeline.New(info, nil)
	require.NoError(t, err)
	return p
}

func dialClient(t *testing.T, h *testutil.ServerHarness) *client.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, "passthrough:bufnet",
		client.WithDeviceID(1),
		client.WithElectionID(client.ElectionID{Low: 1}),
		client.WithInsecure(),
		client.WithArbitrationTimeout(1500*time.Millisecond),
		client.WithDialOptions(grpc.WithContextDialer(h.Dialer())),
	)
	require.NoError(t, err)
	require.NoError(t, c.BecomePrimary(ctx))
	return c
}

func TestDigest_ReceiveAndAck(t *testing.T) {
	h := testutil.StartServer(t)
	c := dialClient(t, h)
	defer c.Close()

	sub, err := digest.NewSubscriber(c, digestPipeline(t))
	require.NoError(t, err)

	var (
		mu  sync.Mutex
		got *p4v1.DigestList
	)
	done := make(chan struct{})
	off, err := sub.Subscribe("ingress.mac_learn", func(ctx context.Context, msg *p4v1.DigestList) {
		mu.Lock()
		defer mu.Unlock()
		if got == nil {
			got = msg
			_ = sub.Ack(ctx, msg)
			close(done)
		}
	})
	require.NoError(t, err)
	defer off()

	// Wait for stream to be live.
	require.Eventually(t, func() bool {
		return h.PushStreamMessage(&p4v1.StreamMessageResponse{
			Update: &p4v1.StreamMessageResponse_Digest{
				Digest: &p4v1.DigestList{DigestId: 0x100, ListId: 42},
			},
		}) == nil
	}, time.Second, 20*time.Millisecond)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("digest timeout")
	}

	require.NotNil(t, got)
	assert.EqualValues(t, 42, got.ListId)

	require.Eventually(t, func() bool {
		h.Mu.Lock()
		defer h.Mu.Unlock()
		return len(h.ReceivedDigestAcks) == 1
	}, time.Second, 20*time.Millisecond)

	h.Mu.Lock()
	ack := h.ReceivedDigestAcks[0]
	h.Mu.Unlock()
	assert.EqualValues(t, 0x100, ack.DigestId)
	assert.EqualValues(t, 42, ack.ListId)
}

func TestDigest_FilterByName(t *testing.T) {
	h := testutil.StartServer(t)
	c := dialClient(t, h)
	defer c.Close()

	sub, err := digest.NewSubscriber(c, digestPipeline(t))
	require.NoError(t, err)

	var count int
	var mu sync.Mutex
	sub.OnDigest("ingress.mac_learn", func(_ context.Context, _ *p4v1.DigestList) {
		mu.Lock()
		count++
		mu.Unlock()
	})

	pushDigest(t, h, &p4v1.DigestList{DigestId: 0x100})
	pushDigest(t, h, &p4v1.DigestList{DigestId: 0x999})
	drainDigests(t, c, h)
	mu.Lock()
	assert.Equal(t, 1, count)
	mu.Unlock()
}

func TestDigest_UnknownNameDoesNotSubscribe(t *testing.T) {
	h := testutil.StartServer(t)
	c := dialClient(t, h)
	defer c.Close()
	sub, err := digest.NewSubscriber(c, digestPipeline(t))
	require.NoError(t, err)

	var count atomic.Int32
	off := sub.OnDigest("typo", func(context.Context, *p4v1.DigestList) {
		count.Add(1)
	})
	require.NotNil(t, off)
	defer off()
	for _, id := range []uint32{0x100, 0x999} {
		pushDigest(t, h, &p4v1.DigestList{DigestId: id, ListId: 1})
	}
	drainDigests(t, c, h)
	assert.Zero(t, count.Load())
}

func TestDigest_AckUnknownID(t *testing.T) {
	h := testutil.StartServer(t)
	c := dialClient(t, h)
	defer c.Close()
	sub, err := digest.NewSubscriber(c, digestPipeline(t))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.ErrorContains(t, sub.Ack(ctx, nil), "nil message")
	for _, id := range []uint32{0, 0x999} {
		require.ErrorContains(t, sub.Ack(ctx, &p4v1.DigestList{DigestId: id, ListId: 42}), "unknown digest ID")
	}
	require.NoError(t, c.CloseGracefully(ctx))
	h.Mu.Lock()
	defer h.Mu.Unlock()
	assert.Empty(t, h.ReceivedDigestAcks)
}

func TestDigest_SubscribeValidation(t *testing.T) {
	h := testutil.StartServer(t)
	c := dialClient(t, h)
	defer c.Close()
	sub, err := digest.NewSubscriber(c, digestPipeline(t))
	require.NoError(t, err)

	for _, tc := range []struct {
		name    string
		handler func(context.Context, *p4v1.DigestList)
		want    string
	}{
		{name: "typo", handler: func(context.Context, *p4v1.DigestList) {}, want: "unknown digest \"typo\""},
		{name: " mac_learn", handler: func(context.Context, *p4v1.DigestList) {}, want: "unknown digest"},
		{name: "ingress.mac_learn", want: "nil handler"},
		{name: "", want: "nil handler"},
	} {
		off, err := sub.Subscribe(tc.name, tc.handler)
		require.ErrorContains(t, err, tc.want)
		require.Nil(t, off)
	}
	p, err := pipeline.New(&p4configv1.P4Info{
		Digests: []*p4configv1.Digest{{Preamble: &p4configv1.Preamble{Name: "zero"}}},
	}, nil)
	require.NoError(t, err)
	zero, err := digest.NewSubscriber(c, p)
	require.NoError(t, err)
	off, err := zero.Subscribe("zero", func(context.Context, *p4v1.DigestList) {})
	require.ErrorContains(t, err, "zero ID")
	require.Nil(t, off)
	require.Error(t, zero.Ack(context.Background(), &p4v1.DigestList{}))

	for _, name := range []string{"", "mac_learn", "typo"} {
		cancel := sub.OnDigest(name, nil)
		require.NotNil(t, cancel)
		cancel()
		cancel()
	}
	pushDigest(t, h, &p4v1.DigestList{DigestId: 0x100})
	drainDigests(t, c, h)
}

func TestDigest_SubscribeNamesAndWildcard(t *testing.T) {
	h := testutil.StartServer(t)
	c := dialClient(t, h)
	defer c.Close()
	sub, err := digest.NewSubscriber(c, digestPipeline(t))
	require.NoError(t, err)

	for _, tc := range []struct {
		name string
		want int32
	}{
		{name: "ingress.mac_learn", want: 1},
		{name: "mac_learn", want: 1},
		{name: "port_event", want: 1},
		{name: "", want: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var count atomic.Int32
			off, err := sub.Subscribe(tc.name, func(context.Context, *p4v1.DigestList) {
				count.Add(1)
			})
			require.NoError(t, err)
			require.NotNil(t, off)
			defer off()
			for _, id := range []uint32{0x100, 0x101, 0x999} {
				pushDigest(t, h, &p4v1.DigestList{DigestId: id})
			}
			drainDigests(t, c, h)
			assert.Equal(t, tc.want, count.Load())
			off()
			off()
			pushDigest(t, h, &p4v1.DigestList{DigestId: 0x100})
			drainDigests(t, c, h)
			assert.Equal(t, tc.want, count.Load())
		})
	}
}

func TestDigest_CancelInHandlerAndAckLater(t *testing.T) {
	h := testutil.StartServer(t)
	c := dialClient(t, h)
	defer c.Close()
	sub, err := digest.NewSubscriber(c, digestPipeline(t))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	received := make(chan *p4v1.DigestList, 2)
	var off func()
	off, err = sub.Subscribe("mac_learn", func(_ context.Context, msg *p4v1.DigestList) {
		off()
		received <- msg
	})
	require.NoError(t, err)
	defer off()
	pushDigest(t, h, &p4v1.DigestList{DigestId: 0x100, ListId: ^uint64(0)})
	var batch *p4v1.DigestList
	select {
	case batch = <-received:
	case <-ctx.Done():
		t.Fatal("digest timeout")
	}
	pushDigest(t, h, &p4v1.DigestList{DigestId: 0x100, ListId: 0})
	drainDigests(t, c, h)
	require.Empty(t, received)
	require.NoError(t, sub.Ack(ctx, batch))
	require.NoError(t, sub.Ack(ctx, &p4v1.DigestList{DigestId: 0x101, ListId: 0}))
	require.NoError(t, c.CloseGracefully(ctx))
	h.Mu.Lock()
	acks := append([]*p4v1.DigestListAck(nil), h.ReceivedDigestAcks...)
	h.Mu.Unlock()
	require.Len(t, acks, 2)
	assert.EqualValues(t, 0x100, acks[0].DigestId)
	assert.Equal(t, ^uint64(0), acks[0].ListId)
	assert.EqualValues(t, 0x101, acks[1].DigestId)
	assert.Zero(t, acks[1].ListId)
	require.ErrorIs(t, sub.Ack(ctx, batch), errs.ErrNotPrimary)
}

func TestDigest_AckCanceledContext(t *testing.T) {
	h := testutil.StartServer(t)
	c := dialClient(t, h)
	defer c.Close()
	sub, err := digest.NewSubscriber(c, digestPipeline(t))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, sub.Ack(ctx, &p4v1.DigestList{DigestId: 0x100}), context.Canceled)
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, c.CloseGracefully(ctx))
	h.Mu.Lock()
	defer h.Mu.Unlock()
	assert.Empty(t, h.ReceivedDigestAcks)
}

func TestDigest_NewSubscriber(t *testing.T) {
	p := digestPipeline(t)
	_, err := digest.NewSubscriber(nil, p)
	require.Error(t, err)
	_, err = digest.NewSubscriber(&client.Client{}, nil)
	require.Error(t, err)
}

func pushDigest(t *testing.T, h *testutil.ServerHarness, msg *p4v1.DigestList) {
	t.Helper()
	require.Eventually(t, func() bool {
		return h.PushStreamMessage(&p4v1.StreamMessageResponse{
			Update: &p4v1.StreamMessageResponse_Digest{Digest: msg},
		}) == nil
	}, time.Second, time.Millisecond)
}

func drainDigests(t *testing.T, c *client.Client, h *testutil.ServerHarness) {
	t.Helper()
	drained := make(chan struct{})
	defer c.OnPacketIn(func(context.Context, *p4v1.PacketIn) { close(drained) })()
	require.Eventually(t, func() bool {
		return h.PushStreamMessage(&p4v1.StreamMessageResponse{
			Update: &p4v1.StreamMessageResponse_Packet{Packet: &p4v1.PacketIn{}},
		}) == nil
	}, time.Second, time.Millisecond)
	select {
	case <-drained:
	case <-time.After(time.Second):
		t.Fatal("stream did not drain")
	}
}
