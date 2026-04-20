package packetio

import (
	"context"
	"fmt"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/internal/codec"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
)

// Subscriber couples a Client with a Pipeline so that PacketIn metadata can
// be decoded and PacketOut metadata can be encoded automatically.
type Subscriber struct {
	c       *client.Client
	p       *pipeline.Pipeline
	inMeta  *pipeline.ControllerPacketMetadataDef
	outMeta *pipeline.ControllerPacketMetadataDef
}

// NewSubscriber creates a packet-in subscription bound to the client and
// pipeline. It looks up the "packet_in" and "packet_out" controller metadata
// headers and keeps references for later encode/decode.
func NewSubscriber(c *client.Client, p *pipeline.Pipeline) (*Subscriber, error) {
	if c == nil || p == nil {
		return nil, fmt.Errorf("packetio.NewSubscriber: nil client or pipeline")
	}
	sub := &Subscriber{c: c, p: p}
	sub.inMeta, _ = p.PacketMetadata("packet_in")
	sub.outMeta, _ = p.PacketMetadata("packet_out")
	return sub, nil
}

// PacketIn is the controller-facing decoded view of a p4v1.PacketIn.
type PacketIn struct {
	Payload  []byte
	Metadata map[string][]byte
}

// PacketOut is the controller-facing view of a p4v1.PacketOut prior to
// encoding.
type PacketOut struct {
	Payload  []byte
	Metadata map[string][]byte
}

// OnPacket registers a handler for every decoded PacketIn. The returned
// closure cancels the subscription.
func (s *Subscriber) OnPacket(h func(context.Context, *PacketIn)) func() {
	return s.c.OnPacketIn(func(ctx context.Context, msg *p4v1.PacketIn) {
		decoded := s.decode(msg)
		h(ctx, decoded)
	})
}

// Send encodes out and delivers it to the target as a PacketOut.
func (s *Subscriber) Send(ctx context.Context, out *PacketOut) error {
	if out == nil {
		return fmt.Errorf("packetio.Send: nil packet")
	}
	msg := &p4v1.PacketOut{Payload: out.Payload}
	if s.outMeta != nil {
		for name, value := range out.Metadata {
			f, ok := s.outMeta.Field(name)
			if !ok {
				return fmt.Errorf("packetio.Send: unknown metadata %q", name)
			}
			canon, err := codec.EncodeBytes(value, int(f.Bitwidth))
			if err != nil {
				return fmt.Errorf("packetio.Send metadata %q: %w", name, err)
			}
			msg.Metadata = append(msg.Metadata, &p4v1.PacketMetadata{
				MetadataId: f.ID,
				Value:      canon,
			})
		}
	}
	return s.c.SendPacketOut(ctx, msg)
}

func (s *Subscriber) decode(msg *p4v1.PacketIn) *PacketIn {
	p := &PacketIn{Payload: msg.GetPayload(), Metadata: map[string][]byte{}}
	if s.inMeta == nil {
		return p
	}
	for _, m := range msg.GetMetadata() {
		f, ok := s.inMeta.FieldByID(m.GetMetadataId())
		if !ok {
			continue
		}
		p.Metadata[f.Name] = m.GetValue()
	}
	return p
}
