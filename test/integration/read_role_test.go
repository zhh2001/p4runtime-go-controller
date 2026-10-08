//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/internal/codec"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
	"github.com/zhh2001/p4runtime-go-controller/tableentry"
)

type readRoleStream struct {
	grpc.ClientStream
	requests chan<- *p4v1.ReadRequest
}

func (s *readRoleStream) SendMsg(msg any) error {
	if err := s.ClientStream.SendMsg(msg); err != nil {
		return err
	}
	if req, ok := msg.(*p4v1.ReadRequest); ok {
		s.requests <- proto.Clone(req).(*p4v1.ReadRequest)
	}
	return nil
}

func TestBMv2_ReadRole(t *testing.T) {
	if deviceConfigPath() == "" {
		t.Skip("P4RT_DEVICE_CONFIG unset; skipping live read roles")
	}
	info, err := os.ReadFile(p4infoPath())
	require.NoError(t, err)
	config, err := os.ReadFile(deviceConfigPath())
	require.NoError(t, err)
	p, err := pipeline.LoadText(info, config)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	primary, err := client.Dial(ctx, targetAddr(), client.WithDeviceID(1), client.WithElectionID(client.ElectionID{Low: 100}), client.WithInsecure())
	require.NoError(t, err)
	defer primary.Close()
	_, err = primary.SetPipeline(ctx, p, client.SetPipelineOptions{})
	require.NoError(t, err)
	entry, err := tableentry.NewBuilder(p, "MyIngress.t_l2").
		Match("hdr.eth.dst", tableentry.Exact(codec.MustMAC("00:11:22:33:44:55"))).
		Action("MyIngress.forward", tableentry.Param("port", codec.MustEncodeUint(1, 9))).Build()
	require.NoError(t, err)
	require.NoError(t, primary.WriteTableEntry(ctx, client.UpdateInsert, entry))

	for _, role := range []string{"", "tenant-a"} {
		t.Run(role, func(t *testing.T) {
			requests := make(chan *p4v1.ReadRequest, 2)
			interceptor := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
				stream, err := streamer(ctx, desc, cc, method, opts...)
				if err != nil {
					return nil, err
				}
				return &readRoleStream{ClientStream: stream, requests: requests}, nil
			}
			reader, err := client.Dial(ctx, targetAddr(), client.WithDeviceID(1), client.WithElectionID(client.ElectionID{Low: 1}), client.WithInsecure(), client.WithRole(role), client.WithDialOptions(grpc.WithStreamInterceptor(interceptor)))
			require.NoError(t, err)
			defer reader.Close()
			entries, err := reader.ReadTableEntries(ctx, 0)
			require.NoError(t, err)
			var found bool
			for _, stored := range entries {
				if stored.GetTableId() == entry.GetTableId() && proto.Equal(stored.GetAction(), entry.GetAction()) &&
					len(stored.GetMatch()) == 1 && proto.Equal(stored.GetMatch()[0], entry.GetMatch()[0]) {
					found = true
				}
			}
			require.True(t, found, "installed entry missing from read")
			req := <-requests
			require.Equal(t, role, req.GetRole())
			require.EqualValues(t, 1, req.GetDeviceId())
			require.Len(t, req.GetEntities(), 1)
			require.Zero(t, req.GetEntities()[0].GetTableEntry().GetTableId())
			t.Logf("role %q read installed entry, reader state %s; BMv2 acceptance does not establish role filtering", role, reader.State())
		})
	}
}
