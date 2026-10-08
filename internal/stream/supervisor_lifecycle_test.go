package stream

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

type lifecycleStream struct {
	grpc.ClientStream
	ctx       context.Context
	send      func(*p4v1.StreamMessageRequest) error
	recv      func() (*p4v1.StreamMessageResponse, error)
	closeSend func() error
}

func (s *lifecycleStream) Context() context.Context { return s.ctx }

func (s *lifecycleStream) Send(req *p4v1.StreamMessageRequest) error {
	if s.send != nil {
		return s.send(req)
	}
	return nil
}

func (s *lifecycleStream) Recv() (*p4v1.StreamMessageResponse, error) {
	return s.recv()
}

func (s *lifecycleStream) CloseSend() error {
	if s.closeSend != nil {
		return s.closeSend()
	}
	return nil
}

func arbitrationResponse(code codes.Code) *p4v1.StreamMessageResponse {
	return &p4v1.StreamMessageResponse{
		Update: &p4v1.StreamMessageResponse_Arbitration{
			Arbitration: &p4v1.MasterArbitrationUpdate{
				Status: &rpcstatus.Status{Code: int32(code)},
			},
		},
	}
}

func packetResponse() *p4v1.StreamMessageResponse {
	return &p4v1.StreamMessageResponse{
		Update: &p4v1.StreamMessageResponse_Packet{Packet: &p4v1.PacketIn{}},
	}
}

func TestSupervisorArbitrationUpdates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		messages := make(chan *p4v1.StreamMessageResponse, 1)
		messages <- arbitrationResponse(codes.OK)
		s := New(Config{}, func(ctx context.Context) (p4v1.P4Runtime_StreamChannelClient, error) {
			return &lifecycleStream{ctx: ctx, recv: func() (*p4v1.StreamMessageResponse, error) {
				select {
				case msg := <-messages:
					return msg, nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}}, nil
		}, nil)
		s.Start(context.Background())
		synctest.Wait()
		require.Equal(t, StatePrimary, s.State())
		require.True(t, s.IsPrimary())
		messages <- arbitrationResponse(codes.AlreadyExists)
		synctest.Wait()
		assert.Equal(t, StateBackup, s.State())
		assert.False(t, s.IsPrimary())
		messages <- arbitrationResponse(codes.OK)
		synctest.Wait()
		assert.Equal(t, StatePrimary, s.State())
		assert.True(t, s.IsPrimary())
		s.Close()
		var states []State
		for ev := range s.Events() {
			states = append(states, ev.State)
		}
		assert.Equal(t, []State{StateConnecting, StatePrimary, StateBackup, StatePrimary, StateDisconnected}, states)
	})
}

func TestSupervisorCloseWithPendingArbitration(t *testing.T) {
	for _, shutdown := range []string{"close", "cancel"} {
		t.Run(shutdown, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				pending := make(chan struct{})
				release := make(chan struct{})
				s := New(Config{}, func(ctx context.Context) (p4v1.P4Runtime_StreamChannelClient, error) {
					calls := 0
					return &lifecycleStream{ctx: ctx, recv: func() (*p4v1.StreamMessageResponse, error) {
						calls++
						if calls == 1 {
							return arbitrationResponse(codes.OK), nil
						}
						if calls == 2 {
							close(pending)
							<-release
							// Model a response already read before cancellation.
							return arbitrationResponse(codes.AlreadyExists), nil
						}
						<-ctx.Done()
						return nil, ctx.Err()
					}}, nil
				}, nil)
				s.Start(ctx)
				<-pending
				if shutdown == "cancel" {
					cancel()
				}
				s.Close()
				state, primary := s.State(), s.IsPrimary()
				close(release)
				synctest.Wait()
				assert.Equal(t, state, s.State(), "late arbitration changed the stopped session")
				assert.Equal(t, primary, s.IsPrimary())
				var states []State
				for ev := range s.Events() {
					states = append(states, ev.State)
				}
				assert.Equal(t, []State{StateConnecting, StatePrimary, StateDisconnected}, states)
			})
		})
	}
}

func TestSupervisorReconnectIgnoresOldResponses(t *testing.T) {
	for _, response := range []string{"arbitration", "packet"} {
		t.Run(response, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				pending := make(chan struct{})
				release := make(chan struct{})
				dials, packets := 0, 0
				s := New(Config{}, func(ctx context.Context) (p4v1.P4Runtime_StreamChannelClient, error) {
					dials++
					first := dials == 1
					calls := 0
					return &lifecycleStream{ctx: ctx,
						send: func(req *p4v1.StreamMessageRequest) error {
							if first && req.GetArbitration() == nil {
								return errors.New("stream send failed")
							}
							return nil
						},
						recv: func() (*p4v1.StreamMessageResponse, error) {
							calls++
							if calls == 1 {
								if first {
									return arbitrationResponse(codes.OK), nil
								}
								return arbitrationResponse(codes.AlreadyExists), nil
							}
							if first && calls == 2 {
								close(pending)
								<-release
								if response == "packet" {
									return packetResponse(), nil
								}
								return arbitrationResponse(codes.OK), nil
							}
							<-ctx.Done()
							return nil, ctx.Err()
						},
					}, nil
				}, func(*p4v1.StreamMessageResponse) { packets++ })
				s.Start(context.Background())
				<-pending
				require.ErrorContains(t, s.Send(context.Background(), &p4v1.StreamMessageRequest{}), "stream send failed")
				synctest.Wait()
				require.Equal(t, 2, dials)
				require.Equal(t, StateBackup, s.State())
				close(release)
				synctest.Wait()
				assert.Equal(t, StateBackup, s.State())
				assert.False(t, s.IsPrimary())
				assert.Zero(t, packets, "a canceled stream delivered a packet")
				s.Close()
			})
		})
	}
}

func TestSupervisorHandlerCanClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		messages := make(chan *p4v1.StreamMessageResponse, 3)
		messages <- arbitrationResponse(codes.OK)
		messages <- packetResponse()
		messages <- packetResponse()
		var s *Supervisor
		handled := 0
		s = New(Config{}, func(ctx context.Context) (p4v1.P4Runtime_StreamChannelClient, error) {
			return &lifecycleStream{ctx: ctx, recv: func() (*p4v1.StreamMessageResponse, error) {
				select {
				case msg := <-messages:
					return msg, nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}}, nil
		}, func(*p4v1.StreamMessageResponse) {
			s.Close()
			handled++
		})
		s.Start(context.Background())
		synctest.Wait()
		assert.Equal(t, 1, handled)
	})
}

func TestSupervisorCloseDuringArbitration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		pending := make(chan struct{})
		s := New(Config{ArbitrationTimeout: time.Minute}, func(ctx context.Context) (p4v1.P4Runtime_StreamChannelClient, error) {
			return &lifecycleStream{ctx: ctx, recv: func() (*p4v1.StreamMessageResponse, error) {
				close(pending)
				<-ctx.Done()
				return nil, ctx.Err()
			}}, nil
		}, nil)
		s.Start(context.Background())
		<-pending
		start := time.Now()
		s.Close()
		synctest.Wait()
		assert.Zero(t, time.Since(start), "Close waited for the arbitration timeout")
	})
}

func TestSupervisorConcurrentClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		pending := make(chan struct{})
		s := New(Config{}, func(ctx context.Context) (p4v1.P4Runtime_StreamChannelClient, error) {
			first := true
			return &lifecycleStream{ctx: ctx, recv: func() (*p4v1.StreamMessageResponse, error) {
				if first {
					first = false
					return arbitrationResponse(codes.OK), nil
				}
				close(pending)
				<-ctx.Done()
				return nil, ctx.Err()
			}}, nil
		}, nil)
		s.Start(context.Background())
		<-pending
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(s.Close)
		}
		wg.Wait()
		synctest.Wait()
		for range s.Events() {
		}
	})
}
