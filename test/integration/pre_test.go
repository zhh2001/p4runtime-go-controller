//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
	"github.com/zhh2001/p4runtime-go-controller/pre"
)

func TestBMv2_PRE(t *testing.T) {
	if deviceConfigPath() == "" {
		t.Skip("P4RT_DEVICE_CONFIG unset; skipping live PRE operations")
	}
	info, err := os.ReadFile(p4infoPath())
	require.NoError(t, err)
	config, err := os.ReadFile(deviceConfigPath())
	require.NoError(t, err)
	p, err := pipeline.LoadText(info, config)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, targetAddr(), client.WithDeviceID(1), client.WithElectionID(client.ElectionID{Low: 1}), client.WithInsecure())
	require.NoError(t, err)
	defer c.Close()
	_, err = c.SetPipeline(ctx, p, client.SetPipelineOptions{})
	require.NoError(t, err)
	w, err := pre.NewWriter(c)
	require.NoError(t, err)
	for _, replicas := range [][]pre.Replica{
		{{EgressPort: 1, Instance: 1}, {EgressPort: 2, Instance: 2}},
		{{Port: []byte{1}, Instance: 1}, {Port: []byte{0, 0, 0, 2}, Instance: 2}},
		{{EgressPort: 1, Instance: 1}, {Port: []byte{0, 2}, Instance: 2}},
	} {
		mg := pre.MulticastGroup{ID: 19, Replicas: replicas}
		require.NoError(t, w.InsertMulticastGroup(ctx, mg))
		groups, err := w.ReadMulticastGroups(ctx, mg.ID)
		require.NoError(t, err)
		require.Len(t, groups, 1)
		require.ElementsMatch(t, replicas, groups[0].Replicas)
		require.NoError(t, w.ModifyMulticastGroup(ctx, groups[0]))
		groups[0].Replicas = []pre.Replica{{Port: []byte{0, 1}, Instance: 3}}
		require.NoError(t, w.ModifyMulticastGroup(ctx, groups[0]))
		groups, err = w.ReadMulticastGroups(ctx, mg.ID)
		require.NoError(t, err)
		require.Len(t, groups, 1)
		require.Equal(t, []pre.Replica{{Port: []byte{0, 1}, Instance: 3}}, groups[0].Replicas)
		require.NoError(t, w.DeleteMulticastGroup(ctx, mg.ID))
		groups, err = w.ReadMulticastGroups(ctx, 0)
		require.NoError(t, err)
		for _, group := range groups {
			require.NotEqual(t, mg.ID, group.ID)
		}

		// BMv2 supports clone sessions without packet truncation.
		cs := pre.CloneSession{ID: 319, Replicas: replicas}
		require.NoError(t, w.InsertCloneSession(ctx, cs))
		sessions, err := w.ReadCloneSessions(ctx, cs.ID)
		require.NoError(t, err)
		require.Len(t, sessions, 1)
		require.ElementsMatch(t, replicas, sessions[0].Replicas)
		require.Zero(t, sessions[0].PacketLengthBytes)
		require.NoError(t, w.ModifyCloneSession(ctx, sessions[0]))
		sessions[0].Replicas = []pre.Replica{{Port: []byte{0, 2}, Instance: 4}}
		require.NoError(t, w.ModifyCloneSession(ctx, sessions[0]))
		sessions, err = w.ReadCloneSessions(ctx, cs.ID)
		require.NoError(t, err)
		require.Len(t, sessions, 1)
		require.Equal(t, []pre.Replica{{Port: []byte{0, 2}, Instance: 4}}, sessions[0].Replicas)
		require.Zero(t, sessions[0].PacketLengthBytes)
		require.NoError(t, w.DeleteCloneSession(ctx, cs.ID))
		sessions, err = w.ReadCloneSessions(ctx, 0)
		require.NoError(t, err)
		for _, session := range sessions {
			require.NotEqual(t, cs.ID, session.ID)
		}
	}
}
