package register_test

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
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
	"github.com/zhh2001/p4runtime-go-controller/register"
)

func registerPipeline(t *testing.T) *pipeline.Pipeline {
	t.Helper()
	info := &p4configv1.P4Info{
		Registers: []*p4configv1.Register{{
			Preamble: &p4configv1.Preamble{Id: 0xD0, Name: "ingress.counter_reg"},
			Size:     8,
		}},
	}
	p, err := pipeline.New(info, nil)
	require.NoError(t, err)
	return p
}

func TestRegister_ReadAndWrite(t *testing.T) {
	h := testutil.StartServer(t)
	h.Mu.Lock()
	h.OverrideReadResp = []*p4v1.ReadResponse{{
		Entities: []*p4v1.Entity{{
			Entity: &p4v1.Entity_RegisterEntry{RegisterEntry: &p4v1.RegisterEntry{
				RegisterId: 0xD0,
				Index:      &p4v1.Index{Index: 1},
				Data: &p4v1.P4Data{
					Data: &p4v1.P4Data_Bitstring{Bitstring: []byte{0xab}},
				},
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

	r, err := register.NewReader(c, registerPipeline(t))
	require.NoError(t, err)

	entries, err := r.Read(ctx, "ingress.counter_reg", 1)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, []byte{0xab}, entries[0].GetData().GetBitstring())

	require.NoError(t, r.Write(ctx, "ingress.counter_reg", 1, []byte{0xcd}))

	_, err = r.Read(ctx, "nope", 0)
	assert.Error(t, err)
	assert.Error(t, r.Write(ctx, "nope", 0, []byte{0x01}))
}
