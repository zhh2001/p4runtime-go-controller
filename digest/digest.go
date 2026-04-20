package digest

import (
	"context"
	"fmt"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
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

// OnDigest registers h to receive every DigestList whose digest_id matches
// the given digest name. If name is empty, every DigestList is delivered.
// The returned closure cancels the subscription.
func (s *Subscriber) OnDigest(name string, h func(context.Context, *p4v1.DigestList)) func() {
	wanted := uint32(0)
	if name != "" {
		if d, ok := s.p.Digest(name); ok {
			wanted = d.ID
		}
	}
	return s.c.OnDigestList(func(ctx context.Context, msg *p4v1.DigestList) {
		if wanted != 0 && msg.GetDigestId() != wanted {
			return
		}
		h(ctx, msg)
	})
}

// Ack sends a DigestListAck for the given DigestList. It is common to call
// this from inside the callback passed to OnDigest once the controller has
// processed the batch.
func (s *Subscriber) Ack(ctx context.Context, msg *p4v1.DigestList) error {
	if msg == nil {
		return fmt.Errorf("digest.Ack: nil message")
	}
	return s.c.SendDigestAck(ctx, &p4v1.DigestListAck{
		DigestId: msg.GetDigestId(),
		ListId:   msg.GetListId(),
	})
}
