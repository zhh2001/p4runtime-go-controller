package client

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	errs "github.com/zhh2001/p4runtime-go-controller/v2/errors"
	"github.com/zhh2001/p4runtime-go-controller/v2/internal/stream"
)

type primaryWaitStream struct {
	grpc.ClientStream
	ctx      context.Context
	messages <-chan *p4v1.StreamMessageResponse
}

func (s *primaryWaitStream) Context() context.Context { return s.ctx }

func (s *primaryWaitStream) Send(*p4v1.StreamMessageRequest) error { return nil }

func (s *primaryWaitStream) Recv() (*p4v1.StreamMessageResponse, error) {
	select {
	case msg := <-s.messages:
		return msg, nil
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

func primaryWaitResponse(code codes.Code) *p4v1.StreamMessageResponse {
	return &p4v1.StreamMessageResponse{
		Update: &p4v1.StreamMessageResponse_Arbitration{
			Arbitration: &p4v1.MasterArbitrationUpdate{
				Status: &rpcstatus.Status{Code: int32(code)},
			},
		},
	}
}

func newPrimaryWaitClient(initial codes.Code) (*Client, chan<- *p4v1.StreamMessageResponse) {
	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan *p4v1.StreamMessageResponse, 1)
	messages <- primaryWaitResponse(initial)
	c := &Client{
		ctx:    ctx,
		cancel: cancel,
		events: make(chan Event, 16),
		closed: make(chan struct{}),
	}
	c.sup = stream.New(stream.Config{}, func(ctx context.Context) (p4v1.P4Runtime_StreamChannelClient, error) {
		return &primaryWaitStream{ctx: ctx, messages: messages}, nil
	}, nil)
	go c.forwardEvents()
	c.sup.Start(ctx)
	synctest.Wait()
	return c, messages
}

func primaryWaiters(c *Client, ctx context.Context, count int) <-chan error {
	results := make(chan error, count)
	for range count {
		go func() { results <- c.BecomePrimary(ctx) }()
	}
	synctest.Wait()
	return results
}

func TestBecomePrimaryIgnoresPastPrimary(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, messages := newPrimaryWaitClient(codes.OK)
		defer c.Close()
		messages <- primaryWaitResponse(codes.AlreadyExists)
		synctest.Wait()
		require.Equal(t, StateBackup, c.State())
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		results := primaryWaiters(c, ctx, 1)
		assert.Len(t, results, 0, "an old primary event completed the wait")
		time.Sleep(time.Second)
		assert.ErrorIs(t, <-results, context.DeadlineExceeded)
	})
}

func TestBecomePrimaryConcurrentWaiters(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, messages := newPrimaryWaitClient(codes.AlreadyExists)
		defer c.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		const count = 32
		results := primaryWaiters(c, ctx, count)
		messages <- primaryWaitResponse(codes.OK)
		synctest.Wait()
		for range count {
			assert.NoError(t, <-results)
		}
	})
}

func TestBecomePrimaryWithEventsConsumer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, messages := newPrimaryWaitClient(codes.AlreadyExists)
		defer c.Close()
		var observed []State
		go func() {
			for ev := range c.Events() {
				observed = append(observed, ev.State)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		const count = 16
		results := primaryWaiters(c, ctx, count)
		messages <- primaryWaitResponse(codes.OK)
		synctest.Wait()
		for range count {
			assert.NoError(t, <-results)
		}
		synctest.Wait()
		assert.Equal(t, []State{StateConnecting, StateBackup, StatePrimary}, observed)
	})
}

func TestBecomePrimaryWithFullEventsQueue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, messages := newPrimaryWaitClient(codes.AlreadyExists)
		defer c.Close()
		for range 16 {
			messages <- primaryWaitResponse(codes.OK)
			synctest.Wait()
			messages <- primaryWaitResponse(codes.AlreadyExists)
			synctest.Wait()
		}
		require.Equal(t, StateBackup, c.State())
		require.Len(t, c.events, cap(c.events))
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		const count = 8
		results := primaryWaiters(c, ctx, count)
		assert.Len(t, results, 0)
		assert.Len(t, c.events, cap(c.events), "waiters consumed public events")
		messages <- primaryWaitResponse(codes.OK)
		synctest.Wait()
		for range count {
			assert.NoError(t, <-results)
		}
		assert.Len(t, c.events, cap(c.events))
	})
}

func TestBecomePrimaryCancellationIsIndependent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, messages := newPrimaryWaitClient(codes.AlreadyExists)
		defer c.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		const count = 8
		results := primaryWaiters(c, ctx, count)
		oneCtx, stopOne := context.WithCancel(context.Background())
		one := primaryWaiters(c, oneCtx, 1)
		stopOne()
		synctest.Wait()
		assert.ErrorIs(t, <-one, context.Canceled)
		assert.Len(t, results, 0)
		messages <- primaryWaitResponse(codes.OK)
		synctest.Wait()
		for range count {
			assert.NoError(t, <-results)
		}
	})
}

func TestBecomePrimaryCloseWakesAllWaiters(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, _ := newPrimaryWaitClient(codes.AlreadyExists)
		defer c.Close()
		const count = 16
		results := primaryWaiters(c, context.Background(), count)
		require.NoError(t, c.Close())
		for range count {
			assert.ErrorIs(t, <-results, errs.ErrStreamClosed)
		}
	})
}
