package counter_test

import (
	"context"
	"math"
	"testing"
	"time"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/zhh2001/p4runtime-go-controller/v2/client"
	"github.com/zhh2001/p4runtime-go-controller/v2/counter"
	errs "github.com/zhh2001/p4runtime-go-controller/v2/errors"
	"github.com/zhh2001/p4runtime-go-controller/v2/internal/testutil"
	"github.com/zhh2001/p4runtime-go-controller/v2/pipeline"
)

func TestCounter_WireValuesAndUnits(t *testing.T) {
	for _, unit := range []p4configv1.CounterSpec_Unit{p4configv1.CounterSpec_PACKETS, p4configv1.CounterSpec_BYTES, p4configv1.CounterSpec_BOTH} {
		t.Run(unit.String(), func(t *testing.T) {
			h := testutil.StartServer(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			c, err := client.Dial(ctx, "passthrough:bufnet", client.WithDeviceID(1), client.WithElectionID(client.ElectionID{Low: 1}), client.WithInsecure(), client.WithDialOptions(grpc.WithContextDialer(h.Dialer())))
			require.NoError(t, err)
			defer c.Close()
			p, err := pipeline.New(&p4configv1.P4Info{Counters: []*p4configv1.Counter{{
				Preamble: &p4configv1.Preamble{Id: 0x12000001, Name: "ingress.stats", Alias: "stats"},
				Spec:     &p4configv1.CounterSpec{Unit: unit},
				Size:     4,
			}}}, nil)
			require.NoError(t, err)
			r, err := counter.NewReader(c, p)
			require.NoError(t, err)
			for _, tc := range []struct {
				name           string
				packets, bytes int64
			}{
				{"zero", 0, 0},
				{"packets", 1, 0},
				{"bytes", 0, 60},
				{"both", 12, 720},
				{"above uint32", int64(math.MaxUint32) + 1, int64(math.MaxUint32) + 2},
				{"maximum int64", math.MaxInt64, math.MaxInt64 - 1},
				{"negative wire values", -1, -2},
				{"minimum int64", math.MinInt64, math.MinInt64 + 1},
			} {
				t.Run(tc.name, func(t *testing.T) {
					require.NoError(t, r.Write(ctx, "stats", 0, tc.packets, tc.bytes))
					h.Mu.Lock()
					requests := append([]*p4v1.WriteRequest(nil), h.WriteRequests...)
					h.WriteRequests = nil
					// Read is independent of Write. Even an unused field must be
					// preserved if the target includes it in the response.
					h.OverrideReadResp = []*p4v1.ReadResponse{{Entities: []*p4v1.Entity{{Entity: &p4v1.Entity_CounterEntry{CounterEntry: &p4v1.CounterEntry{
						CounterId: 0x12000001,
						Index:     &p4v1.Index{Index: 3},
						Data:      &p4v1.CounterData{PacketCount: tc.bytes, ByteCount: tc.packets},
					}}}}}}
					h.Mu.Unlock()
					require.Len(t, requests, 1)
					require.Len(t, requests[0].Updates, 1)
					update := requests[0].Updates[0]
					require.Equal(t, p4v1.Update_MODIFY, update.Type)
					entry := update.Entity.GetCounterEntry()
					require.NotNil(t, entry)
					require.EqualValues(t, 0x12000001, entry.CounterId)
					require.NotNil(t, entry.Index)
					require.Zero(t, entry.Index.Index)
					require.NotNil(t, entry.Data, "zero counts must retain message presence")
					require.Equal(t, tc.packets, entry.Data.PacketCount)
					require.Equal(t, tc.bytes, entry.Data.ByteCount)
					got, err := r.Read(ctx, "stats", 3)
					require.NoError(t, err)
					require.Equal(t, []*counter.Data{{Name: "ingress.stats", ID: 0x12000001, Index: 3, Packets: tc.bytes, Bytes: tc.packets}}, got)
				})
			}
		})
	}
}

func TestCounter_TargetUnsupported(t *testing.T) {
	h := testutil.StartServer(t)
	h.Mu.Lock()
	h.OverrideWriteErr = status.Error(codes.Unimplemented, "counter writes unavailable")
	h.Mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, "passthrough:bufnet", client.WithDeviceID(1), client.WithElectionID(client.ElectionID{Low: 1}), client.WithInsecure(), client.WithDialOptions(grpc.WithContextDialer(h.Dialer())))
	require.NoError(t, err)
	defer c.Close()
	r, err := counter.NewReader(c, counterPipeline(t))
	require.NoError(t, err)
	err = r.Write(ctx, "ingress.pkt_cnt", 0, 0, 0)
	require.ErrorIs(t, err, errs.ErrTargetUnsupported)
	require.Equal(t, codes.Unimplemented, status.Code(err))
	h.Mu.Lock()
	requests := len(h.WriteRequests)
	h.Mu.Unlock()
	require.Equal(t, 1, requests, "unsupported writes must not be retried")
}
