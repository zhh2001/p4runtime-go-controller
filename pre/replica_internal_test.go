package pre

import (
	"testing"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestReplicas_CopyOwnership(t *testing.T) {
	replicas := []Replica{{Port: []byte{0, 7}, Instance: 1, BackupReplicas: []BackupReplica{{Port: []byte{8}, Instance: 2}, {Port: []byte("eth9"), Instance: 3}}}}
	wire := encodeReplicas(replicas)
	decoded, err := decodeReplicas(wire)
	require.NoError(t, err)
	require.Equal(t, replicas, decoded)
	replicas[0].Port[1] = 99
	replicas[0].BackupReplicas[0].Port[0] = 99
	replicas[0].BackupReplicas[1].Instance = 99
	assert.Equal(t, []byte{0, 7}, wire[0].GetPort())
	assert.Equal(t, []byte{8}, wire[0].BackupReplicas[0].Port)
	assert.EqualValues(t, 3, wire[0].BackupReplicas[1].Instance)
	wire[0].GetPort()[1] = 88
	wire[0].BackupReplicas[1].Port[0] = 'X'
	assert.Equal(t, []byte{0, 7}, decoded[0].Port)
	assert.Equal(t, []byte("eth9"), decoded[0].BackupReplicas[1].Port)
	decoded[0].BackupReplicas[0].Port[0] = 77
	assert.Equal(t, []byte{8}, wire[0].BackupReplicas[0].Port)
}

func TestReplicas_NilOneof(t *testing.T) {
	for _, replica := range []*p4v1.Replica{
		nil, {PortKind: (*p4v1.Replica_Port)(nil)},
		{PortKind: (*p4v1.Replica_EgressPort)(nil)}, //nolint:staticcheck // A typed nil legacy arm must return an error.
	} {
		_, err := decodeReplicas([]*p4v1.Replica{replica})
		require.Error(t, err)
	}
}

func FuzzReplicas_RoundTrip(f *testing.F) {
	f.Add([]byte{0, 7}, uint32(3), []byte("eth8"), uint32(4))
	f.Add([]byte{1, 0, 0, 0, 0}, ^uint32(0), []byte{0}, uint32(0))
	f.Fuzz(func(t *testing.T, port []byte, instance uint32, backupPort []byte, backupInstance uint32) {
		if len(port) == 0 || len(backupPort) == 0 {
			return
		}
		wire := []*p4v1.Replica{{PortKind: &p4v1.Replica_Port{Port: port}, Instance: instance,
			BackupReplicas: []*p4v1.BackupReplica{{Port: backupPort, Instance: backupInstance}}}}
		decoded, err := decodeReplicas(wire)
		require.NoError(t, err)
		require.NoError(t, validateReplicas(decoded))
		encoded := encodeReplicas(decoded)
		require.Len(t, encoded, 1)
		require.True(t, proto.Equal(wire[0], encoded[0]))
	})
}
