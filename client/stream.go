package client

import (
	"context"
	"fmt"
	"sync"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"

	errs "github.com/zhh2001/p4runtime-go-controller/errors"
)

// PacketInHandler is invoked for every PacketIn arriving on the
// bidirectional stream. Handlers run on the supervisor goroutine and must
// return quickly; offload slow work to a channel or a worker pool.
type PacketInHandler func(ctx context.Context, msg *p4v1.PacketIn)

// DigestListHandler is invoked for every DigestList arriving on the stream.
type DigestListHandler func(ctx context.Context, msg *p4v1.DigestList)

// IdleTimeoutHandler is invoked for every IdleTimeoutNotification on the
// stream.
type IdleTimeoutHandler func(ctx context.Context, msg *p4v1.IdleTimeoutNotification)

// StreamMessageHandler is a catch-all handler that sees every
// StreamMessageResponse. Useful for logging, debugging, or bespoke message
// types the SDK does not yet wrap.
type StreamMessageHandler func(ctx context.Context, msg *p4v1.StreamMessageResponse)

type dispatchSlots struct {
	mu         sync.RWMutex
	packetIn   map[uint64]PacketInHandler
	digestList map[uint64]DigestListHandler
	idle       map[uint64]IdleTimeoutHandler
	stream     map[uint64]StreamMessageHandler
	nextID     uint64
}

func newDispatch() *dispatchSlots {
	return &dispatchSlots{
		packetIn:   map[uint64]PacketInHandler{},
		digestList: map[uint64]DigestListHandler{},
		idle:       map[uint64]IdleTimeoutHandler{},
		stream:     map[uint64]StreamMessageHandler{},
	}
}

func (d *dispatchSlots) addPacketIn(h PacketInHandler) func() {
	d.mu.Lock()
	id := d.nextID
	d.nextID++
	d.packetIn[id] = h
	d.mu.Unlock()
	return func() {
		d.mu.Lock()
		delete(d.packetIn, id)
		d.mu.Unlock()
	}
}

func (d *dispatchSlots) addDigestList(h DigestListHandler) func() {
	d.mu.Lock()
	id := d.nextID
	d.nextID++
	d.digestList[id] = h
	d.mu.Unlock()
	return func() {
		d.mu.Lock()
		delete(d.digestList, id)
		d.mu.Unlock()
	}
}

func (d *dispatchSlots) addIdle(h IdleTimeoutHandler) func() {
	d.mu.Lock()
	id := d.nextID
	d.nextID++
	d.idle[id] = h
	d.mu.Unlock()
	return func() {
		d.mu.Lock()
		delete(d.idle, id)
		d.mu.Unlock()
	}
}

func (d *dispatchSlots) addStream(h StreamMessageHandler) func() {
	d.mu.Lock()
	id := d.nextID
	d.nextID++
	d.stream[id] = h
	d.mu.Unlock()
	return func() {
		d.mu.Lock()
		delete(d.stream, id)
		d.mu.Unlock()
	}
}

func (d *dispatchSlots) dispatch(ctx context.Context, msg *p4v1.StreamMessageResponse) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, h := range d.stream {
		h(ctx, msg)
	}
	switch u := msg.GetUpdate().(type) {
	case *p4v1.StreamMessageResponse_Packet:
		for _, h := range d.packetIn {
			h(ctx, u.Packet)
		}
	case *p4v1.StreamMessageResponse_Digest:
		for _, h := range d.digestList {
			h(ctx, u.Digest)
		}
	case *p4v1.StreamMessageResponse_IdleTimeoutNotification:
		for _, h := range d.idle {
			h(ctx, u.IdleTimeoutNotification)
		}
	}
}

// OnPacketIn registers cb to receive PacketIn messages. The returned closure
// cancels the registration.
func (c *Client) OnPacketIn(cb PacketInHandler) func() { return c.dispatch.addPacketIn(cb) }

// OnDigestList registers cb to receive DigestList messages.
func (c *Client) OnDigestList(cb DigestListHandler) func() { return c.dispatch.addDigestList(cb) }

// OnIdleTimeout registers cb to receive IdleTimeoutNotification messages.
func (c *Client) OnIdleTimeout(cb IdleTimeoutHandler) func() { return c.dispatch.addIdle(cb) }

// OnStreamMessage registers cb to receive every StreamMessageResponse the
// supervisor delivers. Useful for logging or debugging.
func (c *Client) OnStreamMessage(cb StreamMessageHandler) func() { return c.dispatch.addStream(cb) }

// SendStreamRequest enqueues a raw StreamMessageRequest. Prefer the typed
// helpers (SendPacketOut, SendDigestAck) where possible.
func (c *Client) SendStreamRequest(ctx context.Context, req *p4v1.StreamMessageRequest) error {
	if !c.IsPrimary() {
		return errs.ErrNotPrimary
	}
	return c.sup.Send(ctx, req)
}

// SendPacketOut sends a PacketOut over the bidirectional stream.
func (c *Client) SendPacketOut(ctx context.Context, pkt *p4v1.PacketOut) error {
	if pkt == nil {
		return fmt.Errorf("client.SendPacketOut: nil packet")
	}
	return c.SendStreamRequest(ctx, &p4v1.StreamMessageRequest{
		Update: &p4v1.StreamMessageRequest_Packet{Packet: pkt},
	})
}

// SendDigestAck sends a DigestListAck over the bidirectional stream.
func (c *Client) SendDigestAck(ctx context.Context, ack *p4v1.DigestListAck) error {
	if ack == nil {
		return fmt.Errorf("client.SendDigestAck: nil ack")
	}
	return c.SendStreamRequest(ctx, &p4v1.StreamMessageRequest{
		Update: &p4v1.StreamMessageRequest_DigestAck{DigestAck: ack},
	})
}

// receiveHandler is wired as the supervisor's packet handler.
func (c *Client) receiveHandler(msg *p4v1.StreamMessageResponse) {
	c.dispatch.dispatch(c.ctx, msg)
}
