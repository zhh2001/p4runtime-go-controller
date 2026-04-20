package stream

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
)

// State is the observable mastership state of a P4Runtime StreamChannel.
type State int

const (
	// StateDisconnected is the starting state and the state after the
	// stream drops before a reconnect attempt fires.
	StateDisconnected State = iota
	// StateConnecting is the state during gRPC stream establishment and
	// the pre-arbitration handshake.
	StateConnecting
	// StateBackup is the state when the target has accepted the
	// arbitration exchange but a higher election ID holds primary.
	StateBackup
	// StatePrimary is the state when this client is the primary
	// controller for the device.
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

// Event is published whenever the supervisor's state transitions. Transport
// errors that triggered a disconnect are attached in Err.
type Event struct {
	State State
	Err   error
}

// Config bundles every supervisor tunable. The client package wires defaults
// through its own Options type; Config stays minimal here on purpose so the
// supervisor can be unit-tested without dragging in client-level plumbing.
type Config struct {
	DeviceID           uint64
	ElectionHigh       uint64
	ElectionLow        uint64
	Role               string
	ArbitrationTimeout time.Duration
	BackoffInitial     time.Duration
	BackoffMax         time.Duration
	Logger             *slog.Logger
}

// Dialer opens a fresh bidirectional P4Runtime stream. The supervisor calls
// it on startup and after every disconnect.
type Dialer func(ctx context.Context) (p4v1.P4Runtime_StreamChannelClient, error)

// PacketHandler is called for every StreamMessageResponse that is not a
// MasterArbitrationUpdate. nil disables delivery.
type PacketHandler func(msg *p4v1.StreamMessageResponse)

// Supervisor owns a single P4Runtime StreamChannel. It re-arbitrates on
// reconnect and publishes state transitions on an Events channel. Methods are
// safe for concurrent use.
type Supervisor struct {
	cfg   Config
	dial  Dialer
	onPkt PacketHandler
	log   *slog.Logger

	mu      sync.RWMutex
	state   State
	primary bool
	lastErr error

	events  chan Event
	sendCh  chan *p4v1.StreamMessageRequest
	stop    chan struct{}
	stopped chan struct{}
	once    sync.Once
}

// New constructs a Supervisor. Start() must be called before any observable
// behavior happens.
func New(cfg Config, dial Dialer, onPkt PacketHandler) *Supervisor {
	if cfg.ArbitrationTimeout <= 0 {
		cfg.ArbitrationTimeout = 10 * time.Second
	}
	if cfg.BackoffInitial <= 0 {
		cfg.BackoffInitial = 500 * time.Millisecond
	}
	if cfg.BackoffMax <= 0 {
		cfg.BackoffMax = 30 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Supervisor{
		cfg:     cfg,
		dial:    dial,
		onPkt:   onPkt,
		log:     cfg.Logger,
		state:   StateDisconnected,
		events:  make(chan Event, 16),
		sendCh:  make(chan *p4v1.StreamMessageRequest, 16),
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

// Events returns a receive-only channel of state transitions. The channel is
// closed when the supervisor fully stops.
func (s *Supervisor) Events() <-chan Event { return s.events }

// State returns the current observable state.
func (s *Supervisor) State() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// IsPrimary reports whether the supervisor currently holds primary mastership
// for the device.
func (s *Supervisor) IsPrimary() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.primary
}

// Send enqueues a StreamMessageRequest for the send goroutine. The request is
// sent on whichever stream is currently connected; buffered requests queued
// during a reconnect are drained onto the new stream after arbitration
// completes.
func (s *Supervisor) Send(ctx context.Context, req *p4v1.StreamMessageRequest) error {
	select {
	case s.sendCh <- req:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.stop:
		return errStopped
	}
}

var errStopped = errors.New("stream supervisor stopped")

// Start launches the supervisor goroutine. It returns immediately. Use
// Events() to observe transitions, and Close() to stop.
func (s *Supervisor) Start(ctx context.Context) {
	go s.run(ctx)
}

// Close shuts the supervisor down and waits for the goroutine to exit.
func (s *Supervisor) Close() {
	s.once.Do(func() { close(s.stop) })
	<-s.stopped
}

func (s *Supervisor) run(parent context.Context) {
	defer close(s.events)
	defer close(s.stopped)

	backoff := s.cfg.BackoffInitial
	for {
		if err := parent.Err(); err != nil {
			return
		}
		select {
		case <-s.stop:
			return
		default:
		}

		s.setState(StateConnecting, nil)

		ctx, cancel := context.WithCancel(parent)
		stream, err := s.dial(ctx)
		if err != nil {
			cancel()
			s.setState(StateDisconnected, err)
			if !s.sleep(parent, backoff) {
				return
			}
			backoff = nextBackoff(backoff, s.cfg.BackoffMax)
			continue
		}

		if err := s.arbitrate(ctx, stream); err != nil {
			cancel()
			s.setState(StateDisconnected, err)
			if !s.sleep(parent, backoff) {
				return
			}
			backoff = nextBackoff(backoff, s.cfg.BackoffMax)
			continue
		}

		// Successful arbitration → reset backoff and pump the stream.
		backoff = s.cfg.BackoffInitial
		s.serve(ctx, stream)
		cancel()
	}
}

func (s *Supervisor) arbitrate(ctx context.Context, stream p4v1.P4Runtime_StreamChannelClient) error {
	arb := &p4v1.StreamMessageRequest{
		Update: &p4v1.StreamMessageRequest_Arbitration{
			Arbitration: &p4v1.MasterArbitrationUpdate{
				DeviceId: s.cfg.DeviceID,
				ElectionId: &p4v1.Uint128{
					High: s.cfg.ElectionHigh,
					Low:  s.cfg.ElectionLow,
				},
			},
		},
	}
	if s.cfg.Role != "" {
		arb.GetArbitration().Role = &p4v1.Role{Name: s.cfg.Role}
	}
	if err := stream.Send(arb); err != nil {
		return fmt.Errorf("send arbitration: %w", err)
	}

	deadline, cancel := context.WithTimeout(ctx, s.cfg.ArbitrationTimeout)
	defer cancel()
	type recvResult struct {
		msg *p4v1.StreamMessageResponse
		err error
	}
	ch := make(chan recvResult, 1)
	go func() {
		m, e := stream.Recv()
		ch <- recvResult{m, e}
	}()
	select {
	case <-deadline.Done():
		return fmt.Errorf("arbitration: %w", deadline.Err())
	case r := <-ch:
		if r.err != nil {
			return fmt.Errorf("arbitration recv: %w", r.err)
		}
		arb := r.msg.GetArbitration()
		if arb == nil {
			return fmt.Errorf("first stream message was %T, expected arbitration", r.msg.GetUpdate())
		}
		primary := statusOK(arb.GetStatus())
		s.setPrimary(primary)
		if primary {
			s.setState(StatePrimary, nil)
		} else {
			s.setState(StateBackup, nil)
		}
		return nil
	}
}

func (s *Supervisor) serve(ctx context.Context, stream p4v1.P4Runtime_StreamChannelClient) {
	recvErr := make(chan error, 1)
	go func() {
		for {
			msg, err := stream.Recv()
			if err != nil {
				recvErr <- err
				return
			}
			if arb := msg.GetArbitration(); arb != nil {
				primary := statusOK(arb.GetStatus())
				s.setPrimary(primary)
				if primary {
					s.setState(StatePrimary, nil)
				} else {
					s.setState(StateBackup, nil)
				}
				continue
			}
			if s.onPkt != nil {
				s.onPkt(msg)
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case err := <-recvErr:
			s.setState(StateDisconnected, err)
			return
		case req := <-s.sendCh:
			if err := stream.Send(req); err != nil {
				s.setState(StateDisconnected, err)
				return
			}
		}
	}
}

func (s *Supervisor) setState(st State, err error) {
	s.mu.Lock()
	s.state = st
	s.lastErr = err
	s.mu.Unlock()
	select {
	case s.events <- Event{State: st, Err: err}:
	default:
		// Drop if no reader — the latest state is always retrievable via State().
	}
}

func (s *Supervisor) setPrimary(p bool) {
	s.mu.Lock()
	s.primary = p
	s.mu.Unlock()
}

func (s *Supervisor) sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	t := time.NewTimer(jitter(d))
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	case <-s.stop:
		return false
	}
}

func nextBackoff(cur, max time.Duration) time.Duration {
	next := cur * 2
	if next <= 0 || next > max {
		return max
	}
	return next
}

func jitter(d time.Duration) time.Duration {
	// ±20% jitter.
	delta := time.Duration(float64(d) * 0.2)
	if delta <= 0 {
		return d
	}
	offset := time.Duration(rand.Int64N(int64(2*delta))) - delta
	return d + offset
}

// statusOK reports whether a MasterArbitrationUpdate status indicates primary
// mastership. P4Runtime uses codes.OK for primary and codes.ALREADY_EXISTS for
// backup. A nil status is treated as primary for backwards compatibility with
// targets that do not populate the field.
func statusOK(st *rpcstatus.Status) bool {
	if st == nil {
		return true
	}
	return codes.Code(st.GetCode()) == codes.OK
}
