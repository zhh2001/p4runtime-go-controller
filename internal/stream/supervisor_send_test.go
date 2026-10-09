package stream

import (
	"context"
	"errors"
	"io"
	"testing"
	"testing/synctest"
	"time"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	errs "github.com/zhh2001/p4runtime-go-controller/v2/errors"
)

type sendControl struct {
	halfClosed chan struct{}
	final      chan error
	messages   chan *p4v1.StreamMessageResponse
	closes     int
}

func startSendSupervisor(t *testing.T, send func(context.Context, *p4v1.StreamMessageRequest) error, closeErr error, handler PacketHandler) (*Supervisor, *sendControl) {
	t.Helper()
	h := &sendControl{halfClosed: make(chan struct{}), final: make(chan error, 1), messages: make(chan *p4v1.StreamMessageResponse, 8)}
	s := New(Config{}, func(ctx context.Context) (p4v1.P4Runtime_StreamChannelClient, error) {
		first := true
		return &lifecycleStream{
			ctx: ctx,
			send: func(req *p4v1.StreamMessageRequest) error {
				if req.GetArbitration() != nil || send == nil {
					return nil
				}
				return send(ctx, req)
			},
			closeSend: func() error {
				h.closes++
				if h.closes == 1 {
					close(h.halfClosed)
				}
				return closeErr
			},
			recv: func() (*p4v1.StreamMessageResponse, error) {
				if first {
					first = false
					return arbitrationResponse(codes.OK), nil
				}
				select {
				case err := <-h.final:
					return nil, err
				case message := <-h.messages:
					return message, nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			},
		}, nil
	}, handler)
	s.Start(context.Background())
	synctest.Wait()
	require.True(t, s.IsPrimary())
	t.Cleanup(s.Close)
	return s, h
}

func sendPacket(marker byte) *p4v1.StreamMessageRequest {
	return &p4v1.StreamMessageRequest{Update: &p4v1.StreamMessageRequest_Packet{Packet: &p4v1.PacketOut{Payload: []byte{marker}}}}
}

func TestSupervisorSendWaitsForTransport(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		s, _ := startSendSupervisor(t, func(ctx context.Context, _ *p4v1.StreamMessageRequest) error {
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}, nil, nil)
		done := make(chan error, 1)
		go func() { done <- s.Send(context.Background(), sendPacket(1)) }()
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("Send returned before the transport completed: %v", err)
		default:
		}
		close(release)
		require.NoError(t, <-done)
	})
}

func TestSupervisorSendReturnsTransportError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		failed := status.Error(codes.Unavailable, "transport failed")
		calls := 0
		s, _ := startSendSupervisor(t, func(context.Context, *p4v1.StreamMessageRequest) error {
			calls++
			return failed
		}, nil, nil)
		require.ErrorIs(t, s.Send(context.Background(), sendPacket(1)), failed)
		s.Close()
		assert.Equal(t, 1, calls, "failed send was replayed")
	})
}

func TestSupervisorSendFailureStopsPendingRequests(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		failed := status.Error(codes.Unavailable, "stream failed")
		calls := 0
		s, h := startSendSupervisor(t, func(ctx context.Context, _ *p4v1.StreamMessageRequest) error {
			calls++
			select {
			case <-release:
				return failed
			case <-ctx.Done():
				return ctx.Err()
			}
		}, nil, nil)
		done := make(chan error, 3)
		go func() { done <- s.Send(context.Background(), sendPacket(1)) }()
		synctest.Wait()
		go func() { done <- s.Send(context.Background(), sendPacket(2)) }()
		synctest.Wait()
		go func() { done <- s.CloseGracefully(context.Background()) }()
		synctest.Wait()
		close(release)
		for range cap(done) {
			require.ErrorIs(t, <-done, failed)
		}
		s.Close()
		assert.Equal(t, 1, calls, "queued request was sent after the stream failed")
		assert.Zero(t, h.closes, "half-close started after a prior send failed")
	})
}

func TestSupervisorSendRejectsNilAndBackup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		s, _ := startSendSupervisor(t, func(context.Context, *p4v1.StreamMessageRequest) error {
			calls++
			return nil
		}, nil, nil)
		require.ErrorContains(t, s.Send(context.Background(), nil), "nil request")
		s.setState(context.Background(), StateBackup, nil)
		require.ErrorIs(t, s.Send(context.Background(), sendPacket(1)), errs.ErrNotPrimary)
		s.Close()
		require.ErrorIs(t, s.CloseGracefully(context.Background()), errStopped)
		assert.Zero(t, calls)
	})
}

func TestSupervisorSendCancellation(t *testing.T) {
	for _, mode := range []string{"before enqueue", "queued", "in transport"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				release := make(chan struct{})
				var sent []byte
				s, _ := startSendSupervisor(t, func(ctx context.Context, req *p4v1.StreamMessageRequest) error {
					sent = append(sent, req.GetPacket().Payload[0])
					select {
					case <-release:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				}, nil, nil)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				first := make(chan error, 1)
				if mode == "queued" {
					go func() { first <- s.Send(context.Background(), sendPacket(1)) }()
					synctest.Wait()
				}
				if mode == "before enqueue" {
					cancel()
				}
				done := make(chan error, 1)
				go func() { done <- s.Send(ctx, sendPacket(2)) }()
				synctest.Wait()
				cancel()
				assert.ErrorIs(t, <-done, context.Canceled)
				close(release)
				if mode == "queued" {
					require.NoError(t, <-first)
				}
				s.Close()
				want := []byte(nil)
				if mode == "queued" {
					want = []byte{1}
				} else if mode == "in transport" {
					want = []byte{2}
				}
				assert.Equal(t, want, sent)
			})
		})
	}
}

func TestSupervisorSendDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, _ := startSendSupervisor(t, func(ctx context.Context, _ *p4v1.StreamMessageRequest) error {
			<-ctx.Done()
			return ctx.Err()
		}, nil, nil)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		assert.ErrorIs(t, s.Send(ctx, sendPacket(1)), context.DeadlineExceeded)
		s.Close()
	})
}

func TestSupervisorCloseUnblocksTransportAndQueue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, _ := startSendSupervisor(t, func(ctx context.Context, _ *p4v1.StreamMessageRequest) error {
			<-ctx.Done()
			return ctx.Err()
		}, nil, nil)
		done := make(chan error, 24)
		for i := range cap(done) {
			go func() { done <- s.Send(context.Background(), sendPacket(byte(i))) }()
		}
		synctest.Wait()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		assert.ErrorIs(t, s.CloseGracefully(ctx), context.DeadlineExceeded)
		start := time.Now()
		s.Close()
		assert.Zero(t, time.Since(start), "Close did not cancel a blocked transport")
		for range cap(done) {
			err := <-done
			assert.True(t, errors.Is(err, errStopped) || errors.Is(err, context.Canceled), "unexpected send result: %v", err)
		}
	})
}

func TestSupervisorGracefulCloseDrainsRequests(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		var sent []byte
		s, h := startSendSupervisor(t, func(ctx context.Context, req *p4v1.StreamMessageRequest) error {
			select {
			case <-release:
				sent = append(sent, req.GetPacket().Payload[0])
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}, nil, nil)
		sends := make(chan error, 2)
		go func() { sends <- s.Send(context.Background(), sendPacket(1)) }()
		synctest.Wait()
		go func() { sends <- s.Send(context.Background(), sendPacket(2)) }()
		synctest.Wait()
		done := make(chan error, 8)
		for range cap(done) {
			go func() { done <- s.CloseGracefully(context.Background()) }()
		}
		synctest.Wait()
		require.ErrorIs(t, s.Send(context.Background(), sendPacket(3)), errStopped)
		assert.Zero(t, h.closes, "half-close overlapped an unfinished Send")
		close(release)
		for range cap(sends) {
			require.NoError(t, <-sends)
		}
		<-h.halfClosed
		synctest.Wait()
		require.Equal(t, []byte{1, 2}, sent)
		select {
		case err := <-done:
			t.Fatalf("CloseGracefully returned before final status: %v", err)
		default:
		}
		h.final <- io.EOF
		for range cap(done) {
			require.NoError(t, <-done)
		}
		require.NoError(t, s.CloseGracefully(context.Background()))
		assert.Equal(t, 1, h.closes)
	})
}

func TestSupervisorGracefulCloseFinalStatus(t *testing.T) {
	for _, final := range []error{io.EOF, status.Error(codes.PermissionDenied, "target rejected request")} {
		t.Run(final.Error(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				handled := 0
				s, h := startSendSupervisor(t, nil, nil, func(*p4v1.StreamMessageResponse) { handled++ })
				done := make(chan error, 1)
				go func() { done <- s.CloseGracefully(context.Background()) }()
				<-h.halfClosed
				h.messages <- packetResponse()
				h.messages <- arbitrationResponse(codes.AlreadyExists)
				synctest.Wait()
				h.final <- final
				err := <-done
				if errors.Is(final, io.EOF) {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, final)
					assert.Equal(t, codes.PermissionDenied, status.Code(err))
				}
				s.Close()
				assert.Equal(t, 1, handled)
			})
		})
	}
}

func TestSupervisorGracefulCloseHalfCloseError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		failed := errors.New("half-close failed")
		s, _ := startSendSupervisor(t, nil, failed, nil)
		assert.ErrorIs(t, s.CloseGracefully(context.Background()), failed)
	})
}

func TestSupervisorGracefulCloseTimeoutAndAbort(t *testing.T) {
	for _, mode := range []string{"deadline", "Close"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s, h := startSendSupervisor(t, nil, nil, nil)
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				done := make(chan error, 1)
				go func() { done <- s.CloseGracefully(ctx) }()
				<-h.halfClosed
				if mode == "Close" {
					s.Close()
					assert.ErrorIs(t, <-done, errStopped)
				} else {
					assert.ErrorIs(t, <-done, context.DeadlineExceeded)
				}
			})
		})
	}
}

func TestSupervisorGracefulCloseCanceledTransport(t *testing.T) {
	for _, phase := range []string{"half-close", "receive"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			recvErr := make(chan error, 1)
			transport := &lifecycleStream{closeSend: func() error {
				cancel()
				if phase == "half-close" {
					return context.Canceled
				}
				recvErr <- context.Canceled
				return nil
			}}
			s := New(Config{}, nil, nil)
			require.ErrorIs(t, s.finishStream(ctx, transport, recvErr, nil), errStopped)
		})
	}
}
