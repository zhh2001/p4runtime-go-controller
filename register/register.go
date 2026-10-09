package register

import (
	"context"
	"fmt"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/internal/resourceindex"
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
// index. Other indexes must be non-negative and within the array size, unless
// the register declares a named index type for target-side translation.
// Entries contain the raw P4Data messages.
func (r *Reader) Read(ctx context.Context, name string, index int64) ([]*p4v1.RegisterEntry, error) {
	rdef, ok := r.p.Register(name)
	if !ok {
		return nil, fmt.Errorf("register %q not in pipeline", name)
	}
	if err := resourceindex.Validate(index, int64(rdef.Size), rdef.Raw().GetIndexTypeName().GetName(), true); err != nil {
		return nil, fmt.Errorf("register %q: %w", name, err)
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

// Write stores a bit<W> or int<W> value, including named types that resolve
// to these integer types. Signed values use big-endian two's complement.
// Values are copied and normalized to the shortest encoding. Empty input is zero.
// Use WriteData for other types.
// The index must be non-negative and within the array size, unless the register
// declares a named index type for target-side translation.
func (r *Reader) Write(ctx context.Context, name string, index int64, value []byte) error {
	return r.write(ctx, name, index, &p4v1.P4Data{Data: &p4v1.P4Data_Bitstring{Bitstring: value}}, true)
}

// WriteData stores a P4Data value at index after validating it against P4Info.
// Numeric fields are normalized and the value is copied before the RPC.
// Strings exposed through translated types retain their original bytes.
// Varbits retain their bytes and require an explicit bitwidth and matching length.
// Varbit fields in valid headers are unsupported because P4Header has no bitwidth.
// The index follows the same rules as Write. A nil value is an error.
func (r *Reader) WriteData(ctx context.Context, name string, index int64, value *p4v1.P4Data) error {
	return r.write(ctx, name, index, value, false)
}

func (r *Reader) write(ctx context.Context, name string, index int64, value *p4v1.P4Data, integerOnly bool) error {
	rdef, ok := r.p.Register(name)
	if !ok {
		return fmt.Errorf("register %q not in pipeline", name)
	}
	if err := resourceindex.Validate(index, int64(rdef.Size), rdef.Raw().GetIndexTypeName().GetName(), false); err != nil {
		return fmt.Errorf("register %q: %w", name, err)
	}
	n := dataNormalizer{types: r.p.Info().GetTypeInfo(), active: make(map[*p4configv1.P4DataTypeSpec]bool)}
	data, err := n.normalize(rdef.Raw().GetTypeSpec(), value, integerOnly)
	if err != nil {
		return fmt.Errorf("register %q: %w", name, err)
	}
	update := &p4v1.Update{
		Type: p4v1.Update_MODIFY,
		Entity: &p4v1.Entity{Entity: &p4v1.Entity_RegisterEntry{
			RegisterEntry: &p4v1.RegisterEntry{
				RegisterId: rdef.ID,
				Index:      &p4v1.Index{Index: index},
				Data:       data,
			},
		}},
	}
	return r.c.Write(ctx, client.WriteOptions{}, update)
}
