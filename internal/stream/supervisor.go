package stream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"

	errs "github.com/zhh2001/p4runtime-go-controller/errors"
)

// State is the observable mastership state of a P4Runtime StreamChannel.
type State int

const (
	// StateDisconnected is the starting state and the state after the
	// stream drops before a reconnect attempt fires or after shutdown.
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
	lastErr error
	changed chan struct{}
	cancel  context.CancelFunc
	grace   *sendRequest

	events   chan Event
	sendCh   chan *sendRequest
	sendGate chan struct{}
	draining chan struct{}
	stop     chan struct{}
	stopped  chan struct{}
	once     sync.Once
}

type sendRequest struct {
	ctx     context.Context
	message *p4v1.StreamMessageRequest // nil closes the send direction
	state   <-chan struct{}
	done    chan struct{}
	err     error
	once    sync.Once
}

func (r *sendRequest) finish(err error) {
	r.once.Do(func() {
		r.err = err
		close(r.done)
	})
}

func (r *sendRequest) wait(ctx context.Context, stopped <-chan struct{}) error {
	select {
	case <-r.done:
		return r.err
	default:
	}
	select {
	case <-r.done:
		return r.err
	case <-ctx.Done():
		return ctx.Err()
	case <-stopped:
		select {
		case <-r.done:
			return r.err
		default:
			return errStopped
		}
	}
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
	s := &Supervisor{
		cfg:   cfg,
		dial:  dial,
		onPkt: onPkt,
		log: cfg.Logger.With(
			"device_id", cfg.DeviceID,
			"role", cfg.Role,
			"election_id_high", cfg.ElectionHigh,
			"election_id_low", cfg.ElectionLow,
		),
		state:    StateDisconnected,
		changed:  make(chan struct{}),
		events:   make(chan Event, 16),
		sendCh:   make(chan *sendRequest, 16),
		sendGate: make(chan struct{}, 1),
		draining: make(chan struct{}),
		stop:     make(chan struct{}),
		stopped:  make(chan struct{}),
	}
	s.sendGate <- struct{}{}
	return s
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

// WatchState returns the current state and a channel closed on the next state
// change. Reading both under the same lock prevents a missed notification
// between checking the state and waiting.
func (s *Supervisor) WatchState() (State, <-chan struct{}) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state, s.changed
}

// IsPrimary reports whether the supervisor currently holds primary mastership
// for the device.
func (s *Supervisor) IsPrimary() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state == StatePrimary
}

// Send waits for the request's gRPC Send to complete. Success does not confirm
// target receipt or packet forwarding. Requests interrupted by a stream failure
// are returned as errors and are not replayed. Cancellation after sending starts
// can interrupt the stream, and the target may already have received the request.
func (s *Supervisor) Send(ctx context.Context, req *p4v1.StreamMessageRequest) error {
	if req == nil {
		return fmt.Errorf("stream.Send: nil request")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-s.sendGate:
	case <-ctx.Done():
		return ctx.Err()
	case <-s.draining:
		return errStopped
	case <-s.stop:
		return errStopped
	case <-s.stopped:
		return errStopped
	}
	_, changed := s.WatchState()
	r := &sendRequest{ctx: ctx, message: req, state: changed, done: make(chan struct{})}
	err := s.enqueue(r)
	s.sendGate <- struct{}{}
	if err != nil {
		return err
	}
	return r.wait(ctx, s.stopped)
}

func (s *Supervisor) enqueue(r *sendRequest) error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	select {
	case <-s.draining:
		return errStopped
	case <-s.stop:
		return errStopped
	case <-s.stopped:
		return errStopped
	default:
	}
	select {
	case s.sendCh <- r:
		return nil
	case <-r.ctx.Done():
		return r.ctx.Err()
	case <-s.stop:
		return errStopped
	case <-s.stopped:
		return errStopped
	}
}

var errStopped = errs.ErrStreamClosed

// CloseGracefully sends all accepted requests, half-closes the current stream,
// and waits for its final status. No new requests or reconnects are allowed.
// Close can interrupt this wait. The caller must also call Close to release
// resources if ctx expires. Call this outside receive handlers with a deadline.
func (s *Supervisor) CloseGracefully(ctx context.Context) error {
	s.mu.RLock()
	r := s.grace
	s.mu.RUnlock()
	if r != nil {
		return r.wait(ctx, s.stopped)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-s.sendGate:
	case <-ctx.Done():
		return ctx.Err()
	case <-s.stopped:
		return errStopped
	}
	s.mu.Lock()
	r = s.grace
	if r != nil {
		s.mu.Unlock()
		s.sendGate <- struct{}{}
		return r.wait(ctx, s.stopped)
	}
	r = &sendRequest{ctx: ctx, done: make(chan struct{})}
	s.grace = r
	close(s.draining)
	s.mu.Unlock()
	select {
	case <-r.done:
	case <-s.stopped:
		r.finish(errStopped)
	default:
		select {
		case s.sendCh <- r:
		case <-ctx.Done():
			r.finish(ctx.Err())
		case <-s.stopped:
			r.finish(errStopped)
		}
	}
	s.sendGate <- struct{}{}
	return r.wait(ctx, s.stopped)
}

// Start launches the supervisor goroutine. It returns immediately. Use
// Events() to observe transitions, and Close() to stop.
func (s *Supervisor) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()
	go s.run(ctx)
}

// Close shuts the supervisor down and waits for the main loop to exit.
// It does not wait for a packet handler already in progress, so handlers can
// call Close themselves.
func (s *Supervisor) Close() {
	s.once.Do(func() {
		close(s.stop)
		s.mu.RLock()
		cancel := s.cancel
		s.mu.RUnlock()
		if cancel != nil {
			cancel()
		}
	})
	<-s.stopped
}

func (s *Supervisor) run(parent context.Context) {
	defer close(s.stopped)
	defer close(s.events)
	defer s.failPending(errStopped)
	defer func() {
		if s.State() != StateDisconnected {
			s.setState(parent, StateDisconnected, nil)
		}
		s.log.DebugContext(parent, "p4runtime: stream supervisor stopped")
	}()

	backoff := s.cfg.BackoffInitial
	for {
		if err := parent.Err(); err != nil {
			return
		}
		select {
		case <-s.stop:
			return
		case <-s.draining:
			return
		default:
		}

		s.setState(parent, StateConnecting, nil)

		ctx, cancel := context.WithCancel(parent)
		s.log.InfoContext(ctx, "p4runtime: opening StreamChannel")
		stream, err := s.dial(ctx)
		if err != nil {
			s.logFailure(ctx, "open", err)
			cancel()
			s.setState(parent, StateDisconnected, err)
			if !s.sleep(parent, backoff) {
				return
			}
			backoff = nextBackoff(backoff, s.cfg.BackoffMax)
			continue
		}

		if err := s.arbitrate(ctx, stream); err != nil {
			s.logFailure(ctx, "arbitration", err)
			cancel()
			s.setState(parent, StateDisconnected, err)
			if !s.sleep(parent, backoff) {
				return
			}
			backoff = nextBackoff(backoff, s.cfg.BackoffMax)
			continue
		}

		// Successful arbitration resets backoff before pumping the stream.
		backoff = s.cfg.BackoffInitial
		finished := s.serve(ctx, cancel, stream)
		cancel()
		if finished {
			return
		}
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
	case <-s.stop:
		return fmt.Errorf("arbitration: %w", errStopped)
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
		s.applyArbitration(ctx, arb)
		return nil
	}
}

func (s *Supervisor) serve(ctx context.Context, cancel context.CancelFunc, stream p4v1.P4Runtime_StreamChannelClient) bool {
	recvErr := make(chan error, 1)
	arbitration := make(chan *p4v1.MasterArbitrationUpdate)
	go func() {
		for {
			if ctx.Err() != nil {
				return
			}
			msg, err := stream.Recv()
			if err != nil {
				recvErr <- err
				return
			}
			if ctx.Err() != nil {
				return
			}
			if arb := msg.GetArbitration(); arb != nil {
				// Only the main loop updates state and publishes events.
				select {
				case arbitration <- arb:
				case <-ctx.Done():
					return
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
			return false
		case <-s.stop:
			return false
		case arb := <-arbitration:
			s.applyArbitration(ctx, arb)
		case err := <-recvErr:
			s.logFailure(ctx, "recv", err)
			s.setState(ctx, StateDisconnected, err)
			s.failPending(err)
			return false
		case req := <-s.sendCh:
			if err := req.ctx.Err(); err != nil {
				req.finish(err)
				continue
			}
			if req.message == nil {
				err := s.finishStream(ctx, stream, recvErr, arbitration)
				if err != nil {
					s.logFailure(ctx, "close", err)
				}
				req.finish(err)
				return true
			}
			if !s.IsPrimary() {
				req.finish(errs.ErrNotPrimary)
				continue
			}
			_, changed := s.WatchState()
			if changed != req.state {
				req.finish(errStopped)
				continue
			}
			stopCancel := context.AfterFunc(req.ctx, cancel) //nolint:contextcheck // request cancellation must interrupt the active stream
			err := stream.Send(req.message)
			stopCancel()
			if req.ctx.Err() != nil {
				err = req.ctx.Err()
			}
			req.finish(err)
			if err != nil {
				s.logFailure(ctx, "send", err)
				s.setState(ctx, StateDisconnected, err)
				s.failPending(err)
				return false
			}
		}
	}
}

func (s *Supervisor) finishStream(ctx context.Context, stream p4v1.P4Runtime_StreamChannelClient, recvErr <-chan error, arbitration <-chan *p4v1.MasterArbitrationUpdate) error {
	if err := stream.CloseSend(); err != nil {
		if ctx.Err() != nil {
			return errStopped
		}
		return err
	}
	for {
		select {
		case err := <-recvErr:
			// Recv can return cancellation before this select observes ctx.Done.
			if ctx.Err() != nil {
				return errStopped
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		case arb := <-arbitration:
			s.applyArbitration(ctx, arb)
		case <-ctx.Done():
			return errStopped
		case <-s.stop:
			return errStopped
		}
	}
}

func (s *Supervisor) failPending(err error) {
	for {
		select {
		case req := <-s.sendCh:
			req.finish(err)
		default:
			return
		}
	}
}

func (s *Supervisor) applyArbitration(ctx context.Context, arb *p4v1.MasterArbitrationUpdate) {
	state := StateBackup
	if statusOK(arb.GetStatus()) {
		state = StatePrimary
	}
	level := slog.LevelInfo
	if s.State() == state {
		level = slog.LevelDebug
	}
	s.setState(ctx, state, nil)
	s.log.Log(ctx, level, "p4runtime: arbitration status",
		"state", state.String(), "status_code", codes.Code(arb.GetStatus().GetCode()).String())
}

func (s *Supervisor) setState(ctx context.Context, st State, err error) {
	s.mu.Lock()
	previous := s.state
	if previous != st {
		s.state = st
		close(s.changed)
		s.changed = make(chan struct{})
	}
	s.lastErr = err
	s.mu.Unlock()
	select {
	case s.events <- Event{State: st, Err: err}:
	default:
		// Drop if no reader — the latest state is always retrievable via State().
	}
	if previous != st {
		s.log.DebugContext(ctx, "p4runtime: stream state changed",
			"from", previous.String(), "state", st.String(), "error", err)
	}
}

func (s *Supervisor) logFailure(ctx context.Context, stage string, err error) {
	// Local cancellation is reported by the state and shutdown logs.
	if ctx.Err() != nil {
		return
	}
	select {
	case <-s.stop:
		return
	default:
	}
	s.log.WarnContext(ctx, "p4runtime: StreamChannel failed", "stage", stage, "error", err)
}

func (s *Supervisor) sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	if ctx.Err() != nil {
		return false
	}
	select {
	case <-s.stop:
		return false
	case <-s.draining:
		return false
	default:
	}
	delay := jitter(d)
	t := time.NewTimer(delay)
	defer t.Stop()
	s.log.DebugContext(ctx, "p4runtime: StreamChannel retry scheduled", "delay", delay)
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	case <-s.stop:
		return false
	case <-s.draining:
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
