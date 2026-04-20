package digest_test

import (
	"context"
	"sync"
	"testing"
	"time"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/digest"
	"github.com/zhh2001/p4runtime-go-controller/internal/testutil"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
)

func digestPipeline(t *testing.T) *pipeline.Pipeline {
	t.Helper()
	info := &p4configv1.P4Info{
		Digests: []*p4configv1.Digest{
			{Preamble: &p4configv1.Preamble{Id: 0x100, Name: "ingress.mac_learn"}},
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
	sub.OnDigest("ingress.mac_learn", func(ctx context.Context, msg *p4v1.DigestList) {
		mu.Lock()
		defer mu.Unlock()
		if got == nil {
			got = msg
			_ = sub.Ack(ctx, msg)
			close(done)
		}
	})

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

	// Send two — one matching, one with a different ID — and verify only one hits.
	require.Eventually(t, func() bool {
		return h.PushStreamMessage(&p4v1.StreamMessageResponse{
			Update: &p4v1.StreamMessageResponse_Digest{Digest: &p4v1.DigestList{DigestId: 0x100}},
		}) == nil
	}, time.Second, 20*time.Millisecond)
	require.NoError(t, h.PushStreamMessage(&p4v1.StreamMessageResponse{
		Update: &p4v1.StreamMessageResponse_Digest{Digest: &p4v1.DigestList{DigestId: 0x999}},
	}))

	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	assert.Equal(t, 1, count)
	mu.Unlock()
}
