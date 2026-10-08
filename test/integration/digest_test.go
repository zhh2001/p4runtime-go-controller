//go:build integration && linux

package integration

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/require"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/digest"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
)

func TestBMv2_DigestSubscriptions(t *testing.T) {
	infoPath, configPath := os.Getenv("P4RT_DIGEST_P4INFO"), os.Getenv("P4RT_DIGEST_DEVICE_CONFIG")
	if infoPath == "" && configPath == "" {
		t.Skip("P4RT_DIGEST_P4INFO and P4RT_DIGEST_DEVICE_CONFIG unset; skipping live digest tests")
	}
	require.NotEmpty(t, infoPath)
	require.NotEmpty(t, configPath)
	one, _ := openEthernetPorts(t)
	info, err := os.ReadFile(infoPath)
	require.NoError(t, err)
	config, err := os.ReadFile(configPath)
	require.NoError(t, err)
	p, err := pipeline.LoadText(info, config)
	require.NoError(t, err)
	learn, ok := p.Digest("mac_learn_t")
	require.True(t, ok)
	port, ok := p.Digest("port_event_t")
	require.True(t, ok)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, targetAddr(), client.WithDeviceID(1), client.WithElectionID(client.ElectionID{Low: 1}), client.WithInsecure())
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.BecomePrimary(ctx))
	_, err = c.SetPipeline(ctx, p, client.SetPipelineOptions{})
	require.NoError(t, err)
	sub, err := digest.NewSubscriber(c, p)
	require.NoError(t, err)

	learned := make(chan *p4v1.DigestList, 8)
	stopLearn, err := sub.Subscribe(learn.Name, func(_ context.Context, msg *p4v1.DigestList) { learned <- msg })
	require.NoError(t, err)
	defer stopLearn()
	ports := make(chan *p4v1.DigestList, 8)
	stopPort, err := sub.Subscribe(port.Name, func(_ context.Context, msg *p4v1.DigestList) { ports <- msg })
	require.NoError(t, err)
	defer stopPort()
	all := make(chan *p4v1.DigestList, 8)
	stopAll, err := sub.Subscribe("", func(_ context.Context, msg *p4v1.DigestList) { all <- msg })
	require.NoError(t, err)
	defer stopAll()
	var invalidCalls atomic.Int32
	invalid := func(context.Context, *p4v1.DigestList) { invalidCalls.Add(1) }
	stopInvalid, err := sub.Subscribe("typo", invalid)
	require.ErrorContains(t, err, "unknown digest")
	require.Nil(t, stopInvalid)
	defer sub.OnDigest("typo", invalid)()

	for _, id := range []uint32{learn.ID, port.ID} {
		require.NoError(t, c.Write(ctx, client.WriteOptions{}, &p4v1.Update{
			Type: p4v1.Update_INSERT,
			Entity: &p4v1.Entity{Entity: &p4v1.Entity_DigestEntry{DigestEntry: &p4v1.DigestEntry{
				DigestId: id,
				Config:   &p4v1.DigestEntry_Config{MaxListSize: 1, AckTimeoutNs: int64(30 * time.Second)},
			}}},
		}))
	}
	frame := l2Frame(100)
	frame[5] = 0x54
	portFrame := append([]byte(nil), frame...)
	portFrame[5] = 0x55
	one.send(t, frame)
	one.send(t, portFrame)
	firstLearn := receiveDigest(t, learned)
	firstPort := receiveDigest(t, ports)
	require.Equal(t, learn.ID, firstLearn.DigestId)
	require.Equal(t, port.ID, firstPort.DigestId)
	for _, batch := range []*p4v1.DigestList{firstLearn, firstPort} {
		require.Len(t, batch.Data, 1)
		require.Len(t, batch.Data[0].GetStruct().GetMembers(), 2)
	}
	learnData := firstLearn.Data[0].GetStruct().GetMembers()
	require.Equal(t, []byte{0x66, 0x77, 0x88, 0x99, 0xaa}, learnData[0].GetBitstring())
	require.Equal(t, []byte{1}, learnData[1].GetBitstring())
	portData := firstPort.Data[0].GetStruct().GetMembers()
	require.Equal(t, []byte{1}, portData[0].GetBitstring())
	require.Equal(t, frame[12:14], portData[1].GetBitstring())
	firstIDs := []uint32{receiveDigest(t, all).DigestId, receiveDigest(t, all).DigestId}
	require.ElementsMatch(t, []uint32{learn.ID, port.ID}, firstIDs)

	require.Error(t, sub.Ack(ctx, &p4v1.DigestList{DigestId: 0, ListId: firstLearn.ListId}))
	require.Error(t, sub.Ack(ctx, &p4v1.DigestList{DigestId: 0x17ffffff, ListId: firstPort.ListId}))
	one.send(t, frame)
	one.send(t, portFrame)
	require.Never(t, func() bool { return len(all) != 0 }, 300*time.Millisecond, 10*time.Millisecond,
		"unacknowledged digest data was delivered again")

	stopLearn()
	require.NoError(t, sub.Ack(ctx, firstLearn))
	// Observe each ACK through redelivery before sending the next one. Some PI
	// versions retain a reference to a request while processing it asynchronously.
	require.Eventually(t, func() bool {
		one.send(t, frame)
		return len(all) != 0
	}, 3*time.Second, 20*time.Millisecond)
	secondLearn := receiveDigest(t, all)
	require.Equal(t, learn.ID, secondLearn.DigestId)
	require.NotEqual(t, firstLearn.ListId, secondLearn.ListId)
	require.Empty(t, ports)
	require.NoError(t, sub.Ack(ctx, firstPort))
	require.Eventually(t, func() bool {
		one.send(t, portFrame)
		return len(ports) != 0
	}, 3*time.Second, 20*time.Millisecond)
	secondPort := receiveDigest(t, ports)
	require.Equal(t, port.ID, secondPort.DigestId)
	require.NotEqual(t, firstPort.ListId, secondPort.ListId)
	require.Equal(t, secondPort.ListId, receiveDigest(t, all).ListId)
	require.NoError(t, c.CloseGracefully(ctx))
	require.Empty(t, learned)
	require.Zero(t, invalidCalls.Load())
}

func receiveDigest(t *testing.T, ch <-chan *p4v1.DigestList) *p4v1.DigestList {
	t.Helper()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(3 * time.Second):
		t.Fatal("digest timeout")
		return nil
	}
}
