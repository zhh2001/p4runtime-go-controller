package meter_test

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
	"github.com/zhh2001/p4runtime-go-controller/internal/testutil"
	"github.com/zhh2001/p4runtime-go-controller/meter"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
)

func meterPipeline(t *testing.T) *pipeline.Pipeline {
	t.Helper()
	info := &p4configv1.P4Info{
		Meters: []*p4configv1.Meter{{
			Preamble: &p4configv1.Preamble{Id: 0xF0, Name: "ingress.shaper"},
			Spec:     &p4configv1.MeterSpec{Unit: p4configv1.MeterSpec_BYTES},
			Size:     4,
		}},
	}
	p, err := pipeline.New(info, nil)
	require.NoError(t, err)
	return p
}

func TestMeter_ReadAndWrite(t *testing.T) {
	h := testutil.StartServer(t)
	h.Mu.Lock()
	h.OverrideReadResp = []*p4v1.ReadResponse{{
		Entities: []*p4v1.Entity{{
			Entity: &p4v1.Entity_MeterEntry{MeterEntry: &p4v1.MeterEntry{
				MeterId: 0xF0,
				Index:   &p4v1.Index{Index: 2},
				Config:  &p4v1.MeterConfig{Cir: 1000, Cburst: 2000, Pir: 1500, Pburst: 3000},
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

	r, err := meter.NewReader(c, meterPipeline(t))
	require.NoError(t, err)

	entries, err := r.Read(ctx, "ingress.shaper", 2)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.EqualValues(t, 1000, entries[0].GetConfig().Cir)

	require.NoError(t, r.Write(ctx, "ingress.shaper", 2, meter.Config{CIR: 10, CBurst: 20, PIR: 30, PBurst: 40}))

	_, err = r.Read(ctx, "nope", 0)
	assert.Error(t, err)
	assert.Error(t, r.Write(ctx, "nope", 0, meter.Config{}))
}
