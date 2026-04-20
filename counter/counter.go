package counter

import (
	"context"
	"fmt"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
)

// Data is a controller-facing counter sample.
type Data struct {
	Name    string
	ID      uint32
	Index   int64
	Packets int64
	Bytes   int64
}

// Reader reads indirect counters by name.
type Reader struct {
	c *client.Client
	p *pipeline.Pipeline
}

// NewReader constructs a counter Reader.
func NewReader(c *client.Client, p *pipeline.Pipeline) (*Reader, error) {
	if c == nil || p == nil {
		return nil, fmt.Errorf("counter.NewReader: nil client or pipeline")
	}
	return &Reader{c: c, p: p}, nil
}

// Read returns all indexes for the named counter. index=-1 reads every
// entry; a non-negative value reads a single entry.
func (r *Reader) Read(ctx context.Context, name string, index int64) ([]*Data, error) {
	cdef, ok := r.p.Counter(name)
	if !ok {
		return nil, fmt.Errorf("counter %q not in pipeline", name)
	}
	entry := &p4v1.CounterEntry{CounterId: cdef.ID}
	if index >= 0 {
		entry.Index = &p4v1.Index{Index: index}
	}
	ents, err := r.c.Read(ctx, &p4v1.Entity{Entity: &p4v1.Entity_CounterEntry{CounterEntry: entry}})
	if err != nil {
		return nil, err
	}
	out := make([]*Data, 0, len(ents))
	for _, e := range ents {
		ce := e.GetCounterEntry()
		if ce == nil {
			continue
		}
		d := &Data{
			Name: cdef.Name,
			ID:   cdef.ID,
		}
		if ce.GetIndex() != nil {
			d.Index = ce.GetIndex().GetIndex()
		}
		d.Packets = ce.GetData().GetPacketCount()
		d.Bytes = ce.GetData().GetByteCount()
		out = append(out, d)
	}
	return out, nil
}

// Write sets the counter data at a specific index. Some targets do not
// support counter writes; those will return ErrTargetUnsupported.
func (r *Reader) Write(ctx context.Context, name string, index int64, packets, bytes int64) error {
	cdef, ok := r.p.Counter(name)
	if !ok {
		return fmt.Errorf("counter %q not in pipeline", name)
	}
	update := &p4v1.Update{
		Type: p4v1.Update_MODIFY,
		Entity: &p4v1.Entity{Entity: &p4v1.Entity_CounterEntry{
			CounterEntry: &p4v1.CounterEntry{
				CounterId: cdef.ID,
				Index:     &p4v1.Index{Index: index},
				Data:      &p4v1.CounterData{PacketCount: packets, ByteCount: bytes},
			},
		}},
	}
	return r.c.Write(ctx, client.WriteOptions{}, update)
}
