package cmd

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"

	errs "github.com/zhh2001/p4runtime-go-controller/v2/errors"
)

type packetCommandServer struct {
	p4v1.UnimplementedP4RuntimeServer
	packet chan *p4v1.PacketOut
	half   chan struct{}
	final  chan error
	ended  chan struct{}
}

func writePacketInfo(t *testing.T, info *p4configv1.P4Info) string {
	t.Helper()
	data, err := prototext.Marshal(info)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "packet.p4info.txt")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

func setPacketSendInput(t *testing.T, info string, port uint64) {
	t.Helper()
	oldInfo, oldPort, oldHex := packetP4Info, packetPort, packetHex
	t.Cleanup(func() { packetP4Info, packetPort, packetHex = oldInfo, oldPort, oldHex })
	packetP4Info, packetPort, packetHex = info, port, "dead"
}

func TestPacketSendMetadata(t *testing.T) {
	for _, tc := range []struct {
		name    string
		bits    int32
		port    uint64
		padding bool
		want    []byte
	}{
		{name: "zero", bits: 9, padding: true, want: []byte{0}},
		{name: "9-bit maximum", bits: 9, port: 511, padding: true, want: []byte{1, 255}},
		{name: "16-bit port", bits: 16, port: 65535, padding: true, want: []byte{255, 255}},
		{name: "without padding", bits: 8, port: 255, want: []byte{255}},
		{name: "64-bit maximum", bits: 64, port: ^uint64(0), want: []byte{255, 255, 255, 255, 255, 255, 255, 255}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields := []*p4configv1.ControllerPacketMetadata_Metadata{}
			want := &p4v1.PacketOut{Payload: []byte{0xde, 0xad}}
			if tc.padding {
				fields = append(fields, &p4configv1.ControllerPacketMetadata_Metadata{Id: 7, Name: "_pad", Bitwidth: 7})
				want.Metadata = append(want.Metadata, &p4v1.PacketMetadata{MetadataId: 7, Value: []byte{0}})
			}
			fields = append(fields, &p4configv1.ControllerPacketMetadata_Metadata{Id: 4, Name: "egress_port", Bitwidth: tc.bits})
			want.Metadata = append(want.Metadata, &p4v1.PacketMetadata{MetadataId: 4, Value: tc.want})
			info := &p4configv1.P4Info{ControllerPacketMetadata: []*p4configv1.ControllerPacketMetadata{{Preamble: &p4configv1.Preamble{Id: 2, Name: "packet_out"}, Metadata: fields}}}
			setPacketSendInput(t, writePacketInfo(t, info), tc.port)
			mock := &packetCommandServer{packet: make(chan *p4v1.PacketOut, 1), half: make(chan struct{}), final: make(chan error, 1), ended: make(chan struct{})}
			mock.final <- nil
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			server := grpc.NewServer()
			p4v1.RegisterP4RuntimeServer(server, mock)
			go func() { _ = server.Serve(listener) }()
			t.Cleanup(server.Stop)
			setDialFlags(t, globalFlags{Addr: listener.Addr().String(), DeviceID: 1, Election: 1, Insecure: true})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			command := &cobra.Command{}
			command.SetContext(ctx)
			require.NoError(t, packetSendCmd.RunE(command, nil))
			require.True(t, proto.Equal(want, <-mock.packet))
		})
	}
}

func TestPacketSendMetadataErrorsBeforeDial(t *testing.T) {
	for _, tc := range []struct {
		name  string
		info  *p4configv1.P4Info
		port  uint64
		want  string
		width bool
	}{
		{name: "no header", info: &p4configv1.P4Info{}, want: "packet_out"},
		{name: "no egress port", info: &p4configv1.P4Info{ControllerPacketMetadata: []*p4configv1.ControllerPacketMetadata{{Preamble: &p4configv1.Preamble{Id: 2, Name: "packet_out"}}}}, want: "egress_port"},
		{name: "overflow", info: &p4configv1.P4Info{ControllerPacketMetadata: []*p4configv1.ControllerPacketMetadata{{Preamble: &p4configv1.Preamble{Id: 2, Name: "packet_out"}, Metadata: []*p4configv1.ControllerPacketMetadata_Metadata{{Id: 1, Name: "egress_port", Bitwidth: 9}}}}}, port: 512, want: "--port", width: true},
		{name: "invalid width", info: &p4configv1.P4Info{ControllerPacketMetadata: []*p4configv1.ControllerPacketMetadata{{Preamble: &p4configv1.Preamble{Id: 2, Name: "packet_out"}, Metadata: []*p4configv1.ControllerPacketMetadata_Metadata{{Id: 1, Name: "egress_port"}}}}}, want: "bitwidth"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setPacketSendInput(t, writePacketInfo(t, tc.info), tc.port)
			setDialFlags(t, globalFlags{Addr: "127.0.0.1:0", DeviceID: 1, Election: 1, Insecure: true})
			command := &cobra.Command{}
			command.SetContext(context.Background())
			var err error
			require.NotPanics(t, func() { err = packetSendCmd.RunE(command, nil) })
			require.ErrorContains(t, err, tc.want)
			if tc.width {
				require.ErrorIs(t, err, errs.ErrInvalidBitWidth)
			}
		})
	}
}

func (s *packetCommandServer) StreamChannel(stream p4v1.P4Runtime_StreamChannelServer) error {
	defer close(s.ended)
	arb, err := stream.Recv()
	if err != nil {
		return err
	}
	response := arb.GetArbitration()
	response.Status = &rpcstatus.Status{Code: int32(codes.OK)}
	if err := stream.Send(&p4v1.StreamMessageResponse{Update: &p4v1.StreamMessageResponse_Arbitration{Arbitration: response}}); err != nil {
		return err
	}
	packet, err := stream.Recv()
	if err != nil {
		return err
	}
	s.packet <- packet.GetPacket()
	if _, err = stream.Recv(); !errors.Is(err, io.EOF) {
		return err
	}
	close(s.half)
	select {
	case err := <-s.final:
		return err
	case <-stream.Context().Done():
		return stream.Context().Err()
	}
}

func TestPacketSendWaitsForFinalStatus(t *testing.T) {
	for _, mode := range []string{"success", "target error", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			mock := &packetCommandServer{packet: make(chan *p4v1.PacketOut, 1), half: make(chan struct{}), final: make(chan error, 1), ended: make(chan struct{})}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			server := grpc.NewServer()
			p4v1.RegisterP4RuntimeServer(server, mock)
			go func() { _ = server.Serve(listener) }()
			t.Cleanup(server.Stop)
			setDialFlags(t, globalFlags{Addr: listener.Addr().String(), DeviceID: 1, Election: 1, Insecure: true})
			oldInfo, oldPort, oldHex := packetP4Info, packetPort, packetHex
			t.Cleanup(func() { packetP4Info, packetPort, packetHex = oldInfo, oldPort, oldHex })
			packetP4Info, err = filepath.Abs("../../../examples/testdata/l2.p4info.txt")
			require.NoError(t, err)
			payload := make([]byte, 60)
			packetPort, packetHex = 2, hex.EncodeToString(payload)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			command := &cobra.Command{}
			command.SetContext(ctx)
			done := make(chan error, 1)
			go func() { done <- packetSendCmd.RunE(command, nil) }()
			select {
			case <-mock.half:
			case err := <-done:
				t.Fatalf("packet send exited without a successful half-close: %v", err)
			case <-ctx.Done():
				t.Fatal("packet send did not half-close its stream")
			}
			packet := <-mock.packet
			require.Equal(t, payload, packet.Payload)
			require.Len(t, packet.Metadata, 2)
			metadata := make(map[uint32][]byte)
			for _, field := range packet.Metadata {
				metadata[field.MetadataId] = field.Value
			}
			require.Equal(t, []byte{2}, metadata[1])
			require.Equal(t, []byte{0}, metadata[2])
			select {
			case err := <-done:
				t.Fatalf("packet send exited before the target returned final status: %v", err)
			default:
			}
			switch mode {
			case "success":
				mock.final <- nil
				require.NoError(t, <-done)
			case "target error":
				mock.final <- status.Error(codes.PermissionDenied, "target denied request")
				assert.Equal(t, codes.PermissionDenied, status.Code(<-done))
			case "deadline":
				assert.ErrorIs(t, <-done, context.DeadlineExceeded)
			}
			select {
			case <-mock.ended:
			case <-time.After(time.Second):
				t.Fatal("packet send left its stream open")
			}
		})
	}
}
