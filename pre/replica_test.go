package pre_test

import (
	"context"
	"fmt"
	"testing"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/zhh2001/p4runtime-go-controller/internal/testutil"
	"github.com/zhh2001/p4runtime-go-controller/pre"
)

func TestReadReplicas_BytePort(t *testing.T) {
	for _, multicast := range []bool{true, false} {
		name := "clone"
		if multicast {
			name = "multicast"
		}
		t.Run(name, func(t *testing.T) {
			h := testutil.StartServer(t)
			replica := &p4v1.Replica{PortKind: &p4v1.Replica_Port{Port: []byte{7}}, Instance: 3}
			entry := &p4v1.PacketReplicationEngineEntry{}
			if multicast {
				entry.Type = &p4v1.PacketReplicationEngineEntry_MulticastGroupEntry{
					MulticastGroupEntry: &p4v1.MulticastGroupEntry{MulticastGroupId: 7, Replicas: []*p4v1.Replica{replica}},
				}
			} else {
				entry.Type = &p4v1.PacketReplicationEngineEntry_CloneSessionEntry{
					CloneSessionEntry: &p4v1.CloneSessionEntry{SessionId: 7, Replicas: []*p4v1.Replica{replica}},
				}
			}
			h.Mu.Lock()
			h.OverrideReadResp = []*p4v1.ReadResponse{{Entities: []*p4v1.Entity{{Entity: &p4v1.Entity_PacketReplicationEngineEntry{PacketReplicationEngineEntry: entry}}}}}
			h.Mu.Unlock()
			c := dial(t, h)
			defer c.Close()
			writer, err := pre.NewWriter(c)
			require.NoError(t, err)
			var replicas []pre.Replica
			if multicast {
				groups, err := writer.ReadMulticastGroups(context.Background(), 7)
				require.NoError(t, err)
				require.Len(t, groups, 1)
				replicas = groups[0].Replicas
			} else {
				sessions, err := writer.ReadCloneSessions(context.Background(), 7)
				require.NoError(t, err)
				require.Len(t, sessions, 1)
				replicas = sessions[0].Replicas
			}
			require.Len(t, replicas, 1)
			assert.Zero(t, replicas[0].EgressPort)
			assert.Equal(t, []byte{7}, replicas[0].Port)
			assert.EqualValues(t, 3, replicas[0].Instance)
		})
	}
}

func preEntity(multicast bool, id uint32, replicas []*p4v1.Replica) *p4v1.Entity {
	entry := &p4v1.PacketReplicationEngineEntry{}
	if multicast {
		entry.Type = &p4v1.PacketReplicationEngineEntry_MulticastGroupEntry{MulticastGroupEntry: &p4v1.MulticastGroupEntry{
			MulticastGroupId: id, Replicas: replicas, Metadata: []byte("cookie"),
		}}
	} else {
		entry.Type = &p4v1.PacketReplicationEngineEntry_CloneSessionEntry{CloneSessionEntry: &p4v1.CloneSessionEntry{
			SessionId: id, Replicas: replicas, ClassOfService: 2, PacketLengthBytes: 128,
		}}
	}
	return &p4v1.Entity{Entity: &p4v1.Entity_PacketReplicationEngineEntry{PacketReplicationEngineEntry: entry}}
}

func TestPRE_ReplicaRoundTrip(t *testing.T) {
	replicas := []*p4v1.Replica{
		{PortKind: &p4v1.Replica_EgressPort{EgressPort: 7}, Instance: 1, //nolint:staticcheck // Keep the P4Runtime 1.3 wire representation.
			BackupReplicas: []*p4v1.BackupReplica{{Port: []byte{10}, Instance: 1}}},
		{PortKind: &p4v1.Replica_EgressPort{EgressPort: ^uint32(0)}}, //nolint:staticcheck // Keep all bits of the legacy port.
		{PortKind: &p4v1.Replica_Port{Port: []byte{0, 7}}, Instance: 2},
		{PortKind: &p4v1.Replica_Port{Port: []byte("xe-0/0/7")}, Instance: 3},
		{PortKind: &p4v1.Replica_Port{Port: []byte{1, 0, 0, 0, 0, 0, 0, 0, 0}}, Instance: ^uint32(0)},
		{PortKind: &p4v1.Replica_Port{Port: []byte{0}}, Instance: 4},
		{PortKind: &p4v1.Replica_Port{Port: []byte{'a', 0, 'b'}}, Instance: 5,
			BackupReplicas: []*p4v1.BackupReplica{{Port: []byte{0, 8}, Instance: 6}, {Port: []byte("xe-0/0/9"), Instance: 7}}},
	}
	for _, multicast := range []bool{true, false} {
		t.Run(fmt.Sprint(multicast), func(t *testing.T) {
			h := testutil.StartServer(t)
			want := preEntity(multicast, 7, replicas)
			h.Mu.Lock()
			h.OverrideReadResp = []*p4v1.ReadResponse{{Entities: []*p4v1.Entity{preEntity(!multicast, 9, nil)}}, {Entities: []*p4v1.Entity{want}}}
			h.Mu.Unlock()
			c := dial(t, h)
			defer c.Close()
			writer, err := pre.NewWriter(c)
			require.NoError(t, err)
			var got []pre.Replica
			if multicast {
				groups, err := writer.ReadMulticastGroups(context.Background(), 0)
				require.NoError(t, err)
				require.Len(t, groups, 1)
				got = groups[0].Replicas
				require.NoError(t, writer.ModifyMulticastGroup(context.Background(), groups[0]))
			} else {
				sessions, err := writer.ReadCloneSessions(context.Background(), 0)
				require.NoError(t, err)
				require.Len(t, sessions, 1)
				got = sessions[0].Replicas
				require.NoError(t, writer.ModifyCloneSession(context.Background(), sessions[0]))
			}
			require.Len(t, got, len(replicas))
			assert.EqualValues(t, 7, got[0].EgressPort)
			assert.Nil(t, got[0].Port)
			assert.Equal(t, []byte{0, 7}, got[2].Port)
			assert.Zero(t, got[2].EgressPort)
			require.Len(t, got[6].BackupReplicas, 2)
			assert.Equal(t, []byte{0, 8}, got[6].BackupReplicas[0].Port)
			assert.EqualValues(t, 7, got[6].BackupReplicas[1].Instance)
			h.Mu.Lock()
			defer h.Mu.Unlock()
			require.Len(t, h.WriteRequests, 1)
			require.Len(t, h.WriteRequests[0].Updates, 1)
			update := h.WriteRequests[0].Updates[0]
			assert.Equal(t, p4v1.Update_MODIFY, update.Type)
			assert.True(t, proto.Equal(want, update.Entity), "read/modify changed the entity: %v", update.Entity)
		})
	}
}

func TestPRE_ReplicaWriteValidation(t *testing.T) {
	h := testutil.StartServer(t)
	c := dial(t, h)
	defer c.Close()
	writer, err := pre.NewWriter(c)
	require.NoError(t, err)
	for i, replica := range []pre.Replica{
		{}, {Port: []byte{}}, {EgressPort: 7, Port: []byte{7}}, {EgressPort: 7, Port: []byte{}},
		{Port: []byte{7}, BackupReplicas: []pre.BackupReplica{{}}},
		{EgressPort: 7, BackupReplicas: []pre.BackupReplica{{Port: []byte{}}}},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			mg := pre.MulticastGroup{ID: 7, Replicas: []pre.Replica{replica}}
			cs := pre.CloneSession{ID: 7, Replicas: []pre.Replica{replica}}
			assert.Error(t, writer.InsertMulticastGroup(context.Background(), mg))
			assert.Error(t, writer.ModifyMulticastGroup(context.Background(), mg))
			assert.Error(t, writer.InsertCloneSession(context.Background(), cs))
			assert.Error(t, writer.ModifyCloneSession(context.Background(), cs))
		})
	}
	h.Mu.Lock()
	defer h.Mu.Unlock()
	assert.Empty(t, h.WriteRequests)
}

func TestPRE_ReplicaReadValidation(t *testing.T) {
	for i, replica := range []*p4v1.Replica{
		nil, {}, {PortKind: &p4v1.Replica_Port{}},
		{PortKind: &p4v1.Replica_EgressPort{}}, //nolint:staticcheck // Invalid legacy zero remains distinct from a byte port.
		{PortKind: &p4v1.Replica_Port{Port: []byte{7}}, BackupReplicas: []*p4v1.BackupReplica{nil}},
		{PortKind: &p4v1.Replica_Port{Port: []byte{7}}, BackupReplicas: []*p4v1.BackupReplica{{}}},
	} {
		for _, multicast := range []bool{true, false} {
			t.Run(fmt.Sprintf("%d/%t", i, multicast), func(t *testing.T) {
				h := testutil.StartServer(t)
				h.Mu.Lock()
				h.OverrideReadResp = []*p4v1.ReadResponse{{Entities: []*p4v1.Entity{
					preEntity(multicast, 1, []*p4v1.Replica{{PortKind: &p4v1.Replica_Port{Port: []byte{1}}}}),
					preEntity(multicast, 7, []*p4v1.Replica{{PortKind: &p4v1.Replica_Port{Port: []byte{1}}}, replica}),
				}}}
				h.Mu.Unlock()
				c := dial(t, h)
				defer c.Close()
				writer, err := pre.NewWriter(c)
				require.NoError(t, err)
				if multicast {
					groups, err := writer.ReadMulticastGroups(context.Background(), 0)
					require.ErrorContains(t, err, "group 7: pre: replica[1]")
					assert.Nil(t, groups)
				} else {
					sessions, err := writer.ReadCloneSessions(context.Background(), 0)
					require.ErrorContains(t, err, "session 7: pre: replica[1]")
					assert.Nil(t, sessions)
				}
			})
		}
	}
}
