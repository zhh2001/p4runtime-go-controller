//go:build integration && linux

package integration

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"syscall"
	"testing"
	"time"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/require"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/codec"
	"github.com/zhh2001/p4runtime-go-controller/counter"
	"github.com/zhh2001/p4runtime-go-controller/packetio"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
	"github.com/zhh2001/p4runtime-go-controller/tableentry"
)

type ethernetPort struct {
	fd   int
	addr *syscall.SockaddrLinklayer
}

func openEthernetPorts(t *testing.T) (ethernetPort, ethernetPort) {
	t.Helper()
	one, two := os.Getenv("P4RT_HOST_IFACE1"), os.Getenv("P4RT_HOST_IFACE2")
	if one == "" && two == "" {
		t.Skip("P4RT_HOST_IFACE1 and P4RT_HOST_IFACE2 unset; skipping live Ethernet tests")
	}
	require.NotEmpty(t, one)
	require.NotEmpty(t, two)
	open := func(name string) ethernetPort {
		iface, err := net.InterfaceByName(name)
		require.NoError(t, err)
		protocol := binary.NativeEndian.Uint16([]byte{0x88, 0xb5})
		fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, int(protocol))
		require.NoError(t, err, "raw Ethernet tests require CAP_NET_RAW")
		t.Cleanup(func() { syscall.Close(fd) })
		addr := &syscall.SockaddrLinklayer{Ifindex: iface.Index, Protocol: protocol}
		require.NoError(t, syscall.Bind(fd, addr))
		timeout := syscall.NsecToTimeval((100 * time.Millisecond).Nanoseconds())
		require.NoError(t, syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &timeout))
		return ethernetPort{fd: fd, addr: addr}
	}
	return open(one), open(two)
}

func (p ethernetPort) send(t *testing.T, frame []byte) {
	t.Helper()
	require.NoError(t, syscall.Sendto(p.fd, frame, 0, p.addr))
}

func (p ethernetPort) receives(t *testing.T, frame []byte, timeout time.Duration) bool {
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
		if ok && link.Pkttype != syscall.PACKET_OUTGOING && bytes.Equal(frame, buf[:n]) {
			return true
		}
	}
	return false
}

func l2Frame(marker byte) []byte {
	frame := make([]byte, 60)
	copy(frame, []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x00, 0x66, 0x77, 0x88, 0x99, 0xaa, 0x88, 0xb5})
	for i := 14; i < len(frame); i++ {
		frame[i] = marker
	}
	return frame
}

func TestBMv2_L2Dataplane(t *testing.T) {
	one, two := openEthernetPorts(t)
	require.NotEmpty(t, deviceConfigPath())
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
	require.NoError(t, c.BecomePrimary(ctx))
	_, err = c.SetPipeline(ctx, p, client.SetPipelineOptions{})
	require.NoError(t, err)
	sub, err := packetio.NewSubscriber(c, p)
	require.NoError(t, err)
	packets := make(chan *packetio.PacketIn, 8)
	stop := sub.OnPacket(func(_ context.Context, packet *packetio.PacketIn) {
		if len(packet.Payload) >= 14 && bytes.Equal(packet.Payload[12:14], []byte{0x88, 0xb5}) {
			select {
			case packets <- packet:
			default:
			}
		}
	})
	defer stop()
	packetIn := func(frame []byte, port byte) {
		select {
		case packet := <-packets:
			require.Equal(t, frame, packet.Payload)
			require.Equal(t, []byte{port}, packet.Metadata["ingress_port"])
			require.Equal(t, []byte{0}, packet.Metadata["_pad"])
		case <-time.After(3 * time.Second):
			t.Fatal("PacketIn did not arrive")
		}
	}
	b := tableentry.NewBuilder(p, "MyIngress.t_l2").Match("hdr.eth.dst", tableentry.Exact(codec.MustMAC("00:11:22:33:44:55")))
	entry, err := b.Action("MyIngress.forward", tableentry.Param("port", []byte{2})).Build()
	require.NoError(t, err)
	require.NoError(t, c.WriteTableEntry(ctx, client.UpdateInsert, entry))
	forwarded := l2Frame(1)
	one.send(t, forwarded)
	require.True(t, two.receives(t, forwarded, 3*time.Second), "L2 frame did not reach port 2")
	require.False(t, one.receives(t, forwarded, 200*time.Millisecond), "frame also reached the wrong port")
	r, err := counter.NewReader(c, p)
	require.NoError(t, err)
	counts, err := r.Read(ctx, "MyIngress.pkt_counter", 2)
	require.NoError(t, err)
	require.Len(t, counts, 1)
	require.EqualValues(t, 1, counts[0].Packets)
	require.EqualValues(t, len(forwarded), counts[0].Bytes)
	key, err := b.BuildKey()
	require.NoError(t, err)
	key.CounterData = &p4v1.CounterData{}
	entities, err := c.Read(ctx, &p4v1.Entity{Entity: &p4v1.Entity_TableEntry{TableEntry: key}})
	require.NoError(t, err)
	require.Len(t, entities, 1)
	require.EqualValues(t, 1, entities[0].GetTableEntry().GetCounterData().GetPacketCount())
	require.EqualValues(t, len(forwarded), entities[0].GetTableEntry().GetCounterData().GetByteCount())
	unknown := l2Frame(2)
	unknown[5] = 0x56
	two.send(t, unknown)
	packetIn(unknown, 2)
	require.False(t, one.receives(t, unknown, 200*time.Millisecond), "unmatched frame was forwarded")
	for port, iface := range map[byte]ethernetPort{1: one, 2: two} {
		frame := l2Frame(10 + port)
		require.NoError(t, sub.Send(ctx, &packetio.PacketOut{Payload: frame, Metadata: map[string][]byte{"egress_port": {port}, "_pad": {0}}}))
		require.True(t, iface.receives(t, frame, 3*time.Second), "PacketOut did not reach port %d with its original payload", port)
	}
	counts, err = r.Read(ctx, "MyIngress.pkt_counter", 2)
	require.NoError(t, err)
	require.EqualValues(t, 1, counts[0].Packets)
	key.CounterData = nil
	require.NoError(t, c.WriteTableEntry(ctx, client.UpdateDelete, key))
	one.send(t, forwarded)
	packetIn(forwarded, 1)
	require.False(t, two.receives(t, forwarded, 200*time.Millisecond), "deleted entry still forwarded traffic")
}
