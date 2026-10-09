package digest

import (
	"context"
	"fmt"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"

	"github.com/zhh2001/p4runtime-go-controller/v2/client"
	"github.com/zhh2001/p4runtime-go-controller/v2/pipeline"
)

// Subscriber wraps a Client with a Pipeline so the caller can subscribe to
// P4Runtime digest notifications by their P4 name rather than by numeric ID.
type Subscriber struct {
	c *client.Client
	p *pipeline.Pipeline
}

// NewSubscriber builds a digest Subscriber.
func NewSubscriber(c *client.Client, p *pipeline.Pipeline) (*Subscriber, error) {
	if c == nil || p == nil {
		return nil, fmt.Errorf("digest.NewSubscriber: nil client or pipeline")
	}
	return &Subscriber{c: c, p: p}, nil
}

// Subscribe registers h for the given digest name or alias. An empty name
// receives every non-nil DigestList, including IDs absent from the P4Info.
// Unknown names and nil handlers return an error without registering a callback.
// The returned closure cancels future dispatches and can be called from a
// handler. Handlers already selected for the current message may still run.
func (s *Subscriber) Subscribe(name string, h func(context.Context, *p4v1.DigestList)) (func(), error) {
	if h == nil {
		return nil, fmt.Errorf("digest.Subscribe: nil handler")
	}
	var wanted uint32
	if name != "" {
		d, ok := s.p.Digest(name)
		if !ok {
			return nil, fmt.Errorf("digest.Subscribe: unknown digest %q", name)
		}
		if d.ID == 0 {
			return nil, fmt.Errorf("digest.Subscribe: digest %q has zero ID", name)
		}
		wanted = d.ID
	}
	return s.c.OnDigestList(func(ctx context.Context, msg *p4v1.DigestList) {
		if msg == nil || (name != "" && msg.GetDigestId() != wanted) {
			return
		}
		h(ctx, msg)
	}), nil
}

// OnDigest registers h by name or alias, or receives every non-nil DigestList
// when name is empty. Invalid subscriptions register no callback and return a
// no-op cancellation closure. Use Subscribe to receive validation errors.
func (s *Subscriber) OnDigest(name string, h func(context.Context, *p4v1.DigestList)) func() {
	off, err := s.Subscribe(name, h)
	if err != nil {
		return func() {}
	}
	return off
}

// Ack sends a DigestListAck for the given DigestList. It is common to call
// this after processing a batch received through Subscribe or OnDigest.
// The digest ID must be nonzero and declared in the subscriber's P4Info.
// Ack does not require an active subscription or track received batches, so
// callers can acknowledge a batch asynchronously after canceling a subscription.
// It waits for gRPC to send the acknowledgement, not for target processing.
func (s *Subscriber) Ack(ctx context.Context, msg *p4v1.DigestList) error {
	if msg == nil {
		return fmt.Errorf("digest.Ack: nil message")
	}
	if _, ok := s.p.DigestByID(msg.GetDigestId()); msg.GetDigestId() == 0 || !ok {
		return fmt.Errorf("digest.Ack: unknown digest ID %#x", msg.GetDigestId())
	}
	return s.c.SendDigestAck(ctx, &p4v1.DigestListAck{
		DigestId: msg.GetDigestId(),
		ListId:   msg.GetListId(),
	})
}
