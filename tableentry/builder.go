package tableentry

import (
	"bytes"
	"errors"
	"fmt"
	"sort"

	p4configv1 "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4v1 "github.com/p4lang/p4runtime/go/p4/v1"

	errs "github.com/zhh2001/p4runtime-go-controller/errors"
	"github.com/zhh2001/p4runtime-go-controller/internal/codec"
	"github.com/zhh2001/p4runtime-go-controller/pipeline"
)

// Builder is a fluent constructor for p4v1.TableEntry protos. The same
// Builder may be reused (Build and BuildKey create fresh protos every time).
type Builder struct {
	pipeline *pipeline.Pipeline
	table    *pipeline.TableDef
	tableErr error

	matches   map[string]MatchValue
	action    *actionSpec
	priority  int32
	timeout   int64
	isDefault bool
	metadata  []byte
}

type actionSpec struct {
	name   string
	params map[string]paramSpec
}

type paramSpec struct {
	value []byte
}

// NewBuilder starts a new table-entry builder bound to pipeline p. The table
// parameter is the fully qualified P4 name.
func NewBuilder(p *pipeline.Pipeline, table string) *Builder {
	b := &Builder{pipeline: p, matches: map[string]MatchValue{}}
	if p == nil {
		b.tableErr = errors.New("tableentry: nil pipeline")
		return b
	}
	td, ok := p.Table(table)
	if !ok {
		b.tableErr = fmt.Errorf("tableentry: table %q not in pipeline", table)
		return b
	}
	b.table = td
	return b
}

// Match adds or replaces a match constraint on the named field.
func (b *Builder) Match(field string, v MatchValue) *Builder {
	b.matches[field] = v
	return b
}

// Action sets the action invoked on a matched entry. params names and values
// must match the action declared in P4Info.
func (b *Builder) Action(name string, params ...ActionParam) *Builder {
	spec := &actionSpec{name: name, params: map[string]paramSpec{}}
	for _, p := range params {
		spec.params[p.Name] = paramSpec{value: p.Value}
	}
	b.action = spec
	return b
}

// Priority sets the priority. TERNARY, RANGE, and OPTIONAL tables require a
// positive priority. Other tables require zero.
func (b *Builder) Priority(p int32) *Builder {
	b.priority = p
	return b
}

// IdleTimeout sets the idle timeout in nanoseconds. Zero disables the timeout.
// Build rejects negative values, nonzero values on default entries, and
// nonzero values on tables without idle timeout support.
func (b *Builder) IdleTimeout(ns int64) *Builder {
	b.timeout = ns
	return b
}

// AsDefault marks the entry as the table's default action. When AsDefault is
// true, all match fields and Priority are ignored. The target uses the
// action as the fallback for unmatched packets.
func (b *Builder) AsDefault() *Builder {
	b.isDefault = true
	return b
}

// Metadata attaches opaque caller-supplied metadata to the entry. Useful
// when the controller wants to tag an entry for lookup after a Read.
func (b *Builder) Metadata(m []byte) *Builder {
	b.metadata = append([]byte(nil), m...)
	return b
}

// ActionParam is an action parameter name/value pair. The value must already
// be in canonical byte encoding — use the helpers in internal/codec or the
// exported codec package wrappers.
type ActionParam struct {
	Name  string
	Value []byte
}

// Param is a convenience constructor for ActionParam.
func Param(name string, value []byte) ActionParam {
	return ActionParam{Name: name, Value: value}
}

// Build validates the builder against the pipeline and produces an entry
// with a direct action. Constant entries and constant default actions cannot
// be written. Indirect tables require action profile references, which this
// builder does not construct. Default entries must use MODIFY updates.
func (b *Builder) Build() (*p4v1.TableEntry, error) {
	entry, err := b.BuildKey()
	if err != nil {
		return nil, err
	}
	if err := b.validateIdleTimeout(); err != nil {
		return nil, err
	}
	entry.IdleTimeoutNs = b.timeout
	if len(b.metadata) > 0 {
		entry.Metadata = append([]byte(nil), b.metadata...)
	}

	if b.action == nil {
		return nil, errors.New("tableentry.Build: action required")
	}
	act, err := b.encodeAction()
	if err != nil {
		return nil, err
	}
	entry.Action = &p4v1.TableAction{Type: &p4v1.TableAction_Action{Action: act}}
	return entry, nil
}

func (b *Builder) validateIdleTimeout() error {
	if b.timeout < 0 {
		return fmt.Errorf("tableentry.Build: negative idle timeout %d", b.timeout)
	}
	if b.timeout == 0 {
		return nil
	}
	if b.isDefault {
		return errors.New("tableentry.Build: default entries require zero idle timeout")
	}
	if b.table.Raw().GetIdleTimeoutBehavior() != p4configv1.Table_NOTIFY_CONTROL {
		return fmt.Errorf("tableentry.Build: table %q does not support idle timeout", b.table.Name)
	}
	return nil
}

// BuildKey validates and builds only the fields identifying an entry:
// table ID, match fields, priority, and the default-action flag. Action,
// metadata, and idle timeout are ignored. Use it for DELETE updates of
// ordinary entries. Default entries cannot be deleted.
func (b *Builder) BuildKey() (*p4v1.TableEntry, error) {
	if b.tableErr != nil {
		return nil, b.tableErr
	}
	entry := &p4v1.TableEntry{TableId: b.table.ID, IsDefaultAction: b.isDefault}
	if b.isDefault {
		return entry, nil
	}
	if err := b.encodeMatches(entry); err != nil {
		return nil, err
	}

	needsPriority := false
	for _, m := range b.table.MatchFields {
		switch m.MatchType {
		case p4configv1.MatchField_TERNARY, p4configv1.MatchField_RANGE, p4configv1.MatchField_OPTIONAL:
			needsPriority = true
		}
	}
	if needsPriority {
		if b.priority <= 0 {
			return nil, fmt.Errorf("tableentry: table %q requires positive priority", b.table.Name)
		}
		entry.Priority = b.priority
	} else if b.priority != 0 {
		return nil, fmt.Errorf("tableentry: table %q requires zero priority", b.table.Name)
	}
	return entry, nil
}

func (b *Builder) encodeMatches(entry *p4v1.TableEntry) error {
	// Build match field protos in field ID order for determinism.
	order := make([]*pipeline.MatchFieldDef, len(b.table.MatchFields))
	copy(order, b.table.MatchFields)
	sort.SliceStable(order, func(i, j int) bool { return order[i].ID < order[j].ID })

	for _, mf := range order {
		mv, ok := b.matches[mf.Name]
		if !ok {
			if mf.MatchType == p4configv1.MatchField_EXACT {
				return fmt.Errorf("%w: exact field %q required on table %q", errs.ErrInvalidMatchField, mf.Name, b.table.Name)
			}
			// Missing match value means don't-care; omit entirely.
			continue
		}
		fm, err := encodeField(mf, mv)
		if err != nil {
			return err
		}
		if fm != nil {
			entry.Match = append(entry.Match, fm)
		}
	}
	// Surface unknown fields as an error so typos are caught early.
	for name := range b.matches {
		if _, ok := b.table.MatchField(name); !ok {
			return fmt.Errorf("%w: %q not on table %q", errs.ErrInvalidMatchField, name, b.table.Name)
		}
	}
	return nil
}

func encodeField(mf *pipeline.MatchFieldDef, mv MatchValue) (*p4v1.FieldMatch, error) {
	switch v := mv.(type) {
	case ExactMatch:
		canon, err := codec.EncodeBytes(v.Value, int(mf.Bitwidth))
		if err != nil {
			return nil, fmt.Errorf("match %q: %w", mf.Name, err)
		}
		if mf.MatchType != p4configv1.MatchField_EXACT {
			return nil, fmt.Errorf("%w: field %q expects %s, got EXACT",
				errs.ErrUnsupportedMatchKind, mf.Name, mf.MatchType)
		}
		return &p4v1.FieldMatch{
			FieldId:        mf.ID,
			FieldMatchType: &p4v1.FieldMatch_Exact_{Exact: &p4v1.FieldMatch_Exact{Value: canon}},
		}, nil
	case LPMMatch:
		if mf.MatchType != p4configv1.MatchField_LPM {
			return nil, fmt.Errorf("%w: field %q expects %s, got LPM",
				errs.ErrUnsupportedMatchKind, mf.Name, mf.MatchType)
		}
		if v.PrefixLen == 0 {
			return nil, nil // don't-care
		}
		canon, err := codec.LPMMask(v.Value, int(v.PrefixLen), int(mf.Bitwidth))
		if err != nil {
			return nil, fmt.Errorf("match %q: %w", mf.Name, err)
		}
		return &p4v1.FieldMatch{
			FieldId: mf.ID,
			FieldMatchType: &p4v1.FieldMatch_Lpm{Lpm: &p4v1.FieldMatch_LPM{
				Value:     canon,
				PrefixLen: v.PrefixLen,
			}},
		}, nil
	case TernaryMatch:
		if mf.MatchType != p4configv1.MatchField_TERNARY {
			return nil, fmt.Errorf("%w: field %q expects %s, got TERNARY",
				errs.ErrUnsupportedMatchKind, mf.Name, mf.MatchType)
		}
		if allZero(v.Mask) {
			return nil, nil // don't-care
		}
		applied, err := codec.TernaryApply(v.Value, v.Mask, int(mf.Bitwidth))
		if err != nil {
			return nil, fmt.Errorf("match %q: %w", mf.Name, err)
		}
		mask, err := codec.EncodeBytes(v.Mask, int(mf.Bitwidth))
		if err != nil {
			return nil, fmt.Errorf("match %q mask: %w", mf.Name, err)
		}
		return &p4v1.FieldMatch{
			FieldId: mf.ID,
			FieldMatchType: &p4v1.FieldMatch_Ternary_{Ternary: &p4v1.FieldMatch_Ternary{
				Value: applied,
				Mask:  mask,
			}},
		}, nil
	case RangeMatch:
		if mf.MatchType != p4configv1.MatchField_RANGE {
			return nil, fmt.Errorf("%w: field %q expects %s, got RANGE",
				errs.ErrUnsupportedMatchKind, mf.Name, mf.MatchType)
		}
		if err := codec.ValidateRange(v.Low, v.High, int(mf.Bitwidth)); err != nil {
			return nil, fmt.Errorf("match %q: %w", mf.Name, err)
		}
		low, err := codec.EncodeBytes(v.Low, int(mf.Bitwidth))
		if err != nil {
			return nil, fmt.Errorf("match %q low: %w", mf.Name, err)
		}
		high, err := codec.EncodeBytes(v.High, int(mf.Bitwidth))
		if err != nil {
			return nil, fmt.Errorf("match %q high: %w", mf.Name, err)
		}
		if isFullRange(low, high, int(mf.Bitwidth)) {
			return nil, nil // don't-care
		}
		return &p4v1.FieldMatch{
			FieldId: mf.ID,
			FieldMatchType: &p4v1.FieldMatch_Range_{Range: &p4v1.FieldMatch_Range{
				Low: low, High: high,
			}},
		}, nil
	case OptionalMatch:
		if mf.MatchType != p4configv1.MatchField_OPTIONAL {
			return nil, fmt.Errorf("%w: field %q expects %s, got OPTIONAL",
				errs.ErrUnsupportedMatchKind, mf.Name, mf.MatchType)
		}
		if v.Value == nil {
			return nil, nil // don't-care
		}
		canon, err := codec.EncodeBytes(v.Value, int(mf.Bitwidth))
		if err != nil {
			return nil, fmt.Errorf("match %q: %w", mf.Name, err)
		}
		return &p4v1.FieldMatch{
			FieldId:        mf.ID,
			FieldMatchType: &p4v1.FieldMatch_Optional_{Optional: &p4v1.FieldMatch_Optional{Value: canon}},
		}, nil
	default:
		return nil, fmt.Errorf("unrecognized match type %T on field %q", mv, mf.Name)
	}
}

func (b *Builder) encodeAction() (*p4v1.Action, error) {
	if b.pipeline == nil {
		return nil, errors.New("tableentry.Build: pipeline required to encode action")
	}
	if b.isDefault {
		if b.table.Raw().GetConstDefaultActionId() != 0 {
			return nil, fmt.Errorf("tableentry.Build: table %q has a constant default action", b.table.Name)
		}
	} else if b.table.Const {
		return nil, fmt.Errorf("tableentry.Build: cannot write an action on constant table %q", b.table.Name)
	}
	if b.table.Raw().GetImplementationId() != 0 {
		return nil, fmt.Errorf("tableentry.Build: indirect table %q cannot use this direct-action builder", b.table.Name)
	}
	act, ok := b.pipeline.Action(b.action.name)
	if !ok {
		return nil, fmt.Errorf("tableentry.Build: action %q not in pipeline", b.action.name)
	}
	var ref *pipeline.ActionRef
	for _, candidate := range b.table.ActionRefs {
		if candidate.ID == act.ID {
			ref = candidate
			break
		}
	}
	if ref == nil {
		return nil, fmt.Errorf("tableentry.Build: action %q not on table %q", b.action.name, b.table.Name)
	}
	switch ref.Scope {
	case p4configv1.ActionRef_TABLE_AND_DEFAULT:
	case p4configv1.ActionRef_TABLE_ONLY:
		if b.isDefault {
			return nil, fmt.Errorf("tableentry.Build: action %q is TABLE_ONLY on table %q", b.action.name, b.table.Name)
		}
	case p4configv1.ActionRef_DEFAULT_ONLY:
		if !b.isDefault {
			return nil, fmt.Errorf("tableentry.Build: action %q is DEFAULT_ONLY on table %q", b.action.name, b.table.Name)
		}
	default:
		return nil, fmt.Errorf("tableentry.Build: action %q has unsupported scope %d on table %q", b.action.name, ref.Scope, b.table.Name)
	}
	out := &p4v1.Action{ActionId: act.ID}
	for _, pdef := range act.Params {
		p, ok := b.action.params[pdef.Name]
		if !ok {
			return nil, fmt.Errorf("%w: missing parameter %q on action %q",
				errs.ErrInvalidActionParam, pdef.Name, b.action.name)
		}
		canon, err := codec.EncodeBytes(p.value, int(pdef.Bitwidth))
		if err != nil {
			return nil, fmt.Errorf("action %q param %q: %w", b.action.name, pdef.Name, err)
		}
		out.Params = append(out.Params, &p4v1.Action_Param{
			ParamId: pdef.ID,
			Value:   canon,
		})
	}
	// Detect typos: parameters supplied but not declared.
	for name := range b.action.params {
		if _, ok := act.Param(name); !ok {
			return nil, fmt.Errorf("%w: %q not on action %q",
				errs.ErrInvalidActionParam, name, b.action.name)
		}
	}
	return out, nil
}

// isFullRange receives canonical endpoints validated against a positive width.
func isFullRange(low, high []byte, bitwidth int) bool {
	if !allZero(low) || len(high) != (bitwidth-1)/8+1 {
		return false
	}
	first := byte(0xff)
	if bits := bitwidth % 8; bits != 0 {
		first = byte((1 << uint(bits)) - 1)
	}
	if high[0] != first {
		return false
	}
	for _, value := range high[1:] {
		if value != 0xff {
			return false
		}
	}
	return true
}

func allZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}

// Equals is a convenience used by tests and the read path: two encoded match
// fields compare equal when their bytes match.
func Equals(a, b *p4v1.FieldMatch) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.FieldId != b.FieldId {
		return false
	}
	return bytes.Equal(mustMarshal(a), mustMarshal(b))
}

func mustMarshal(m *p4v1.FieldMatch) []byte {
	// Use proto reflection to get a stable byte representation via its
	// proto text form.
	return []byte(m.String())
}
