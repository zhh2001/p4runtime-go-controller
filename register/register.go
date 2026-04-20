package register

import (
	"context"
	"fmt"

	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
)

// Reader reads and writes P4 register arrays by name.
type Reader struct {
	c *client.Client
	p *pipeline.Pipeline
}

// NewReader constructs a register Reader.
func NewReader(c *client.Client, p *pipeline.Pipeline) (*Reader, error) {
	if c == nil || p == nil {
		return nil, fmt.Errorf("register.NewReader: nil client or pipeline")
	}
	return &Reader{c: c, p: p}, nil
}

// Read returns the register entry at index; pass index=-1 to read every
// index. The returned slices contain the raw P4Data messages.
func (r *Reader) Read(ctx context.Context, name string, index int64) ([]*p4v1.RegisterEntry, error) {
	rdef, ok := r.p.Register(name)
	if !ok {
		return nil, fmt.Errorf("register %q not in pipeline", name)
	}
	entry := &p4v1.RegisterEntry{RegisterId: rdef.ID}
	if index >= 0 {
		entry.Index = &p4v1.Index{Index: index}
	}
	ents, err := r.c.Read(ctx, &p4v1.Entity{Entity: &p4v1.Entity_RegisterEntry{RegisterEntry: entry}})
	if err != nil {
		return nil, err
	}
	out := make([]*p4v1.RegisterEntry, 0, len(ents))
	for _, e := range ents {
		if re := e.GetRegisterEntry(); re != nil {
			out = append(out, re)
		}
	}
	return out, nil
}

// Write stores the canonical-byte value at the given index of register.
func (r *Reader) Write(ctx context.Context, name string, index int64, value []byte) error {
	rdef, ok := r.p.Register(name)
	if !ok {
		return fmt.Errorf("register %q not in pipeline", name)
	}
	update := &p4v1.Update{
		Type: p4v1.Update_MODIFY,
		Entity: &p4v1.Entity{Entity: &p4v1.Entity_RegisterEntry{
			RegisterEntry: &p4v1.RegisterEntry{
				RegisterId: rdef.ID,
				Index:      &p4v1.Index{Index: index},
				Data: &p4v1.P4Data{
					Data: &p4v1.P4Data_Bitstring{Bitstring: value},
				},
			},
		}},
	}
	return r.c.Write(ctx, client.WriteOptions{}, update)
}
