//go:build integration && linux

package integration

import (
	"bytes"
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
	"github.com/zhh2001/p4runtime-go-controller/pre"
)

// receivesReplica matches a frame while allowing any egress instance byte.
func (p ethernetPort) receivesReplica(t *testing.T, frame []byte, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	buf := make([]byte, 2048)
	for time.Now().Before(deadline) {
		n, addr, err := syscall.Recvfrom(p.fd, buf, 0)
		if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EINTR) {
			continue
		}
		require.NoError(t, err)
		link, ok := addr.(*syscall.SockaddrLinklayer)
		if !ok || link.Pkttype == syscall.PACKET_OUTGOING || n != len(frame) || n < 12 {
			continue
		}
		buf[11] = frame[11]
		if bytes.Equal(frame, buf[:n]) {
			return true
		}
	}
	return false
}

func TestBMv2_PREDataplane(t *testing.T) {
	infoPath, configPath := os.Getenv("P4RT_PRE_P4INFO"), os.Getenv("P4RT_PRE_DEVICE_CONFIG")
	if infoPath == "" && configPath == "" {
		t.Skip("P4RT_PRE_P4INFO and P4RT_PRE_DEVICE_CONFIG unset; skipping live replication")
	}
	require.NotEmpty(t, infoPath)
	require.NotEmpty(t, configPath)
	one, two := openEthernetPorts(t)
	info, err := os.ReadFile(infoPath)
	require.NoError(t, err)
	config, err := os.ReadFile(configPath)
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
	frame := func(marker byte, clone bool) []byte {
		value := l2Frame(marker)
		value[5] = 0x50
		if clone {
			value[5] = 0x51
		}
		return value
	}
	replica := func(value []byte, instance byte) []byte {
		out := bytes.Clone(value)
		out[11] = instance
		return out
	}
	mg := pre.MulticastGroup{ID: 19, Replicas: []pre.Replica{{EgressPort: 1, Instance: 11}, {Port: []byte{0, 2}, Instance: 22}}}
	require.NoError(t, w.InsertMulticastGroup(ctx, mg))
	multicast := frame(71, false)
	one.send(t, multicast)
	require.True(t, one.receives(t, replica(multicast, 11), 3*time.Second), "legacy replica did not arrive with instance 11")
	require.True(t, two.receives(t, replica(multicast, 22), 3*time.Second), "byte-port replica did not arrive with instance 22")
	groups, err := w.ReadMulticastGroups(ctx, mg.ID)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.ElementsMatch(t, mg.Replicas, groups[0].Replicas)
	require.NoError(t, w.ModifyMulticastGroup(ctx, groups[0]))
	multicast = frame(72, false)
	one.send(t, multicast)
	require.True(t, one.receives(t, replica(multicast, 11), 3*time.Second))
	require.True(t, two.receives(t, replica(multicast, 22), 3*time.Second))
	groups[0].Replicas = []pre.Replica{{Port: []byte{2}, Instance: 33}}
	require.NoError(t, w.ModifyMulticastGroup(ctx, groups[0]))
	multicast = frame(73, false)
	one.send(t, multicast)
	require.True(t, two.receives(t, replica(multicast, 33), 3*time.Second))
	require.False(t, one.receivesReplica(t, replica(multicast, 33), 200*time.Millisecond), "removed multicast port still received the packet")
	require.NoError(t, w.DeleteMulticastGroup(ctx, mg.ID))
	multicast = frame(74, false)
	one.send(t, multicast)
	require.False(t, one.receivesReplica(t, replica(multicast, 33), 200*time.Millisecond))
	require.False(t, two.receivesReplica(t, replica(multicast, 33), 200*time.Millisecond))

	cs := pre.CloneSession{ID: 319, Replicas: []pre.Replica{{Port: []byte{0, 2}, Instance: 44}}}
	require.NoError(t, w.InsertCloneSession(ctx, cs))
	cloned := frame(81, true)
	one.send(t, cloned)
	require.True(t, two.receives(t, replica(cloned, 44), 3*time.Second), "byte-port clone did not arrive with instance 44")
	require.False(t, one.receivesReplica(t, replica(cloned, 44), 200*time.Millisecond))
	sessions, err := w.ReadCloneSessions(ctx, cs.ID)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.Equal(t, cs, sessions[0])
	require.NoError(t, w.ModifyCloneSession(ctx, sessions[0]))
	cloned = frame(82, true)
	one.send(t, cloned)
	require.True(t, two.receives(t, replica(cloned, 44), 3*time.Second))
	sessions[0].Replicas = []pre.Replica{{EgressPort: 1, Instance: 55}}
	require.NoError(t, w.ModifyCloneSession(ctx, sessions[0]))
	cloned = frame(83, true)
	one.send(t, cloned)
	require.True(t, one.receives(t, replica(cloned, 55), 3*time.Second), "modified clone did not reach the legacy port")
	require.False(t, two.receivesReplica(t, replica(cloned, 55), 200*time.Millisecond))
	require.NoError(t, w.DeleteCloneSession(ctx, cs.ID))
	cloned = frame(84, true)
	one.send(t, cloned)
	require.False(t, one.receivesReplica(t, replica(cloned, 55), 200*time.Millisecond))
	require.False(t, two.receivesReplica(t, replica(cloned, 55), 200*time.Millisecond))
}
