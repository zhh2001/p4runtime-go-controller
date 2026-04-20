package client

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	"google.golang.org/grpc"

	errs "github.com/zhh2001/p4runtime-go-controller/errors"
	"github.com/zhh2001/p4runtime-go-controller/internal/stream"
)

// Event mirrors stream.Event so callers never need to import the internal
// package. It is re-exported so consumers can subscribe via Client.Events.
type Event struct {
	State State
	Err   error
}

// State enumerates the observable mastership state of a Client.
type State int

const (
	// StateDisconnected indicates no live stream to the target.
	StateDisconnected State = iota
	// StateConnecting indicates a dial or arbitration is in flight.
	StateConnecting
	// StateBackup indicates the stream is up but another controller holds
	// primary.
	StateBackup
	// StatePrimary indicates this Client is the primary controller.
	StatePrimary
)

// String returns the snake_case name of the state.
func (s State) String() string {
	switch s {
	case StateDisconnected:
		return "disconnected"
	case StateConnecting:
		return "connecting"
	case StateBackup:
		return "backup"
	case StatePrimary:
		return "primary"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// Client is a long-lived P4Runtime controller session against a single
// target. It is safe for concurrent use by multiple goroutines.
type Client struct {
	opts   options
	conn   *grpc.ClientConn
	rpc    p4v1.P4RuntimeClient
	sup    *stream.Supervisor
	ctx    context.Context
	cancel context.CancelFunc

	events   chan Event
	closed   chan struct{}
	once     sync.Once
	dispatch *dispatchSlots
}

// Dial opens a Client against addr. It dials the gRPC target, starts the
// stream supervisor, and waits (bounded by the arbitration timeout) for the
// first MasterArbitrationUpdate response so callers know the connection is
// live when Dial returns.
func Dial(ctx context.Context, addr string, opts ...Option) (*Client, error) {
	o := defaultOptions()
	for _, fn := range opts {
		fn(&o)
	}
	if o.deviceID == 0 {
		return nil, fmt.Errorf("client.Dial: %w", errors.New("device ID required (use WithDeviceID)"))
	}
	if o.electionID.IsZero() {
		return nil, fmt.Errorf("client.Dial: %w", errs.ErrElectionIDZero)
	}

	conn, err := grpc.NewClient(addr, o.buildGRPCDialOptions()...)
	if err != nil {
		return nil, fmt.Errorf("grpc.NewClient: %w", err)
	}

	// The supervisor outlives the caller-supplied Dial context: the caller
	// wants Dial to return once arbitration settles, but the supervisor must
	// keep running until Client.Close tears it down. Deriving from
	// context.Background is intentional.
	supCtx, cancel := context.WithCancel(context.Background()) //nolint:contextcheck // supervisor outlives Dial ctx by design
	rpc := p4v1.NewP4RuntimeClient(conn)

	c := &Client{
		opts:     o,
		conn:     conn,
		rpc:      rpc,
		ctx:      supCtx,
		cancel:   cancel,
		events:   make(chan Event, 16),
		closed:   make(chan struct{}),
		dispatch: newDispatch(),
	}

	c.sup = stream.New(stream.Config{
		DeviceID:           o.deviceID,
		ElectionHigh:       o.electionID.High,
		ElectionLow:        o.electionID.Low,
		Role:               o.role,
		ArbitrationTimeout: o.arbitrationTO,
		BackoffInitial:     o.backoffInitial,
		BackoffMax:         o.backoffMax,
		Logger:             o.logger,
	}, c.dial, c.receiveHandler)

	go c.forwardEvents()
	c.sup.Start(supCtx)

	// Wait for either the first transition out of connecting or the deadline.
	dialDeadline := time.NewTimer(o.arbitrationTO + 2*time.Second)
	defer dialDeadline.Stop()
	for {
		s := c.sup.State()
		if s == stream.StatePrimary || s == stream.StateBackup {
			return c, nil
		}
		select {
		case <-ctx.Done():
			_ = c.Close()
			return nil, ctx.Err()
		case <-dialDeadline.C:
			_ = c.Close()
			return nil, fmt.Errorf("client.Dial: %w", errs.ErrArbitrationFailed)
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func (c *Client) dial(ctx context.Context) (p4v1.P4Runtime_StreamChannelClient, error) {
	return c.rpc.StreamChannel(ctx)
}

func (c *Client) forwardEvents() {
	defer close(c.events)
	for ev := range c.sup.Events() {
		select {
		case c.events <- Event{State: State(ev.State), Err: ev.Err}:
		default:
		}
	}
}

// Close tears the client down. It cancels the supervisor, closes the
// bidirectional stream, and closes the gRPC connection. It is safe to call
// multiple times; only the first invocation does work.
func (c *Client) Close() error {
	var closeErr error
	c.once.Do(func() {
		c.cancel()
		if c.sup != nil {
			c.sup.Close()
		}
		if c.conn != nil {
			closeErr = c.conn.Close()
		}
		close(c.closed)
	})
	return closeErr
}

// DeviceID returns the device_id this client is bound to.
func (c *Client) DeviceID() uint64 { return c.opts.deviceID }

// ElectionID returns the election ID currently used for arbitration.
func (c *Client) ElectionID() ElectionID { return c.opts.electionID }

// State returns the observable mastership state.
func (c *Client) State() State { return State(c.sup.State()) }

// IsPrimary reports whether this client is the primary controller for the
// device.
func (c *Client) IsPrimary() bool { return c.sup.IsPrimary() }

// Events returns a receive-only channel of mastership transitions. The
// channel is closed after Close.
func (c *Client) Events() <-chan Event { return c.events }

// BecomePrimary blocks until the client observes StatePrimary or ctx is done.
// It returns nil on success, ctx.Err() on cancellation, or ErrNotPrimary if
// Close is invoked while waiting.
func (c *Client) BecomePrimary(ctx context.Context) error {
	if c.IsPrimary() {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-c.events:
			if !ok {
				return errs.ErrStreamClosed
			}
			if ev.State == StatePrimary {
				return nil
			}
		case <-c.closed:
			return errs.ErrStreamClosed
		}
	}
}

// Conn exposes the underlying gRPC connection. Advanced callers can build
// additional stubs (for example gNMI) on top of it. The Client remains the
// owner; do not call Close on the returned conn.
func (c *Client) Conn() *grpc.ClientConn { return c.conn }

// RPC exposes the P4RuntimeClient stub. Callers that need to use unary RPCs
// the SDK does not wrap (yet) can reach the stub directly.
func (c *Client) RPC() p4v1.P4RuntimeClient { return c.rpc }
