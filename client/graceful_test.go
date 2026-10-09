package client

import (
	"context"
	"io"
	"testing"
	"testing/synctest"
	"time"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	errs "github.com/zhh2001/p4runtime-go-controller/v2/errors"
	"github.com/zhh2001/p4runtime-go-controller/v2/internal/stream"
)

type gracefulClientStream struct {
	grpc.ClientStream
	ctx    context.Context
	first  bool
	half   chan struct{}
	final  <-chan error
	closes int
}

func (s *gracefulClientStream) Send(*p4v1.StreamMessageRequest) error { return nil }

func (s *gracefulClientStream) CloseSend() error {
	s.closes++
	if s.closes == 1 {
		close(s.half)
	}
	return nil
}

func (s *gracefulClientStream) Recv() (*p4v1.StreamMessageResponse, error) {
	if s.first {
		s.first = false
		return primaryWaitResponse(codes.OK), nil
	}
	select {
	case err := <-s.final:
		return nil, err
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

func newGracefulClient() (*Client, *gracefulClientStream, chan<- error) {
	ctx, cancel := context.WithCancel(context.Background())
	final := make(chan error, 1)
	fake := &gracefulClientStream{half: make(chan struct{}), final: final, first: true}
	c := &Client{ctx: ctx, cancel: cancel, events: make(chan Event, 16), closed: make(chan struct{}), dispatch: newDispatch()}
	c.sup = stream.New(stream.Config{}, func(ctx context.Context) (p4v1.P4Runtime_StreamChannelClient, error) {
		fake.ctx = ctx
		return fake, nil
	}, c.receiveHandler)
	go c.forwardEvents()
	c.sup.Start(ctx)
	synctest.Wait()
	return c, fake, final
}

func TestCloseGracefullyConcurrentCalls(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, fake, final := newGracefulClient()
		defer c.Close()
		require.NoError(t, c.SendPacketOut(context.Background(), &p4v1.PacketOut{Payload: []byte{1}}))
		done := make(chan error, 8)
		for range cap(done) {
			go func() { done <- c.CloseGracefully(context.Background()) }()
		}
		<-fake.half
		synctest.Wait()
		require.Empty(t, done, "close completed before the target's final status")
		require.ErrorIs(t, c.SendDigestAck(context.Background(), &p4v1.DigestListAck{}), errs.ErrStreamClosed)
		final <- io.EOF
		for range cap(done) {
			require.NoError(t, <-done)
		}
		require.NoError(t, c.CloseGracefully(context.Background()))
		assert.Equal(t, 1, fake.closes)
		assert.False(t, c.IsPrimary())
		assert.Equal(t, StateDisconnected, c.State())
		assert.ErrorIs(t, c.BecomePrimary(context.Background()), errs.ErrStreamClosed)
		for range c.Events() {
		}
	})
}

func TestCloseGracefullyAlwaysReleasesClient(t *testing.T) {
	for _, mode := range []string{"deadline", "canceled", "target status", "Close"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c, fake, final := newGracefulClient()
				defer c.Close()
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if mode == "canceled" {
					cancel()
				}
				done := make(chan error, 1)
				go func() { done <- c.CloseGracefully(ctx) }()
				if mode != "canceled" {
					<-fake.half
				}
				var want error = context.DeadlineExceeded
				switch mode {
				case "canceled":
					want = context.Canceled
				case "target status":
					want = status.Error(codes.PermissionDenied, "target denied request")
					final <- want
				case "Close":
					want = errs.ErrStreamClosed
					require.NoError(t, c.Close())
				}
				err := <-done
				require.ErrorIs(t, err, want)
				if mode == "target status" {
					assert.Equal(t, codes.PermissionDenied, status.Code(err))
				}
				assert.ErrorIs(t, c.ctx.Err(), context.Canceled)
				assert.False(t, c.IsPrimary())
				for range c.Events() {
				}
			})
		})
	}
}
