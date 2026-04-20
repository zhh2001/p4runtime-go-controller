package counter_test

import (
	"context"
	"testing"
	"time"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/counter"
	"github.com/zhh2001/p4runtime-go-controller/internal/testutil"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
)

func counterPipeline(t *testing.T) *pipeline.Pipeline {
	t.Helper()
	info := &p4configv1.P4Info{
		Counters: []*p4configv1.Counter{
			{
				Preamble: &p4configv1.Preamble{Id: 0xC0, Name: "ingress.pkt_cnt"},
				Spec:     &p4configv1.CounterSpec{Unit: p4configv1.CounterSpec_BOTH},
				Size:     16,
			},
		},
	}
	p, err := pipeline.New(info, nil)
	require.NoError(t, err)
	return p
}

func TestCounter_ReadAndWrite(t *testing.T) {
	h := testutil.StartServer(t)
	h.Mu.Lock()
	h.OverrideReadResp = []*p4v1.ReadResponse{{
		Entities: []*p4v1.Entity{{
			Entity: &p4v1.Entity_CounterEntry{CounterEntry: &p4v1.CounterEntry{
				CounterId: 0xC0,
				Index:     &p4v1.Index{Index: 3},
				Data:      &p4v1.CounterData{PacketCount: 99, ByteCount: 1500},
			}},
		}},
	}}
	h.Mu.Unlock()

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
	defer c.Close()
	require.NoError(t, c.BecomePrimary(ctx))

	r, err := counter.NewReader(c, counterPipeline(t))
	require.NoError(t, err)

	got, err := r.Read(ctx, "ingress.pkt_cnt", 3)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.EqualValues(t, 0xC0, got[0].ID)
	assert.EqualValues(t, 3, got[0].Index)
	assert.EqualValues(t, 99, got[0].Packets)
	assert.EqualValues(t, 1500, got[0].Bytes)

	// Write translates to a Modify on the counter entry.
	require.NoError(t, r.Write(ctx, "ingress.pkt_cnt", 3, 123, 2000))
	h.Mu.Lock()
	require.NotEmpty(t, h.WriteRequests)
	req := h.WriteRequests[len(h.WriteRequests)-1]
	h.Mu.Unlock()
	require.Len(t, req.Updates, 1)
	assert.Equal(t, p4v1.Update_MODIFY, req.Updates[0].Type)
}

func TestCounter_UnknownName(t *testing.T) {
	h := testutil.StartServer(t)
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
	defer c.Close()
	require.NoError(t, c.BecomePrimary(ctx))

	r, err := counter.NewReader(c, counterPipeline(t))
	require.NoError(t, err)

	_, err = r.Read(ctx, "nope", -1)
	assert.Error(t, err)
	assert.Error(t, r.Write(ctx, "nope", 0, 1, 2))
}
