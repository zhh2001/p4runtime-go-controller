package meter

import (
	"context"
	"fmt"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
)

// Config is a meter rate/burst configuration pair.
type Config struct {
	CIR    int64 // committed information rate (bytes/packets per second)
	CBurst int64 // committed burst size
	PIR    int64 // peak information rate
	PBurst int64 // peak burst size
}

// Reader reads and writes meter configurations by P4 name.
type Reader struct {
	c *client.Client
	p *pipeline.Pipeline
}

// NewReader constructs a meter Reader.
func NewReader(c *client.Client, p *pipeline.Pipeline) (*Reader, error) {
	if c == nil || p == nil {
		return nil, fmt.Errorf("meter.NewReader: nil client or pipeline")
	}
	return &Reader{c: c, p: p}, nil
}

// Read returns the meter configuration at the given index. Pass index=-1 to
// read every index.
func (r *Reader) Read(ctx context.Context, name string, index int64) ([]*p4v1.MeterEntry, error) {
	mdef, ok := r.p.Meter(name)
	if !ok {
		return nil, fmt.Errorf("meter %q not in pipeline", name)
	}
	entry := &p4v1.MeterEntry{MeterId: mdef.ID}
	if index >= 0 {
		entry.Index = &p4v1.Index{Index: index}
	}
	ents, err := r.c.Read(ctx, &p4v1.Entity{Entity: &p4v1.Entity_MeterEntry{MeterEntry: entry}})
	if err != nil {
		return nil, err
	}
	out := make([]*p4v1.MeterEntry, 0, len(ents))
	for _, e := range ents {
		if me := e.GetMeterEntry(); me != nil {
			out = append(out, me)
		}
	}
	return out, nil
}

// Write sets the meter configuration at index.
func (r *Reader) Write(ctx context.Context, name string, index int64, cfg Config) error {
	mdef, ok := r.p.Meter(name)
	if !ok {
		return fmt.Errorf("meter %q not in pipeline", name)
	}
	update := &p4v1.Update{
		Type: p4v1.Update_MODIFY,
		Entity: &p4v1.Entity{Entity: &p4v1.Entity_MeterEntry{
			MeterEntry: &p4v1.MeterEntry{
				MeterId: mdef.ID,
				Index:   &p4v1.Index{Index: index},
				Config: &p4v1.MeterConfig{
					Cir:    cfg.CIR,
					Cburst: cfg.CBurst,
					Pir:    cfg.PIR,
					Pburst: cfg.PBurst,
				},
			},
		}},
	}
	return r.c.Write(ctx, client.WriteOptions{}, update)
}
