package stream

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func TestSupervisorShutdownRevokesMastership(t *testing.T) {
	for _, shutdown := range []string{"close", "cancel"} {
		t.Run(shutdown, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				s := New(Config{}, func(ctx context.Context) (p4v1.P4Runtime_StreamChannelClient, error) {
					first := true
					return &lifecycleStream{ctx: ctx, recv: func() (*p4v1.StreamMessageResponse, error) {
						if first {
							first = false
							return arbitrationResponse(codes.OK), nil
						}
						<-ctx.Done()
						return nil, ctx.Err()
					}}, nil
				}, nil)
				s.Start(ctx)
				synctest.Wait()
				require.True(t, s.IsPrimary())
				if shutdown == "cancel" {
					cancel()
					synctest.Wait()
				} else {
					s.Close()
				}
				assert.Equal(t, StateDisconnected, s.State())
				assert.False(t, s.IsPrimary())
				sendCtx, stopSend := context.WithTimeout(context.Background(), time.Second)
				defer stopSend()
				for range 32 {
					assert.ErrorIs(t, s.Send(sendCtx, &p4v1.StreamMessageRequest{}), errStopped)
				}
				s.Close()
				var states []State
				for ev := range s.Events() {
					states = append(states, ev.State)
				}
				assert.Equal(t, []State{StateConnecting, StatePrimary, StateDisconnected}, states)
			})
		})
	}
}

func TestSupervisorShutdownUnblocksSend(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s := New(Config{}, func(ctx context.Context) (p4v1.P4Runtime_StreamChannelClient, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}, nil)
		s.Start(ctx)
		for range cap(s.sendCh) {
			require.NoError(t, s.Send(context.Background(), &p4v1.StreamMessageRequest{}))
		}
		done := make(chan error, 1)
		go func() {
			done <- s.Send(context.Background(), &p4v1.StreamMessageRequest{})
		}()
		synctest.Wait()
		cancel()
		assert.ErrorIs(t, <-done, errStopped)
		s.Close()
	})
}

func TestSupervisorReconnectRevokesMastership(t *testing.T) {
	for _, failure := range []string{"recv", "send"} {
		for _, role := range []string{"primary", "backup"} {
			t.Run(failure+"/"+role, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					failed := errors.New("stream failed")
					failRecv := make(chan struct{})
					allowDial := make(chan struct{})
					allowReply := make(chan struct{})
					dials := 0
					s := New(Config{}, func(ctx context.Context) (p4v1.P4Runtime_StreamChannelClient, error) {
						dials++
						initial := dials == 1
						if !initial {
							select {
							case <-allowDial:
							case <-ctx.Done():
								return nil, ctx.Err()
							}
						}
						first := true
						return &lifecycleStream{ctx: ctx,
							send: func(req *p4v1.StreamMessageRequest) error {
								if initial && req.GetArbitration() == nil {
									return failed
								}
								return nil
							},
							recv: func() (*p4v1.StreamMessageResponse, error) {
								if first {
									first = false
									if initial {
										return arbitrationResponse(codes.OK), nil
									}
									select {
									case <-allowReply:
									case <-ctx.Done():
										return nil, ctx.Err()
									}
									if role == "backup" {
										return arbitrationResponse(codes.AlreadyExists), nil
									}
									return arbitrationResponse(codes.OK), nil
								}
								select {
								case <-failRecv:
									if initial {
										return nil, failed
									}
									<-ctx.Done()
									return nil, ctx.Err()
								case <-ctx.Done():
									return nil, ctx.Err()
								}
							},
						}, nil
					}, nil)
					s.Start(ctx)
					synctest.Wait()
					require.True(t, s.IsPrimary())
					if failure == "recv" {
						close(failRecv)
					} else {
						require.NoError(t, s.Send(context.Background(), &p4v1.StreamMessageRequest{}))
					}
					synctest.Wait()
					require.Equal(t, 2, dials)
					assert.Equal(t, StateConnecting, s.State())
					assert.False(t, s.IsPrimary(), "mastership survived a stream failure")
					close(allowDial)
					synctest.Wait()
					assert.Equal(t, StateConnecting, s.State())
					assert.False(t, s.IsPrimary(), "mastership restored before arbitration")
					close(allowReply)
					synctest.Wait()
					want := StatePrimary
					if role == "backup" {
						want = StateBackup
					}
					assert.Equal(t, want, s.State())
					assert.Equal(t, role == "primary", s.IsPrimary())
					s.Close()
					var states []State
					for ev := range s.Events() {
						states = append(states, ev.State)
						if ev.Err != nil {
							assert.ErrorIs(t, ev.Err, failed)
						}
					}
					assert.Equal(t, []State{StateConnecting, StatePrimary, StateDisconnected, StateConnecting, want, StateDisconnected}, states)
				})
			})
		}
	}
}
