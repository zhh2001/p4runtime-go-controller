package client_test

import (
	"context"
	"net"
	"testing"
	"time"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	"github.com/zhh2001/p4runtime-go-controller/v2/client"
	"github.com/zhh2001/p4runtime-go-controller/v2/internal/testutil"
)

type readRoleServer struct {
	*testutil.MockServer
	requests chan *p4v1.ReadRequest
	read     func(*p4v1.ReadRequest, p4v1.P4Runtime_ReadServer) error
}

func (s *readRoleServer) Read(req *p4v1.ReadRequest, stream p4v1.P4Runtime_ReadServer) error {
	s.requests <- req
	return s.read(req, stream)
}

func startReadRoleServer(t *testing.T, read func(*p4v1.ReadRequest, p4v1.P4Runtime_ReadServer) error) (*readRoleServer, client.Option) {
	t.Helper()
	s := &readRoleServer{MockServer: testutil.NewMockServer(), requests: make(chan *p4v1.ReadRequest, 64), read: read}
	lis := bufconn.Listen(1024 * 1024)
	gs := grpc.NewServer()
	p4v1.RegisterP4RuntimeServer(gs, s)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = gs.Serve(lis)
	}()
	t.Cleanup(func() {
		gs.Stop()
		<-done
	})
	return s, client.WithDialOptions(grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return lis.DialContext(ctx)
	}))
}

func TestRead_RoleRequests(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []client.Option
		role string
	}{
		{name: "default"},
		{name: "explicit default", opts: []client.Option{client.WithRole("")}},
		{name: "named", opts: []client.Option{client.WithRole("tenant-a")}, role: "tenant-a"},
		{name: "last option", opts: []client.Option{client.WithRole("tenant-a"), client.WithRole("tenant-b")}, role: "tenant-b"},
		{name: "unicode", opts: []client.Option{client.WithRole("租户 A")}, role: "租户 A"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			one := &p4v1.Entity{Entity: &p4v1.Entity_TableEntry{TableEntry: &p4v1.TableEntry{TableId: 7, Priority: 1}}}
			two := &p4v1.Entity{Entity: &p4v1.Entity_CounterEntry{CounterEntry: &p4v1.CounterEntry{CounterId: 9}}}
			s, dialer := startReadRoleServer(t, func(_ *p4v1.ReadRequest, stream p4v1.P4Runtime_ReadServer) error {
				if err := stream.Send(&p4v1.ReadResponse{Entities: []*p4v1.Entity{one}}); err != nil {
					return err
				}
				return stream.Send(&p4v1.ReadResponse{Entities: []*p4v1.Entity{two}})
			})
			// A backup can read without waiting to become primary.
			s.PrimaryElectionLow = 99
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			opts := []client.Option{client.WithDeviceID(17), client.WithElectionID(client.ElectionID{Low: 1}), client.WithInsecure(), dialer}
			c, err := client.Dial(ctx, "passthrough:bufnet", append(opts, tc.opts...)...)
			require.NoError(t, err)
			defer c.Close()
			require.Equal(t, client.StateBackup, c.State())

			selectors := []*p4v1.Entity{
				{Entity: &p4v1.Entity_TableEntry{TableEntry: &p4v1.TableEntry{}}},
				{Entity: &p4v1.Entity_CounterEntry{CounterEntry: &p4v1.CounterEntry{CounterId: 9, Index: &p4v1.Index{Index: 3}}}},
			}
			entities, err := c.Read(ctx, selectors...)
			require.NoError(t, err)
			require.Len(t, entities, 2)
			assert.True(t, proto.Equal(one, entities[0]))
			assert.True(t, proto.Equal(two, entities[1]))
			req := <-s.requests
			assert.True(t, proto.Equal(&p4v1.ReadRequest{DeviceId: 17, Role: tc.role, Entities: selectors}, req), "unexpected request: %v", req)

			entries, err := c.ReadTableEntries(ctx, 0)
			require.NoError(t, err)
			require.Len(t, entries, 1)
			assert.True(t, proto.Equal(one.GetTableEntry(), entries[0]))
			req = <-s.requests
			assert.Equal(t, tc.role, req.GetRole())
			require.Len(t, req.GetEntities(), 1)
			assert.Zero(t, req.GetEntities()[0].GetTableEntry().GetTableId())
			s.Mu.Lock()
			assert.Equal(t, tc.role, s.LastArbitrationUpdate.GetRole().GetName())
			s.Mu.Unlock()
		})
	}
}

func TestRead_RoleFiltering(t *testing.T) {
	entries := []*p4v1.TableEntry{{TableId: 7, Priority: 1}, {TableId: 8, Priority: 2}, {TableId: 7, Priority: 3}}
	for _, tc := range []struct {
		role string
		want []*p4v1.TableEntry
	}{
		{role: "", want: entries},
		{role: "tenant-a", want: []*p4v1.TableEntry{entries[0], entries[2]}},
		{role: "tenant-b", want: []*p4v1.TableEntry{entries[1]}},
		{role: "unknown"},
	} {
		t.Run(tc.role, func(t *testing.T) {
			s, dialer := startReadRoleServer(t, func(req *p4v1.ReadRequest, stream p4v1.P4Runtime_ReadServer) error {
				allowed := uint32(0)
				switch req.GetRole() {
				case "":
				case "tenant-a":
					allowed = 7
				case "tenant-b":
					allowed = 8
				default:
					return status.Error(codes.PermissionDenied, "unknown read role")
				}
				for _, entry := range entries {
					if allowed != 0 && entry.GetTableId() != allowed {
						continue
					}
					if err := stream.Send(&p4v1.ReadResponse{Entities: []*p4v1.Entity{{Entity: &p4v1.Entity_TableEntry{TableEntry: entry}}}}); err != nil {
						return err
					}
				}
				return nil
			})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			c, err := client.Dial(ctx, "passthrough:bufnet", client.WithDeviceID(1), client.WithElectionID(client.ElectionID{Low: 1}), client.WithInsecure(), client.WithRole(tc.role), dialer)
			require.NoError(t, err)
			defer c.Close()
			got, err := c.ReadTableEntries(ctx, 0)
			if tc.role == "unknown" {
				require.Error(t, err)
				assert.Nil(t, got)
				assert.Equal(t, codes.PermissionDenied, status.Code(err))
				assert.Contains(t, status.Convert(err).Message(), "unknown read role")
			} else {
				require.NoError(t, err)
				require.Len(t, got, len(tc.want))
				for i, want := range tc.want {
					assert.True(t, proto.Equal(want, got[i]))
				}
			}
			req := <-s.requests
			assert.Equal(t, tc.role, req.GetRole())
			assert.Empty(t, s.requests, "a read must not retry with the default role")
		})
	}
}
