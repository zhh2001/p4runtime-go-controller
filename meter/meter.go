package meter

import (
	"context"
	"fmt"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"

	"github.com/zhh2001/p4runtime-go-controller/client"
	"github.com/zhh2001/p4runtime-go-controller/internal/resourceindex"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
)

// Config holds meter rates and burst sizes in the units declared by P4Info.
// Values must be non-negative. A zero Config explicitly sets all values to zero.
// Use Reset to restore the default GREEN behavior.
type Config struct {
	CIR    int64 // committed information rate (bytes/packets per second)
	CBurst int64 // committed burst size
	PIR    int64 // peak information rate
	PBurst int64 // peak burst size
	EBurst int64 // excess burst size (single-rate three-color meters only)
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
// read every index. Other indexes must be non-negative and within the array
// size, unless the meter declares a named index type for target-side translation.
func (r *Reader) Read(ctx context.Context, name string, index int64) ([]*p4v1.MeterEntry, error) {
	mdef, ok := r.p.Meter(name)
	if !ok {
		return nil, fmt.Errorf("meter %q not in pipeline", name)
	}
	if err := resourceindex.Validate(index, mdef.Size, mdef.Raw().GetIndexTypeName().GetName(), true); err != nil {
		return nil, fmt.Errorf("meter %q: %w", name, err)
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

// Write sets the meter configuration at index after validating it against P4Info.
// The configuration must satisfy the declared meter type's rate and burst rules.
// Target-specific limits are checked by the target.
// The index must be non-negative and within the array size, unless the meter
// declares a named index type for target-side translation.
func (r *Reader) Write(ctx context.Context, name string, index int64, cfg Config) error {
	return r.write(ctx, name, index, &cfg)
}

// Reset restores the meter at index to its default GREEN behavior by omitting
// Config in the request. It leaves any per-color counters untouched.
// The index follows the same rules as Write.
func (r *Reader) Reset(ctx context.Context, name string, index int64) error {
	return r.write(ctx, name, index, nil)
}

func (r *Reader) write(ctx context.Context, name string, index int64, cfg *Config) error {
	mdef, ok := r.p.Meter(name)
	if !ok {
		return fmt.Errorf("meter %q not in pipeline", name)
	}
	if err := resourceindex.Validate(index, mdef.Size, mdef.Raw().GetIndexTypeName().GetName(), false); err != nil {
		return fmt.Errorf("meter %q: %w", name, err)
	}
	var config *p4v1.MeterConfig
	if cfg != nil {
		var err error
		config, err = cfg.toProto(mdef.Raw().GetSpec())
		if err != nil {
			return fmt.Errorf("meter %q: %w", name, err)
		}
	}
	update := &p4v1.Update{
		Type: p4v1.Update_MODIFY,
		Entity: &p4v1.Entity{Entity: &p4v1.Entity_MeterEntry{
			MeterEntry: &p4v1.MeterEntry{
				MeterId: mdef.ID,
				Index:   &p4v1.Index{Index: index},
				Config:  config,
			},
		}},
	}
	return r.c.Write(ctx, client.WriteOptions{}, update)
}

func (cfg Config) toProto(spec *p4configv1.MeterSpec) (*p4v1.MeterConfig, error) {
	if spec == nil {
		return nil, fmt.Errorf("missing P4Info meter specification")
	}
	if spec.Unit != p4configv1.MeterSpec_BYTES && spec.Unit != p4configv1.MeterSpec_PACKETS {
		return nil, fmt.Errorf("unsupported meter unit %v", spec.Unit)
	}
	for _, field := range []struct {
		name  string
		value int64
	}{
		{"CIR", cfg.CIR}, {"CBurst", cfg.CBurst}, {"PIR", cfg.PIR}, {"PBurst", cfg.PBurst}, {"EBurst", cfg.EBurst},
	} {
		if field.value < 0 {
			return nil, fmt.Errorf("%s %d must be non-negative", field.name, field.value)
		}
	}
	switch spec.Type {
	case p4configv1.MeterSpec_TWO_RATE_THREE_COLOR:
		if cfg.PIR < cfg.CIR {
			return nil, fmt.Errorf("PIR %d must be at least CIR %d", cfg.PIR, cfg.CIR)
		}
		if cfg.EBurst != 0 {
			return nil, fmt.Errorf("EBurst must be zero for %v", spec.Type)
		}
	case p4configv1.MeterSpec_SINGLE_RATE_THREE_COLOR, p4configv1.MeterSpec_SINGLE_RATE_TWO_COLOR:
		if cfg.CIR != cfg.PIR || cfg.CBurst != cfg.PBurst {
			return nil, fmt.Errorf("%v requires CIR = PIR and CBurst = PBurst", spec.Type)
		}
		if spec.Type == p4configv1.MeterSpec_SINGLE_RATE_TWO_COLOR && cfg.EBurst != 0 {
			return nil, fmt.Errorf("EBurst must be zero for %v", spec.Type)
		}
	default:
		return nil, fmt.Errorf("unsupported meter type %v", spec.Type)
	}
	return &p4v1.MeterConfig{Cir: cfg.CIR, Cburst: cfg.CBurst, Pir: cfg.PIR, Pburst: cfg.PBurst, Eburst: cfg.EBurst}, nil
}
