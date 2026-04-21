package pre_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"google.golang.org/grpc"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/internal/testutil"
	"github.com/zhh2001/p4runtime-go-controller/pre"
)

func dial(t *testing.T, h *testutil.ServerHarness) *client.Client {
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

func TestNewWriter_NilClient(t *testing.T) {
	_, err := pre.NewWriter(nil)
	assert.Error(t, err)
}

func TestMulticastGroup_Write(t *testing.T) {
	h := testutil.StartServer(t)
	c := dial(t, h)
	defer c.Close()

	w, err := pre.NewWriter(c)
	require.NoError(t, err)

	mg := pre.MulticastGroup{
		ID: 1,
		Replicas: []pre.Replica{
			{EgressPort: 1, Instance: 0},
			{EgressPort: 2, Instance: 0},
			{EgressPort: 3, Instance: 1},
		},
	}
	ctx := context.Background()
	require.NoError(t, w.InsertMulticastGroup(ctx, mg))

	h.Mu.Lock()
	require.Len(t, h.WriteRequests, 1)
	req := h.WriteRequests[0]
	h.Mu.Unlock()

	require.Len(t, req.Updates, 1)
	assert.Equal(t, p4v1.Update_INSERT, req.Updates[0].Type)

	pre_entry := req.Updates[0].GetEntity().GetPacketReplicationEngineEntry().GetMulticastGroupEntry()
	require.NotNil(t, pre_entry)
	assert.EqualValues(t, 1, pre_entry.GetMulticastGroupId())
	require.Len(t, pre_entry.GetReplicas(), 3)
	assert.EqualValues(t, 1, pre_entry.GetReplicas()[0].GetEgressPort())
	assert.EqualValues(t, 2, pre_entry.GetReplicas()[1].GetEgressPort())
	assert.EqualValues(t, 3, pre_entry.GetReplicas()[2].GetEgressPort())
	assert.EqualValues(t, 1, pre_entry.GetReplicas()[2].GetInstance())
}

func TestMulticastGroup_Modify(t *testing.T) {
	h := testutil.StartServer(t)
	c := dial(t, h)
	defer c.Close()
	w, _ := pre.NewWriter(c)

	require.NoError(t, w.ModifyMulticastGroup(context.Background(),
		pre.MulticastGroup{
			ID:       5,
			Replicas: []pre.Replica{{EgressPort: 1}},
		}))

	h.Mu.Lock()
	defer h.Mu.Unlock()
	require.Len(t, h.WriteRequests, 1)
	assert.Equal(t, p4v1.Update_MODIFY, h.WriteRequests[0].Updates[0].Type)
}

func TestMulticastGroup_Delete(t *testing.T) {
	h := testutil.StartServer(t)
	c := dial(t, h)
	defer c.Close()
	w, _ := pre.NewWriter(c)

	require.NoError(t, w.DeleteMulticastGroup(context.Background(), 3))

	h.Mu.Lock()
	defer h.Mu.Unlock()
	require.Len(t, h.WriteRequests, 1)
	assert.Equal(t, p4v1.Update_DELETE, h.WriteRequests[0].Updates[0].Type)

	mge := h.WriteRequests[0].Updates[0].GetEntity().
		GetPacketReplicationEngineEntry().GetMulticastGroupEntry()
	require.NotNil(t, mge)
	assert.EqualValues(t, 3, mge.GetMulticastGroupId())
}

func TestMulticastGroup_Read(t *testing.T) {
	h := testutil.StartServer(t)
	h.Mu.Lock()
	h.OverrideReadResp = []*p4v1.ReadResponse{{
		Entities: []*p4v1.Entity{{
			Entity: &p4v1.Entity_PacketReplicationEngineEntry{
				PacketReplicationEngineEntry: &p4v1.PacketReplicationEngineEntry{
					Type: &p4v1.PacketReplicationEngineEntry_MulticastGroupEntry{
						MulticastGroupEntry: &p4v1.MulticastGroupEntry{
							MulticastGroupId: 7,
							Replicas: []*p4v1.Replica{
								{PortKind: &p4v1.Replica_EgressPort{EgressPort: 1}},
								{PortKind: &p4v1.Replica_EgressPort{EgressPort: 2}, Instance: 1},
							},
							Metadata: []byte("cookie"),
						},
					},
				},
			},
		}},
	}}
	h.Mu.Unlock()

	c := dial(t, h)
	defer c.Close()
	w, _ := pre.NewWriter(c)

	got, err := w.ReadMulticastGroups(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.EqualValues(t, 7, got[0].ID)
	require.Len(t, got[0].Replicas, 2)
	assert.EqualValues(t, 2, got[0].Replicas[1].EgressPort)
	assert.EqualValues(t, 1, got[0].Replicas[1].Instance)
	assert.Equal(t, []byte("cookie"), got[0].Metadata)
}

func TestCloneSession_WriteAndRead(t *testing.T) {
	h := testutil.StartServer(t)
	c := dial(t, h)
	defer c.Close()
	w, _ := pre.NewWriter(c)

	cs := pre.CloneSession{
		ID:                100,
		Replicas:          []pre.Replica{{EgressPort: 5}},
		ClassOfService:    2,
		PacketLengthBytes: 128,
	}
	require.NoError(t, w.InsertCloneSession(context.Background(), cs))

	h.Mu.Lock()
	require.Len(t, h.WriteRequests, 1)
	cse := h.WriteRequests[0].Updates[0].GetEntity().
		GetPacketReplicationEngineEntry().GetCloneSessionEntry()
	h.Mu.Unlock()
	require.NotNil(t, cse)
	assert.EqualValues(t, 100, cse.GetSessionId())
	assert.EqualValues(t, 2, cse.GetClassOfService())
	assert.EqualValues(t, 128, cse.GetPacketLengthBytes())
	require.Len(t, cse.GetReplicas(), 1)
	assert.EqualValues(t, 5, cse.GetReplicas()[0].GetEgressPort())

	require.NoError(t, w.ModifyCloneSession(context.Background(), cs))
	require.NoError(t, w.DeleteCloneSession(context.Background(), 100))
}

func TestCloneSession_Read(t *testing.T) {
	h := testutil.StartServer(t)
	h.Mu.Lock()
	h.OverrideReadResp = []*p4v1.ReadResponse{{
		Entities: []*p4v1.Entity{{
			Entity: &p4v1.Entity_PacketReplicationEngineEntry{
				PacketReplicationEngineEntry: &p4v1.PacketReplicationEngineEntry{
					Type: &p4v1.PacketReplicationEngineEntry_CloneSessionEntry{
						CloneSessionEntry: &p4v1.CloneSessionEntry{
							SessionId: 100,
							Replicas:  []*p4v1.Replica{{PortKind: &p4v1.Replica_EgressPort{EgressPort: 5}}},
						},
					},
				},
			},
		}},
	}}
	h.Mu.Unlock()

	c := dial(t, h)
	defer c.Close()
	w, _ := pre.NewWriter(c)

	got, err := w.ReadCloneSessions(context.Background(), 100)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.EqualValues(t, 100, got[0].ID)
	require.Len(t, got[0].Replicas, 1)
	assert.EqualValues(t, 5, got[0].Replicas[0].EgressPort)
}

func TestValidation(t *testing.T) {
	h := testutil.StartServer(t)
	c := dial(t, h)
	defer c.Close()
	w, _ := pre.NewWriter(c)
	ctx := context.Background()

	// Zero ID
	assert.Error(t, w.InsertMulticastGroup(ctx, pre.MulticastGroup{ID: 0, Replicas: []pre.Replica{{EgressPort: 1}}}))
	assert.Error(t, w.InsertCloneSession(ctx, pre.CloneSession{ID: 0, Replicas: []pre.Replica{{EgressPort: 1}}}))
	assert.Error(t, w.DeleteMulticastGroup(ctx, 0))
	assert.Error(t, w.DeleteCloneSession(ctx, 0))

	// Empty replicas
	assert.Error(t, w.InsertMulticastGroup(ctx, pre.MulticastGroup{ID: 1}))
	assert.Error(t, w.InsertCloneSession(ctx, pre.CloneSession{ID: 1}))

	// Zero egress port
	assert.Error(t, w.InsertMulticastGroup(ctx, pre.MulticastGroup{
		ID: 1, Replicas: []pre.Replica{{EgressPort: 0}},
	}))
}
