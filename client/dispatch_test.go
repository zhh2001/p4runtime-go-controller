package client

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/require"
)

type dispatchCase struct {
	name      string
	message   *p4v1.StreamMessageResponse
	subscribe func(*dispatchSlots, func()) func()
}

func dispatchCases() []dispatchCase {
	return []dispatchCase{
		{
			name:    "packet",
			message: &p4v1.StreamMessageResponse{Update: &p4v1.StreamMessageResponse_Packet{Packet: &p4v1.PacketIn{}}},
			subscribe: func(d *dispatchSlots, h func()) func() {
				return d.addPacketIn(func(context.Context, *p4v1.PacketIn) { h() })
			},
		},
		{
			name:    "digest",
			message: &p4v1.StreamMessageResponse{Update: &p4v1.StreamMessageResponse_Digest{Digest: &p4v1.DigestList{}}},
			subscribe: func(d *dispatchSlots, h func()) func() {
				return d.addDigestList(func(context.Context, *p4v1.DigestList) { h() })
			},
		},
		{
			name:    "idle",
			message: &p4v1.StreamMessageResponse{Update: &p4v1.StreamMessageResponse_IdleTimeoutNotification{IdleTimeoutNotification: &p4v1.IdleTimeoutNotification{}}},
			subscribe: func(d *dispatchSlots, h func()) func() {
				return d.addIdle(func(context.Context, *p4v1.IdleTimeoutNotification) { h() })
			},
		},
		{
			name:    "stream",
			message: &p4v1.StreamMessageResponse{},
			subscribe: func(d *dispatchSlots, h func()) func() {
				return d.addStream(func(context.Context, *p4v1.StreamMessageResponse) { h() })
			},
		},
	}
}

func awaitDispatch(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("subscription change blocked while a handler was running")
	}
}

func runDispatch(t *testing.T, d *dispatchSlots, msg *p4v1.StreamMessageResponse) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		d.dispatch(context.Background(), msg)
		close(done)
	}()
	awaitDispatch(t, done)
}

func TestDispatch_SubscriptionChangesInsideHandlers(t *testing.T) {
	for _, tc := range dispatchCases() {
		for _, operation := range []string{"cancel", "register"} {
			t.Run(tc.name+"/"+operation, func(t *testing.T) {
				d := newDispatch()
				calls, newCalls := 0, 0
				var off func()
				off = tc.subscribe(d, func() {
					calls++
					if operation == "cancel" {
						off()
						off()
					} else if calls == 1 {
						tc.subscribe(d, func() { newCalls++ })
					}
				})
				runDispatch(t, d, tc.message)
				require.Equal(t, 1, calls)
				require.Zero(t, newCalls)
				runDispatch(t, d, tc.message)
				if operation == "cancel" {
					require.Equal(t, 1, calls)
				} else {
					require.Equal(t, 2, calls)
					require.Equal(t, 1, newCalls)
				}
			})
		}
	}
}

func TestDispatch_SnapshotsHandlersBeforeCallbacks(t *testing.T) {
	for _, tc := range dispatchCases()[:3] {
		t.Run(tc.name, func(t *testing.T) {
			d := newDispatch()
			var order []string
			offTyped := tc.subscribe(d, func() { order = append(order, "original") })
			var offStream func()
			offStream = d.addStream(func(context.Context, *p4v1.StreamMessageResponse) {
				order = append(order, "stream")
				offTyped()
				tc.subscribe(d, func() { order = append(order, "replacement") })
				offStream()
			})
			runDispatch(t, d, tc.message)
			require.Equal(t, []string{"stream", "original"}, order)
			runDispatch(t, d, tc.message)
			require.Equal(t, []string{"stream", "original", "replacement"}, order)
		})
	}
}

func TestDispatch_SubscriptionChangesDuringHandler(t *testing.T) {
	for _, tc := range dispatchCases() {
		t.Run(tc.name, func(t *testing.T) {
			d := newDispatch()
			entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			off := tc.subscribe(d, func() {
				close(entered)
				<-release
			})
			go func() {
				d.dispatch(context.Background(), tc.message)
				close(done)
			}()
			t.Cleanup(func() {
				close(release)
				awaitDispatch(t, done)
			})
			awaitDispatch(t, entered)
			changed := make(chan struct{})
			go func() {
				cancel := tc.subscribe(d, func() {})
				off()
				cancel()
				close(changed)
			}()
			awaitDispatch(t, changed)
		})
	}
}

func TestDispatch_ConcurrentSubscriptionChanges(t *testing.T) {
	for _, tc := range dispatchCases() {
		t.Run(tc.name, func(t *testing.T) {
			d := newDispatch()
			var count atomic.Int64
			tc.subscribe(d, func() { count.Add(1) })
			var workers sync.WaitGroup
			for range 8 {
				workers.Go(func() {
					for range 50 {
						off := tc.subscribe(d, func() {})
						d.dispatch(context.Background(), tc.message)
						off()
					}
				})
			}
			workers.Wait()
			require.Equal(t, int64(400), count.Load())
		})
	}
}
