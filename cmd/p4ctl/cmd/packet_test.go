package cmd

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type packetCommandServer struct {
	p4v1.UnimplementedP4RuntimeServer
	packet chan *p4v1.PacketOut
	half   chan struct{}
	final  chan error
	ended  chan struct{}
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
			metadata := make(map[uint32][]byte)
			for _, field := range packet.Metadata {
				metadata[field.MetadataId] = field.Value
			}
			require.Equal(t, []byte{2}, metadata[1])
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
